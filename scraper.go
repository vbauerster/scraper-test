package scraper

import (
	"bufio"
	"container/heap"
	"context"
	"errors"
	"fmt"
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
	timeout              = 15 * time.Second
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
	respTime    time.Duration
	avgRespTime time.Duration
	errRate     float32
	err         error
	ready       chan struct{}
}

type window [10]*result

type entry struct {
	name   string
	window atomic.Value
	// ress   window

	bcMin *BoundsCounter
	bcMax *BoundsCounter
}

type BoundsCounter struct {
	name  string
	index int
	count int
}

func (e *entry) minUpd(h *BoundsHeap) {
	e.bcMin.count++
	heap.Fix(h, e.bcMin.index)
}

func (e *entry) maxUpd(h *BoundsHeap) {
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
	for i, r := range w {
		if r == nil {
			if e.name == "qq.com" {
				fmt.Fprintf(os.Stderr, "check#%d: w[%d]=%v\n", count, i, r)
			}
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
		// boundsReq:    make(chan *boundsRequest),
		done: make(chan struct{}),
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

	sem := make(chan struct{}, s.fetchLimit)

	check := func(wg *sync.WaitGroup, e *entry, ch chan<- *result, count uint) {
		defer wg.Done()
		// This sem is guaranteeing that there are no more than N fetch operations running.
		// It isn't guaranteeing that there are no more than N goroutines running.
		sem <- struct{}{}
		e.check(ctx, ch, count)
		<-sem
	}

	ticker := time.NewTicker(s.rr)
	m := make(srvMap)
	minHeap := BoundsHeap{}
	maxHeap := BoundsHeap{}

	for _, name := range s.services {
		if m[name] != nil {
			continue
		}
		bcMin := &BoundsCounter{name: name}
		bcMax := &BoundsCounter{name: name}
		e := &entry{
			name:  name,
			bcMin: bcMin,
			bcMax: bcMax,
		}
		e.window.Store(window{})
		m[name] = e
		minHeap = append(minHeap, bcMin)
		maxHeap = append(maxHeap, bcMax)
	}
	heap.Init(&minHeap)
	heap.Init(&maxHeap)

	var bounds *boundsResult
	var count uint
	for {
		select {
		case <-ticker.C:
			in := make(chan *result)
			var wg sync.WaitGroup
			wg.Add(len(m))
			for _, e := range m {
				w := e.window.Load().(window)
				w[count%uint(len(w))] = nil
				e.window.Store(w)
				go check(&wg, e, in, count)
			}
			go func() {
				wg.Wait()
				close(in)
			}()
			go s.boundsAgregate(in)
			count++

		case res := <-s.agregateDone:
			m[res.min.name].minUpd(&minHeap)
			m[res.max.name].maxUpd(&maxHeap)
			// minEntry := m[minHeap[0].name]
			// maxEntry := m[maxHeap[0].name]
			// bounds = &boundsResult{
			// 	min: minEntry.ress[(count-1)%uint(len(minEntry.ress))],
			// 	max: maxEntry.ress[(count-1)%uint(len(maxEntry.ress))],
			// }

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
			go func(e *entry, resp chan<- *serviceResponse, count uint) {
				if e == nil {
					resp <- &serviceResponse{err: ErrNotFound}
					return
				}
				w := e.window.Load().(window)
				var r *result
				if count > 0 {
					for i := 1; i <= len(w); i++ {
						r = w[(count-uint(i))%uint(len(w))]
						if e.name == "qq.com" {
							fmt.Fprintf(os.Stderr, "for#%d: w[%d]=%v\n", count, (count-uint(i))%uint(len(w)), r)
						}
						if r != nil {
							break
						}
					}
				}
				if r == nil {
					// fmt.Fprintf(os.Stderr, "not-ready: %d\n", count)
					resp <- &serviceResponse{err: ErrNotReady}
					return
				}
				<-r.ready
				resp <- &serviceResponse{res: r}
			}(m[req.name], req.resp, count)

		case <-ctx.Done():
			ticker.Stop()
			close(s.done)
			return
		}
	}
}

func (s *Scraper) boundsAgregate(in <-chan *result) {
	var min, max *result
	var total int
	for r := range in {
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
		resp: make(chan *serviceResponse),
	}
	select {
	case s.serviceReq <- req:
		resp := <-req.resp
		switch resp.err {
		case ErrNotReady:
			return nil, ErrNotReady
		case ErrNotFound:
			return nil, ErrNotFound
		}
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

func (s *Scraper) GetBounds() (*BoundsResponse, error) {
	ch := make(chan *boundsResponse, 1)
	select {
	case s.boundsReq <- ch:
		resp := <-ch
		if resp.err == ErrNotReady {
			return nil, ErrNotReady
		}
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
