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
	AvgRespTime  time.Duration `json:"-"`
	LastRespTime time.Duration `json:"-"`
	LastError    error         `json:"-"`
	ErrRate      float32       `json:"err_rate"`
}

func (rd *Service) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

type ServiceBoundsResponse struct {
	Min *ServiceQueryResponse `json:"min"`
	Max *ServiceQueryResponse `json:"max"`
}

func (rd *ServiceBoundsResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

type ServiceQueryResponse struct {
	*Service
	AvgRespTime  int64  `json:"avg_resp_ms"`
	LastRespTime int64  `json:"last_resp_ms"`
	LastError    string `json:"last_error,omitempty"`
}

func (rd *ServiceQueryResponse) Render(w http.ResponseWriter, r *http.Request) error {
	rd.AvgRespTime = int64(rd.Service.AvgRespTime / time.Millisecond)
	rd.LastRespTime = int64(rd.Service.LastRespTime / time.Millisecond)
	if err := rd.Service.LastError; err != nil {
		rd.LastError = err.Error()
	}
	return nil
}

func (s *server) queryService(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "serviceName")
	resp, err := s.scraper.GetServiceResponse(name)
	if err != nil {
		if err == scraper.ErrNotFound {
			render.Render(w, r, ErrNotFound)
		}
		render.Render(w, r, ErrRender(err))
		return
	}
	payload := &ServiceQueryResponse{
		Service: &Service{
			Name:         resp.Name,
			AvgRespTime:  resp.AvgRespTime,
			LastRespTime: resp.LastRespTime,
			LastError:    resp.LastError,
			ErrRate:      resp.ErrRate,
		},
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
	}
}

func (s *server) queryBounds(w http.ResponseWriter, r *http.Request) {
	resp, err := s.scraper.GetBounds()
	if err != nil {
		if err == scraper.ErrNotReady {
			render.Render(w, r, ErrRender(err))
		}
		render.Render(w, r, ErrRender(err))
		return
	}
	payload := &ServiceBoundsResponse{
		Min: &ServiceQueryResponse{
			Service: &Service{
				Name:         resp.Min.Name,
				AvgRespTime:  resp.Min.AvgRespTime,
				LastRespTime: resp.Min.LastRespTime,
				LastError:    resp.Min.LastError,
				ErrRate:      resp.Min.ErrRate,
			},
		},
		Max: &ServiceQueryResponse{
			Service: &Service{
				Name:         resp.Max.Name,
				AvgRespTime:  resp.Max.AvgRespTime,
				LastRespTime: resp.Max.LastRespTime,
				LastError:    resp.Max.LastError,
				ErrRate:      resp.Max.ErrRate,
			},
		},
	}
	if err := render.Render(w, r, payload); err != nil {
		render.Render(w, r, ErrRender(err))
	}
}
