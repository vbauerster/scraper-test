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

var (
	ErrNotFound = errors.New("not found")
	ErrNotReady = errors.New("not ready")
)

type BoundaryResponse struct {
	Participants int
	Name         string
	RespTime     time.Duration
}

type boundsType int

const (
	boundsMin boundsType = iota
	boundsMax
)

// assuming srevice scheme is https
const defaultServiceScheme = "https"

type srvMap map[string]*entry

type result struct {
	name     string
	respTime time.Duration
	err      error
}

type boundsRequest struct {
	boundsType boundsType
	response   chan *boundsResponse
}

type boundsResponse struct {
	total int
	res   result
}

type entry struct {
	res   result
	ready chan struct{} // closed when res is ready
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
	stream  chan *result
	request chan *boundsRequest
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
			stream:  make(chan *result),
			request: make(chan *boundsRequest),
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
		e.check(ctx, s.rr)
		s.bk.send(ctx, &e.res)
		<-sem
	}

	ticker := time.NewTicker(s.rr)

	m := make(srvMap)
	for _, name := range s.services {
		if m[name] != nil {
			continue
		}
		m[name] = &entry{
			res:   result{name: name},
			ready: make(chan struct{}),
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
		select {
		case <-ticker.C:
			wg.Wait()
			s.bk.send(ctx, nil)
			m = make(srvMap)
			for _, name := range s.services {
				if m[name] != nil {
					continue
				}
				m[name] = &entry{
					res:   result{name: name},
					ready: make(chan struct{}),
				}
			}
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

func (s *boundsKeeper) send(ctx context.Context, res *result) {
	select {
	case s.stream <- res:
	case <-ctx.Done():
	}
}

func (s *boundsKeeper) serve(ctx context.Context) {
	var reset bool
	var total int
	var min *result
	var max *result
	for {
		select {
		case res := <-s.stream:
			if res == nil {
				reset = !reset
				break
			}
			if res.err != nil {
				break
			}
			if reset {
				reset = !reset
				total = 0
			}
			if total > 0 {
				if res.respTime < min.respTime {
					min = res
				}
				if res.respTime > max.respTime {
					max = res
				}
			} else {
				min = res
				max = res
			}
			total++

		case req := <-s.request:
			resp := &boundsResponse{total: total}
			switch req.boundsType {
			case boundsMin:
				if min != nil {
					resp.res = *min
				} else {
					resp.res.err = ErrNotReady
				}
			case boundsMax:
				if max != nil {
					resp.res = *max
				} else {
					resp.res.err = ErrNotReady
				}
			}
			req.response <- resp

		case <-ctx.Done():
			return
		}
	}
}

func (s *Scraper) GetServices() []string {
	return s.services
}

// GetResponseTime returns response time of the service identified by name.
// If err != nil, service is not accessible.
func (s *Scraper) GetResponseTime(name string) (time.Duration, error) {
	<-s.ready
	e := s.cache.Load().(srvMap)[name]
	if e == nil {
		return 0, ErrNotFound
	}
	res := e.getResult()
	return res.respTime, res.err
}

func (s *Scraper) GetMin() (*BoundaryResponse, error) {
	total, res := s.getBoundary(boundsMin)
	if res.err != nil {
		return nil, res.err
	}
	return &BoundaryResponse{
		Participants: total,
		Name:         res.name,
		RespTime:     res.respTime,
	}, nil
}

func (s *Scraper) GetMax() (*BoundaryResponse, error) {
	total, res := s.getBoundary(boundsMax)
	if res.err != nil {
		return nil, res.err
	}
	return &BoundaryResponse{
		Participants: total,
		Name:         res.name,
		RespTime:     res.respTime,
	}, nil
}

func (s *Scraper) getBoundary(t boundsType) (total int, res result) {
	req := &boundsRequest{
		boundsType: t,
		response:   make(chan *boundsResponse, 1),
	}
	select {
	case s.bk.request <- req:
	case <-s.done:
		req.response <- new(boundsResponse)
	}
	resp := <-req.response
	return resp.total, resp.res
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
