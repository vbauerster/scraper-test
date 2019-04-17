package scraper

import (
	"bufio"
	"container/heap"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// assuming srevice scheme is https
	defaultServiceScheme = "https"
	timeout              = 10 * time.Second
)

var (
	ErrNotFound = errors.New("not found")
	ErrNotReady = errors.New("not ready")
	ErrSrvDone  = errors.New("service done")
)

type (
	ServiceResponse struct {
		Name         string
		WindowTotal  int64
		AvgRespTime  time.Duration
		LastRespTime time.Duration
		LastError    error
		ErrRate      float64
	}
	BoundsResponse struct {
		Min string
		Max string
	}
)

type (
	serivceRequest struct {
		name string
		resp chan *serviceResponse
	}
	serviceResponse struct {
		res  *result
		stat *statistic
		err  error
	}
	bounds struct {
		min *result
		max *result
		err error
	}
	result struct {
		name     string
		respTime time.Duration
		err      error
	}
	statistic struct {
		windowTotal int64
		avgRespTime time.Duration
		errRate     float64
	}
)

type srvMap map[string]*entry

type window [11]*result

func (w window) last(count uint) (r *result) {
	for i := 0; i < len(w); i++ {
		r = w[(count-uint(i))%uint(len(w))]
		if r != nil {
			return r
		}
	}
	return r
}

func (w window) stat() (s *statistic) {
	s = new(statistic)
	var sum, errs int64
	var skipped int
	for _, r := range w {
		if r == nil {
			skipped++
			continue
		}
		if r.err != nil {
			errs++
		} else {
			sum += int64(r.respTime)
		}
		s.windowTotal++
	}
	if total := s.windowTotal - errs; total > 0 {
		s.avgRespTime = time.Duration(sum / total)
	}
	s.errRate = float64(errs) / float64(len(w)-skipped)
	return s
}

type entry struct {
	name   string
	window atomic.Value
	bcMin  *boundsCounter
	bcMax  *boundsCounter
}

func (e *entry) minUpd(h *boundsHeap) {
	e.bcMin.count++
	heap.Fix(h, e.bcMin.index)
}

func (e *entry) maxUpd(h *boundsHeap) {
	e.bcMax.count++
	heap.Fix(h, e.bcMax.index)
}

func (e *entry) check(ctx context.Context, count uint, aggCh chan<- *result) {
	res := &result{
		name: e.name,
	}
	res.respTime, res.err = httpGet(ctx, e.name)
	w := e.window.Load().(window)
	w[count%uint(len(w))] = res
	e.window.Store(w)
	if res.err != nil {
		return
	}
	aggCh <- res
}

type Scraper struct {
	fetchLimit   int
	rr           time.Duration
	services     []string
	agregateDone chan *bounds
	serviceReq   chan *serivceRequest
	boundsReq    chan chan *bounds
	done         chan struct{}
}

// NewScraper initializes and starts a scraper service
func NewScraper(ctx context.Context, services []string, refreshRate time.Duration, fetchLimit int) *Scraper {
	s := &Scraper{
		fetchLimit:   fetchLimit,
		rr:           refreshRate,
		services:     services,
		agregateDone: make(chan *bounds),
		serviceReq:   make(chan *serivceRequest),
		boundsReq:    make(chan chan *bounds),
		done:         make(chan struct{}),
	}
	if s.fetchLimit < 1 {
		s.fetchLimit = runtime.NumCPU()
	}
	if s.rr < time.Minute {
		s.rr = time.Minute
	}
	go s.serve(ctx)
	return s
}

