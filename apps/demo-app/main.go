// Package main provides the SteadyState demonstration workload.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	Name      string
	Namespace string
	Owner     string
	Version   string
	Port      int
}

type response struct {
	Application string `json:"application"`
	Namespace   string `json:"namespace"`
	Owner       string `json:"owner,omitempty"`
	Status      string `json:"status"`
	Version     string `json:"version"`
}

func main() {
	configuration, err := loadConfig(os.Getenv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, configuration); err != nil {
		slog.Error("demo application stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig(getenv func(string) string) (config, error) {
	configuration := config{
		Name:      valueOrDefault(getenv("STEADYSTATE_APP_NAME"), "steadystate-demo"),
		Namespace: valueOrDefault(getenv("STEADYSTATE_APP_NAMESPACE"), "local"),
		Owner:     valueOrDefault(getenv("STEADYSTATE_APP_OWNER"), "local-developer"),
		Version:   valueOrDefault(getenv("STEADYSTATE_APP_VERSION"), "development"),
		Port:      8080,
	}
	if rawPort := getenv("PORT"); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
		configuration.Port = port
	}
	return configuration, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func run(ctx context.Context, configuration config) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", configuration.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", configuration.Port, err)
	}
	ready := &atomic.Bool{}
	server := &http.Server{
		Handler:           newHandler(configuration, ready),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	errorsChannel := make(chan error, 1)
	ready.Store(true)
	slog.Info("demo application listening", "address", listener.Addr(), "application", configuration.Name, "version", configuration.Version)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errorsChannel <- serveErr
			return
		}
		errorsChannel <- nil
	}()

	select {
	case err := <-errorsChannel:
		return err
	case <-ctx.Done():
		ready.Store(false)
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return <-errorsChannel
	}
}

func newHandler(configuration config, ready *atomic.Bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, response{Application: configuration.Name, Namespace: configuration.Namespace, Status: "healthy", Version: configuration.Version})
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			writeJSON(writer, http.StatusServiceUnavailable, response{Application: configuration.Name, Namespace: configuration.Namespace, Status: "not-ready", Version: configuration.Version})
			return
		}
		writeJSON(writer, http.StatusOK, response{Application: configuration.Name, Namespace: configuration.Namespace, Status: "ready", Version: configuration.Version})
	})
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		writeJSON(writer, http.StatusOK, response{Application: configuration.Name, Namespace: configuration.Namespace, Owner: configuration.Owner, Status: "running", Version: configuration.Version})
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, statusCode int, value response) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		slog.Error("write response", "error", err)
	}
}
