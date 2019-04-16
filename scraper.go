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

type BoundsResponse struct {
	Min *ServiceResponse
	Max *ServiceResponse
}

type ServiceResponse struct {
	Name         string
	AvgRespTime  time.Duration
	LastRespTime time.Duration
	LastError    error
	ErrRate      float32
}

type srvMap map[string]*entry

type result struct {
	name        string
	ready       chan struct{}
	respTime    time.Duration
	avgRespTime time.Duration
	errRate     float32
	err         error
}

type window [15]*result

func (w window) last(count uint) (r *result) {
	for i := 0; i < len(w); i++ {
		r = w[(count-uint(i))%uint(len(w))]
		if r != nil {
			return r
		}
	}
	return r
}

type entry struct {
	name   string
	window atomic.Value

	bcMin *boundsCounter
	bcMax *boundsCounter
}

type boundsCounter struct {
	name  string
	index int
	count uint
}

func (e *entry) minUpd(h *boundsHeap) {
	e.bcMin.count++
	heap.Fix(h, e.bcMin.index)
}

func (e *entry) maxUpd(h *boundsHeap) {
	e.bcMax.count++
	heap.Fix(h, e.bcMax.index)
}

func (e *entry) check(ctx context.Context, ch chan<- *result, count uint) {
	res := &result{
		name:  e.name,
		ready: make(chan struct{}),
	}
	res.respTime, res.err = httpGet(ctx, e.name)
	w := e.window.Load().(window)
	w[count%uint(len(w))] = res
	var sum, errs, total int64
	for _, r := range w {
		if r == nil {
			break
		}
		if r.err != nil {
			errs++
		} else {
			sum += int64(r.respTime)
			total++
		}
	}
	if total > 0 {
		res.avgRespTime = time.Duration(sum / total)
	}
	res.errRate = float32(errs) / float32(len(w))
	close(res.ready)
	e.window.Store(w)
	if res.err != nil {
		return
	}
	ch <- res
}

type Scraper struct {
	fetchLimit   int
	rr           time.Duration
	services     []string
	agregateDone chan *boundsResult
	serviceReq   chan *serivceRequest
	boundsReq    chan chan *boundsResponse
	done         chan struct{}
}

type serivceRequest struct {
	name string
	resp chan *serviceResponse
}

type serviceResponse struct {
	res *result
	err error
}

type boundsResponse struct {
	min *result
	max *result
	err error
}

type boundsResult struct {
	min *result
	max *result
}

// NewScraper initializes and starts a scraper service
func NewScraper(ctx context.Context, services []string, refreshRate time.Duration, fetchLimit int) *Scraper {
	s := &Scraper{
		fetchLimit:   fetchLimit,
		rr:           refreshRate,
		services:     services,
		agregateDone: make(chan *boundsResult),
		serviceReq:   make(chan *serivceRequest),
		boundsReq:    make(chan chan *boundsResponse),
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
				e.check(ctx, aggCh, count)
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

	var bounds *boundsResult
	ticker := time.NewTicker(s.rr)
	for {
		select {
		case <-ticker.C:
			count++
			check(count)

		case res := <-s.agregateDone:
			m[res.min.name].minUpd(&minHeap)
			m[res.max.name].maxUpd(&maxHeap)
			wMin := m[minHeap[0].name].window.Load().(window)
			wMax := m[maxHeap[0].name].window.Load().(window)
			bounds = &boundsResult{
				min: wMin.last(count),
				max: wMax.last(count),
			}

		case ch := <-s.boundsReq:
			if bounds == nil {
				ch <- &boundsResponse{err: ErrNotReady}
				break
			}
			ch <- &boundsResponse{
				min: bounds.min,
				max: bounds.max,
			}

		case req := <-s.serviceReq:
			e := m[req.name]
			if e == nil {
				req.resp <- &serviceResponse{err: ErrNotFound}
				break
			}
			r := e.window.Load().(window).last(count)
			if r == nil {
				req.resp <- &serviceResponse{err: ErrNotReady}
				break
			}
			req.resp <- &serviceResponse{res: r}

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
	s.agregateDone <- &boundsResult{
		min: min,
		max: max,
	}
}

// GetServiceResponse returns response time of the service identified by name.
func (s *Scraper) GetServiceResponse(name string) (*ServiceResponse, error) {
	req := &serivceRequest{
		name: name,
		resp: make(chan *serviceResponse, 1),
	}
	select {
	case s.serviceReq <- req:
		resp := <-req.resp
		if resp.err != nil {
			return nil, resp.err
		}
		<-resp.res.ready
		return &ServiceResponse{
			Name:         name,
			AvgRespTime:  resp.res.avgRespTime,
			LastRespTime: resp.res.respTime,
			LastError:    resp.res.err,
			ErrRate:      resp.res.errRate,
		}, nil
	case <-s.done:
		return nil, ErrSrvDone
	}
}

// GetBounds ...
func (s *Scraper) GetBounds() (*BoundsResponse, error) {
	ch := make(chan *boundsResponse, 1)
	select {
	case s.boundsReq <- ch:
		resp := <-ch
		if resp.err != nil {
			return nil, resp.err
		}
		<-resp.min.ready
		<-resp.max.ready
		return &BoundsResponse{
			Min: &ServiceResponse{
				Name:         resp.min.name,
				AvgRespTime:  resp.min.avgRespTime,
				LastRespTime: resp.min.respTime,
				LastError:    resp.min.err,
				ErrRate:      resp.min.errRate,
			},
			Max: &ServiceResponse{
				Name:         resp.max.name,
				AvgRespTime:  resp.max.avgRespTime,
				LastRespTime: resp.max.respTime,
				LastError:    resp.max.err,
				ErrRate:      resp.max.errRate,
			},
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