func (s *Scraper) serve(ctx context.Context) {

	m := make(srvMap)
	sem := make(chan struct{}, s.fetchLimit)

	check := func(count uint) {
		var wg sync.WaitGroup
		wg.Add(len(m))
		aggCh := make(chan *result)
		for _, e := range m {
			w := e.window.Load().(window)
			// override old result, if any
			w[count%uint(len(w))] = nil
			e.window.Store(w)
			go func(e *entry) {
				defer wg.Done()
				// This sem is guaranteeing that there are no more than N fetch operations running.
				// It isn't guaranteeing that there are no more than N goroutines running.
				sem <- struct{}{}
				e.check(ctx, count, aggCh)
				<-sem
			}(e)
		}
		go func() {
			wg.Wait()
			close(aggCh)
		}()
		go s.boundsAgregate(aggCh)
	}

	minHeap := boundsHeap{}
	maxHeap := boundsHeap{}

	for i, name := range s.services {
		if m[name] != nil {
			continue
		}
		e := &entry{
			name:  name,
			bcMin: &boundsCounter{name, i, 0},
			bcMax: &boundsCounter{name, i, 0},
		}
		e.window.Store(window{})
		minHeap = append(minHeap, e.bcMin)
		maxHeap = append(maxHeap, e.bcMax)
		m[name] = e
	}
	heap.Init(&minHeap)
	heap.Init(&maxHeap)

	var count uint
	check(count)

	ticker := time.NewTicker(s.rr)
	for {
		select {
		case <-ticker.C:
			count++
			check(count)

		case res := <-s.agregateDone:
			m[res.min.name].minUpd(&minHeap)
			m[res.max.name].maxUpd(&maxHeap)

		case ch := <-s.boundsReq:
			go func(eMin, eMax *entry, count uint) {
				wMin := eMin.window.Load().(window)
				wMax := eMax.window.Load().(window)
				resp := &bounds{
					min: wMin.last(count),
					max: wMax.last(count),
				}
				if resp.min == nil || resp.max == nil {
					ch <- &bounds{err: ErrNotReady}
					return
				}
				ch <- resp
			}(m[minHeap[0].name], m[maxHeap[0].name], count)

		case req := <-s.serviceReq:
			go func(e *entry, count uint) {
				if e == nil {
					req.resp <- &serviceResponse{err: ErrNotFound}
					return
				}
				w := e.window.Load().(window)
				res := w.last(count)
				if res == nil {
					req.resp <- &serviceResponse{err: ErrNotReady}
					return
				}
				req.resp <- &serviceResponse{
					res:  res,
					stat: w.stat(),
				}
			}(m[req.name], count)

		case <-ctx.Done():
			ticker.Stop()
			close(s.done)
			return
		}
	}
}

func (s *Scraper) boundsAgregate(aggCh <-chan *result) {
	var min, max *result
	var total int
	for r := range aggCh {
		if total > 0 {
			if r.respTime < min.respTime {
				min = r
			}
			if r.respTime > max.respTime {
				max = r
			}
		} else {
			min = r
			max = r
		}
		total++
	}
	s.agregateDone <- &bounds{
		min: min,
		max: max,
	}
}

// GetServiceResponse returns response time of the service identified by name.
func (s *Scraper) GetServiceResponse(name string) (*ServiceResponse, error) {
	req := &serivceRequest{
		name: name,
		resp: make(chan *serviceResponse),
	}
	select {
	case s.serviceReq <- req:
		resp := <-req.resp
		if resp.err != nil {
			return nil, resp.err
		}
		return &ServiceResponse{
			Name:         name,
			WindowTotal:  resp.stat.windowTotal,
			ErrRate:      resp.stat.errRate,
			AvgRespTime:  resp.stat.avgRespTime,
			LastRespTime: resp.res.respTime,
			LastError:    resp.res.err,
		}, nil
	case <-s.done:
		return nil, ErrSrvDone
	}
}

// GetBounds ...
func (s *Scraper) GetBounds() (*BoundsResponse, error) {
	ch := make(chan *bounds)
	select {
	case s.boundsReq <- ch:
		resp := <-ch
		if resp.err != nil {
			return nil, resp.err
		}
		return &BoundsResponse{
			Min: resp.min.name,
			Max: resp.max.name,
		}, nil
	case <-s.done:
		return nil, ErrSrvDone
	}
}

func httpGet(ctx context.Context, name string) (time.Duration, error) {
	// in perfect world we should use http.MethodHead, but not all services implement it
	req, err := http.NewRequest(http.MethodGet, "", nil)
	if err != nil {
		return 0, err
	}
	req.URL.Scheme = defaultServiceScheme
	req.URL.Host = name
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return 0, err
	}
	return time.Since(start), resp.Body.Close()
}

// ReadFrom helper func to read file
func ReadFrom(name string) (services []string, err error) {
	f, err := os.Open(name)
	if err != nil {
		return services, err
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	return readLines(f)
}

func readLines(r io.Reader) (lines []string, err error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if len(text) == 0 || strings.HasPrefix(text, "#") {
			continue
		}
		lines = append(lines, text)
	}
	return lines, scanner.Err()
}
