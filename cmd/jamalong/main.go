// Command jamalong runs the jamalong room server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urmzd/jamalong/internal/provider"
	"github.com/urmzd/jamalong/internal/server"
)

var version, commit, date string

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("jamalong %s (%s, %s)\n", orDev(version), orDev(commit), orDev(date))
		return
	}
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on")
	web := flag.String("web", "", "directory of the built web app to serve (empty serves the API only)")
	secure := flag.Bool("secure-cookies", false, "mark session cookies Secure; set this behind HTTPS")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*addr, *web, *secure, log); err != nil {
		log.Error("jamalong stopped", "err", err)
		os.Exit(1)
	}
}

func run(addr, web string, secure bool, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(server.Config{
		Resolver:      provider.YouTube{},
		WebDir:        web,
		SecureCookies: secure,
		Logger:        log,
	})
	go srv.Run(ctx)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	failed := make(chan error, 1)
	go func() { failed <- httpSrv.ListenAndServe() }()
	log.Info("listening", "addr", addr, "web", web)

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdown); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func orDev(s string) string {
	if s == "" {
		return "dev"
	}
	return s
}
