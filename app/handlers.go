package app

import (
	"net/http"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	scraper "github.com/vbauerster/scraper-test"
)

// Base model
type Service struct {
	Name         string        `json:"name"`
	Milliseconds int64         `json:"response_time_ms"`
	Alive        bool          `json:"is_alive"`
	RespTime     time.Duration `json:"-"`
	Err          error         `json:"-"`
}

func (rd *Service) Render(w http.ResponseWriter, r *http.Request) error {
	if rd.Err == nil {
		rd.Milliseconds = int64(rd.RespTime / time.Millisecond)
		rd.Alive = true
	}
	return nil
}

type ServiceBoundsResponse struct {
	*Service
	TotalOk  int `json:"total_ok"`
	TotalErr int `json:"total_err"`
}

type ServiceQueryResponse struct {
	*Service
	ErrorText string `json:"error_text,omitempty"`
}

func (rd *ServiceQueryResponse) Render(w http.ResponseWriter, r *http.Request) error {
	if rd.Err != nil {
		rd.ErrorText = rd.Err.Error()
	}
	return nil
}

func servicesResponse(services []*scraper.ServiceResponse) []render.Renderer {
	errServices := make([]render.Renderer, 0, len(services))
	for _, s := range services {
		errServices = append(errServices, &ServiceQueryResponse{
			Service: &Service{
				Name:     s.Name,
				RespTime: s.RespTime,
				Err:      s.Error,
			},
		})
	}
	return errServices
}

// func (s *server) listServices(w http.ResponseWriter, r *http.Request) {
// 	services := s.scraper.GetServices()
// 	if err := render.RenderList(w, r, servicesResponse(services)); err != nil {
// 		render.Render(w, r, ErrRender(err))
// 		return
// 	}
// }

func (s *server) listErrorServices(w http.ResponseWriter, r *http.Request) {
	services := s.scraper.GetErrorServices()
	if err := render.RenderList(w, r, servicesResponse(services)); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (s *server) queryService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "serviceName")
	resp, err := s.scraper.GetServiceResponse(name)
	// spew.Dump(resp)
	if err != nil {
		if err == scraper.ErrNotFound {
			render.Render(w, r, ErrNotFound)
		}
		render.Render(w, r, ErrRender(err))
		return
	}
	payload := &ServiceQueryResponse{
		Service: &Service{
			Name:     resp.Name,
			RespTime: resp.RespTime,
			Err:      resp.Error,
		},
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (s *server) boundsMin(w http.ResponseWriter, r *http.Request) {
	resp, err := s.scraper.GetMin()
	if err != nil {
		if err == scraper.ErrNotReady {
			render.Render(w, r, ErrRender(err))
		}
		render.Render(w, r, ErrRender(err))
		return
	}
	payload := &ServiceBoundsResponse{
		Service: &Service{
			Name:     resp.Name,
			RespTime: resp.RespTime,
		},
		TotalOk:  resp.TotalOk,
		TotalErr: resp.TotalErr,
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}

func (s *server) boundsMax(w http.ResponseWriter, r *http.Request) {
	resp, err := s.scraper.GetMax()
	if err != nil {
		if err == scraper.ErrNotReady {
			render.Render(w, r, ErrRender(err))
		}
		render.Render(w, r, ErrRender(err))
		return
	}
	payload := &ServiceBoundsResponse{
		Service: &Service{
			Name:     resp.Name,
			RespTime: resp.RespTime,
		},
		TotalOk:  resp.TotalOk,
		TotalErr: resp.TotalErr,
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
		return
	}
}
