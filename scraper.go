package scraper

import (
	"bufio"
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

type boundsType int

const (
	boundsMin boundsType = iota
	boundsMax
)

const (
	// assuming srevice scheme is https
	defaultServiceScheme = "https"
	timeout              = 15 * time.Second
)

var (
	ErrNotFound = errors.New("not found")
	ErrNotReady = errors.New("not ready")
)

type BoundaryResponse struct {
	TotalOk  int
	TotalErr int
	Name     string
	RespTime time.Duration
}

type ServiceResponse struct {
	Name     string
	RespTime time.Duration
	Error    error
}

type srvMap map[string]*entry

type result struct {
	checkCount uint
	name       string
	respTime   time.Duration
	err        error
}

type boundsRequest struct {
	boundsType boundsType
	response   chan *boundsResponse
}

type boundsResponse struct {
	totalOk  int
	totalErr int
	res      result
}

type entry struct {
	chkCount uint
	res      result
	ready    chan struct{} // closed when res is ready
}

func (e *entry) check(ctx context.Context, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	e.res.respTime, e.res.err = httpGet(ctx, e.res.name)
	close(e.ready)
}

func (e *entry) getResult() result {
	<-e.ready
	return e.res
}

type Scraper struct {
	fetchLimit int
	rr         time.Duration
	services   []string
	cache      atomic.Value
	ready      chan struct{}
	done       chan struct{}
	bk         *boundsKeeper
}

type boundsKeeper struct {
	stream  chan *entry
	request chan *boundsRequest
	errors  chan []result
}

// NewScraper initializes and starts a scraper service
func NewScraper(ctx context.Context, services []string, refreshRate time.Duration, fetchLimit int) *Scraper {
	s := &Scraper{
		fetchLimit: fetchLimit,
		rr:         refreshRate,
		services:   services,
		ready:      make(chan struct{}),
		done:       make(chan struct{}),
		bk: &boundsKeeper{
			stream:  make(chan *entry),
			request: make(chan *boundsRequest),
			errors:  make(chan []result),
		},
	}
	if s.fetchLimit < 1 {
		s.fetchLimit = runtime.NumCPU()
	}
	if s.rr < time.Minute {
		s.rr = time.Minute
	}
	go s.serve(ctx)
	go s.bk.serve(ctx)
	return s
}

func (s *Scraper) serve(ctx context.Context) {

	sem := make(chan struct{}, s.fetchLimit)

	check := func(wg *sync.WaitGroup, e *entry) {
		defer wg.Done()
		// This sem is guaranteeing that there are no more than N fetch operations running.
		// It isn't guaranteeing that there are no more than N goroutines running.
		sem <- struct{}{}
		e.check(ctx, timeout)
		<-sem
		s.bk.send(ctx, e)
	}

	var chkCount uint
	ticker := time.NewTicker(s.rr)

	m := make(srvMap)
	for _, name := range s.services {
		if m[name] != nil {
			continue
		}
		m[name] = &entry{
			chkCount: chkCount,
			res:      result{name: name},
			ready:    make(chan struct{}),
		}
	}
	s.cache.Store(m)
	close(s.ready)
	wg := new(sync.WaitGroup)
	for _, e := range m {
		wg.Add(1)
		go check(wg, e)
	}

	for {
		chkCount++
		select {
		case <-ticker.C:
			wg.Wait()
			m = make(srvMap)
			for _, name := range s.services {
				if m[name] != nil {
					continue
				}
				m[name] = &entry{
					chkCount: chkCount,
					res:      result{name: name},
					ready:    make(chan struct{}),
				}
			}
			// no need to protect by mutex, as this is the only goroutine updating
			// s.cache Value
			s.cache.Store(m)
			for _, e := range m {
				wg.Add(1)
				go check(wg, e)
			}

		case <-ctx.Done():
			ticker.Stop()
			close(s.done)
			return
		}
	}
}

func (s *boundsKeeper) send(ctx context.Context, e *entry) {
	select {
	case s.stream <- e:
	case <-ctx.Done():
	}
}

func (s *boundsKeeper) serve(ctx context.Context) {
	var chkCount uint
	var total int
	min := result{err: ErrNotReady}
	max := result{err: ErrNotReady}
	errors := []result{}
	for {
		select {
		case entry := <-s.stream:
			if entry.chkCount != chkCount {
				total = 0
				errors = errors[0:0]
			}
			if entry.res.err != nil {
				errors = append(errors, entry.res)
				break
			}
			if total > 0 {
				if entry.res.respTime < min.respTime {
					min = entry.res
				}
				if entry.res.respTime > max.respTime {
					max = entry.res
				}
			} else {
				min = entry.res
				max = entry.res
			}
			total++
			chkCount = entry.chkCount

		case req := <-s.request:
			resp := &boundsResponse{
				totalOk:  total,
				totalErr: len(errors),
			}
			switch req.boundsType {
			case boundsMin:
				resp.res = min
			case boundsMax:
				resp.res = max
			}
			req.response <- resp

		case s.errors <- errors[0:len(errors):len(errors)]:

		case <-ctx.Done():
			return
		}
	}
}

func (s *Scraper) GetErrorServices() (services []*ServiceResponse) {

	select {
	case errors := <-s.bk.errors:
		for _, res := range errors {
			services = append(services, &ServiceResponse{
				Name:  res.name,
				Error: res.err,
			})
		}
	case <-s.done:
	}

	return services
}

// GetServiceResponse returns response time of the service identified by name.
func (s *Scraper) GetServiceResponse(name string) (*ServiceResponse, error) {
	<-s.ready
	entry := s.cache.Load().(srvMap)[name]
	if entry == nil {
		return nil, ErrNotFound
	}
	res := entry.getResult()
	return &ServiceResponse{
		Name:     res.name,
		RespTime: res.respTime,
		Error:    res.err,
	}, nil
}

func (s *Scraper) GetMin() (*BoundaryResponse, error) {
	res := s.getBoundary(boundsMin)
	if res.res.err != nil {
		return nil, res.res.err
	}
	return &BoundaryResponse{
		TotalOk:  res.totalOk,
		TotalErr: res.totalErr,
		Name:     res.res.name,
		RespTime: res.res.respTime,
	}, nil
}

func (s *Scraper) GetMax() (*BoundaryResponse, error) {
	res := s.getBoundary(boundsMax)
	if res.res.err != nil {
		return nil, res.res.err
	}
	return &BoundaryResponse{
		TotalOk:  res.totalOk,
		TotalErr: res.totalErr,
		Name:     res.res.name,
		RespTime: res.res.respTime,
	}, nil
}

func (s *Scraper) getBoundary(t boundsType) *boundsResponse {
	req := &boundsRequest{
		boundsType: t,
		response:   make(chan *boundsResponse, 1),
	}
	select {
	case s.bk.request <- req:
	case <-s.done:
		req.response <- new(boundsResponse)
	}
	return <-req.response
}

func httpGet(ctx context.Context, name string) (time.Duration, error) {
	// in perfect world we should use http.MethodHead, but not all services implement it
	req, err := http.NewRequest(http.MethodGet, "", nil)
	if err != nil {
		return 0, err
	}
	req.URL.Scheme = defaultServiceScheme
	req.URL.Host = name
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
