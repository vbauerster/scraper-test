package app

import (
	"net/http"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	"github.com/vbauerster/scraper-test"
)

type Service struct {
	Name string `json:"name"`
}

func (rd *Service) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

type ServiceQueryResponse struct {
	*Service
	Milliseconds int64  `json:"response_time_ms"`
	Alive        bool   `json:"is_alive"`
	ErrorText    string `json:"error_text,omitempty"`
	Participants int    `json:"participants,omitempty"`
	respTime     time.Duration
	err          error
}

func (rd *ServiceQueryResponse) Render(w http.ResponseWriter, r *http.Request) error {
	if rd.err != nil {
		rd.ErrorText = rd.err.Error()
	} else {
		rd.Milliseconds = int64(rd.respTime / time.Millisecond)
		rd.Alive = true
	}
	return nil
}

func servicesResponse(services []string) (payload []render.Renderer) {
	for i := range services {
		payload = append(payload, &Service{Name: services[i]})
	}
	return payload
}

func (s *server) listServices(w http.ResponseWriter, r *http.Request) {
	services := s.scraper.GetServices()
	if err := render.RenderList(w, r, servicesResponse(services)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (s *server) queryService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "serviceName")
	respTime, err := s.scraper.GetResponseTime(name)
	if err == scraper.ErrNotFound {
		render.Render(w, r, ErrNotFound)
		return
	}
	payload := &ServiceQueryResponse{
		Service:  &Service{Name: name},
		respTime: respTime,
		err:      err,
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (s *server) boundsMin(w http.ResponseWriter, r *http.Request) {
	resp, err := s.scraper.GetMin()
	if err == scraper.ErrNotReady {
		render.Render(w, r, ErrRender(err))
		return
	}
	payload := &ServiceQueryResponse{
		Service:      &Service{Name: resp.Name},
		Participants: resp.Participants,
		respTime:     resp.RespTime,
		err:          err,
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (s *server) boundsMax(w http.ResponseWriter, r *http.Request) {
	resp, err := s.scraper.GetMax()
	if err == scraper.ErrNotReady {
		render.Render(w, r, ErrRender(err))
		return
	}
	payload := &ServiceQueryResponse{
		Service:      &Service{Name: resp.Name},
		Participants: resp.Participants,
		respTime:     resp.RespTime,
		err:          err,
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}
