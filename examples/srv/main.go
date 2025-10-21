// Package main provides an example overlay services HTTP server implementation.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/config"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/config/loaders"
)

func main() {
	if err := execute(); err != nil {
		log.Fatal(err)
	}
}

func execute() error {
	configPath := flag.String("config", loaders.DefaultConfigFilePath, "Path to the configuration file")
	flag.Parse()

	cfg, err := config.LoadFromPath(*configPath, "OVERLAY")
	if err != nil {
		return fmt.Errorf("load config op failed: %w", err)
	}

	ctx := context.Background()
	srv := server.New(server.WithConfig(cfg))
	done := make(chan struct{})

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// We received an interrupt signal, shut down.
		if shutdownErr := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("http server shutdown err: %v", shutdownErr)
		}
		close(done)
	}()

	err = srv.ListenAndServe(ctx)
	<-done
	if !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server listen and serve op failure: %w", err)
	}

	return nil
}
