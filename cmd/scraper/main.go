package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "net/http/pprof"

	scraper "github.com/vbauerster/scraper-test"
	"github.com/vbauerster/scraper-test/app"
)

var (
	gendoc   = flag.Bool("gendoc", false, "Generate router documentation")
	src      = flag.String("src", "sites.txt", "Services source file")
	numFetch = flag.Int("num-fetch", 0, "concurrent fetch, default is NumCPU")
	port     = flag.String("port", "8080", "http port to listen on")
	pprof    = flag.String("pprof", "", "start pprof server on provided port")
)

func main() {
	flag.Parse()

	services, err := scraper.ReadFrom(*src)
	if err != nil {
		log.Fatal(err)
	}

	// Start pprof server, if needed
	if *pprof != "" {
		go func() {
			log.Println(http.ListenAndServe(":"+*pprof, nil))
		}()
	}

	ctx := backgroundContext()
	server := app.New(scraper.NewScraper(ctx, services, time.Minute, *numFetch), *gendoc)
	server.Serve(ctx, ":"+*port, 5*time.Second)
}

func backgroundContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		signal.Stop(quit)
		cancel()
	}()

	return ctx
}
