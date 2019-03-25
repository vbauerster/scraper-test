package scraper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNotFound = errors.New("not found")

type Map map[string]*entry

type boundsType int

const (
	boundsMin boundsType = iota
	boundsMax
)

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

func (e *entry) check(ctx context.Context) {
	fmt.Printf("check %q\n", e.res.name)
	e.res.respTime, e.res.err = httpGet(ctx, e.res.name)
	close(e.ready)
}

func (e *entry) getResult() result {
	<-e.ready
	return e.res
}

type scraper struct {
	services []string
	cache    atomic.Value
	ready    chan struct{}
	done     chan struct{}
	bk       *boundsKeeper
}

type boundsKeeper struct {
	stream  chan *result
	request chan *boundsRequest
}

func NewScraper(ctx context.Context, services []string) *scraper {
	s := &scraper{
		services: services,
		ready:    make(chan struct{}),
		done:     make(chan struct{}),
		bk: &boundsKeeper{
			stream:  make(chan *result),
			request: make(chan *boundsRequest),
		},
	}
	go s.serve(ctx)
	go s.bk.serve(ctx)
	return s
}

func (s *scraper) serve(ctx context.Context) {

	sem := make(chan struct{}, 8)

	check := func(ctx context.Context, wg *sync.WaitGroup, sem chan struct{}, e *entry) {
		defer wg.Done()
		// This sem is guaranteeing that there are no more than N fetch operations running.
		// It isn't guaranteeing thath there are no more than N goroutines running.
		sem <- struct{}{}
		e.check(ctx)
		s.bk.send(ctx, &e.res)
		<-sem
	}

	cctx, cancel := context.WithCancel(ctx)
	// ticker := time.NewTicker(time.Minute)
	ticker := time.NewTicker(30 * time.Second)

	m := make(Map)
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
		go check(cctx, wg, sem, e)
	}
	go s.bk.terminate(ctx, wg)

	for {
		select {
		case <-ticker.C:
			cancel()
			wg.Wait()
			m = make(Map)
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
			// wg = new(sync.WaitGroup)
			for _, e := range m {
				wg.Add(1)
				go check(cctx, wg, sem, e)
			}
			go s.bk.terminate(ctx, wg)

		case <-ctx.Done():
			cancel()
			ticker.Stop()
			close(s.done)
			return
		}
		cctx, cancel = context.WithCancel(ctx)
	}
}

func (s *boundsKeeper) send(ctx context.Context, res *result) {
	select {
	case s.stream <- res:
	case <-ctx.Done():
	}
}

func (s *boundsKeeper) terminate(ctx context.Context, wg *sync.WaitGroup) {
	wg.Wait()
	select {
	case s.stream <- nil:
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
				}
			case boundsMax:
				if max != nil {
					resp.res = *max
				}
			}
			req.response <- resp

		case <-ctx.Done():
			return
		}
	}
}

// GetResponseTime returns response time of the service identified by name.
// If err != nil, service is not accessible.
func (s *scraper) GetResponseTime(name string) (time.Duration, error) {
	<-s.ready
	e := s.cache.Load().(Map)[name]
	if e == nil {
		return 0, ErrNotFound
	}
	res := e.getResult()
	return res.respTime, res.err
}

func (s *scraper) GetMin() (total int, res result) {
	return s.getBoundary(boundsMin)
}

func (s *scraper) GetMax() (total int, res result) {
	return s.getBoundary(boundsMax)
}

func (s *scraper) getBoundary(t boundsType) (total int, res result) {
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
	url := "https://" + name
	// in perfect world we should use http.MethodHead, but not all services implement it
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
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
