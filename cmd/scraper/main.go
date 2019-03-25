package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vbauerster/scraper-test"
	"github.com/vbauerster/scraper-test/app"
)

var (
	gendoc = flag.Bool("gendoc", false, "Generate router documentation")
	src    = flag.String("src", "sites.txt", "Services source file")
	port   = flag.String("port", "8080", "http port to listen on")
)

func main() {
	flag.Parse()

	services, err := scraper.ReadFrom(*src)
	if err != nil {
		log.Fatal(err)
	}
	ctx := backgroundContext()
	server := app.New(scraper.NewScraper(ctx, services), *gendoc)
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
