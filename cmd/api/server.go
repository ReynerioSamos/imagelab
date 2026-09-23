package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Week 2: Gracefully stops both the background worker goroutines
// and the HTTP server during shutdown.
func (app *application) serve() error {
	srv := &http.Server{
		Addr:        fmt.Sprintf(":%d", app.config.port),
		Handler:     app.routes(),
		IdleTimeout: time.Minute,
		// ReadTimeout is generous because it must cover the upload of a
		// file up to 10 MB over a slow connection, not just header parsing.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	shutdownError := make(chan error)

	workerCtx, cancelWorker := context.WithCancel(context.Background())

	// Start single background worker
	app.worker.Start(workerCtx)

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		s := <-quit

		app.logger.Info("caught signal", "signal", s.String())

		// Cancel worker first, wait second, then stop HTTP server.
		cancelWorker()
		app.worker.Stop()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		shutdownError <- srv.Shutdown(ctx)
	}()

	app.logger.Info("starting server", "addr", srv.Addr, "env", app.config.env)

	err := srv.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	if err := <-shutdownError; err != nil {
		return err
	}

	app.logger.Info("stopped server", "addr", srv.Addr)
	return nil
}