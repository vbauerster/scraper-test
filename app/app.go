package app

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/docgen"
	scraper "github.com/vbauerster/scraper-test"
)

type server struct {
	router  *chi.Mux
	scraper *scraper.Scraper
}

func New(scraper *scraper.Scraper, gendoc bool) *server {
	s := &server{
		router:  chi.NewRouter(),
		scraper: scraper,
	}
	s.initRoutes()

	if gendoc {
		md := docgen.MarkdownRoutesDoc(s.router, docgen.MarkdownOpts{
			ProjectPath: "github.com/vbauerster/scraper-test",
			Intro:       "scraper-test REST API.",
		})
		if err := ioutil.WriteFile("routes.md", []byte(md), 0644); err != nil {
			log.Println(err)
		}
	}

	return s
}

func (s *server) Serve(ctx context.Context, addr string, shutdownTimeout time.Duration) {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		<-ctx.Done()
		tctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer func() {
			cancel()
			close(idleConnsClosed)
		}()
		if err := srv.Shutdown(tctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("Can't run server: %v", err)
	}
	<-idleConnsClosed
}
