// Package main provides the SteadyState demonstration workload.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	Name               string
	Namespace          string
	Owner              string
	Version            string
	Port               int
	InjectErrorRate    float64
	InjectLatency      time.Duration
	CrashAfterRequests uint64
	OTLPEndpoint       string
}

type runtimeHooks struct {
	sleep          func(time.Duration)
	exit           func(int)
	log            *slog.Logger
	tracerProvider trace.TracerProvider
}

type requestRuntime struct {
	configuration config
	metrics       *demoMetrics
	hooks         runtimeHooks
	sequence      atomic.Uint64
	crashed       atomic.Bool
}

type demoMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	handler  http.Handler
}

type response struct {
	Application string `json:"application"`
	Namespace   string `json:"namespace"`
	Owner       string `json:"owner,omitempty"`
	Status      string `json:"status"`
	Version     string `json:"version"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
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
		Name:         valueOrDefault(getenv("STEADYSTATE_APP_NAME"), "steadystate-demo"),
		Namespace:    valueOrDefault(getenv("STEADYSTATE_APP_NAMESPACE"), "local"),
		Owner:        valueOrDefault(getenv("STEADYSTATE_APP_OWNER"), "local-developer"),
		Version:      valueOrDefault(getenv("STEADYSTATE_APP_VERSION"), "development"),
		Port:         8080,
		OTLPEndpoint: getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	}
	if rawPort := getenv("PORT"); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
		configuration.Port = port
	}
	if rawRate := getenv("INJECT_ERROR_RATE"); rawRate != "" {
		rate, err := strconv.ParseFloat(rawRate, 64)
		if err != nil || math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 1 {
			return config{}, fmt.Errorf("INJECT_ERROR_RATE must be a decimal between 0 and 1")
		}
		configuration.InjectErrorRate = rate
	}
	if rawLatency := getenv("INJECT_LATENCY_MS"); rawLatency != "" {
		latencyMilliseconds, err := strconv.ParseInt(rawLatency, 10, 64)
		if err != nil || latencyMilliseconds < 0 || latencyMilliseconds > 60000 {
			return config{}, fmt.Errorf("INJECT_LATENCY_MS must be an integer between 0 and 60000")
		}
		configuration.InjectLatency = time.Duration(latencyMilliseconds) * time.Millisecond
	}
	if rawCrashThreshold := getenv("CRASH_AFTER_REQUESTS"); rawCrashThreshold != "" {
		crashThreshold, err := strconv.ParseUint(rawCrashThreshold, 10, 64)
		if err != nil {
			return config{}, fmt.Errorf("CRASH_AFTER_REQUESTS must be a non-negative integer")
		}
		configuration.CrashAfterRequests = crashThreshold
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
	shutdownTracing, err := configureTracing(ctx, configuration)
	if err != nil {
		return err
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if shutdownErr := shutdownTracing(shutdownContext); shutdownErr != nil {
			slog.Error("flush tracing", "error", shutdownErr)
		}
	}()
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
	return newHandlerWithRuntime(configuration, ready, runtimeHooks{sleep: time.Sleep, exit: os.Exit, log: slog.Default()})
}

func newHandlerWithRuntime(configuration config, ready *atomic.Bool, hooks runtimeHooks) http.Handler {
	if hooks.sleep == nil {
		hooks.sleep = time.Sleep
	}
	if hooks.exit == nil {
		hooks.exit = os.Exit
	}
	if hooks.log == nil {
		hooks.log = slog.Default()
	}
	if hooks.tracerProvider == nil {
		hooks.tracerProvider = otel.GetTracerProvider()
	}
	metrics := newDemoMetrics(configuration)
	runtime := &requestRuntime{configuration: configuration, metrics: metrics, hooks: hooks}
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
	mux.Handle("/metrics", metrics.handler)
	applicationHandler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		trace.SpanFromContext(request.Context()).SetAttributes(attribute.String("http.route", "/"))
		runtime.serveApplication(writer, request)
	})
	propagators := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	mux.Handle("/", otelhttp.NewHandler(applicationHandler, "HTTP /", otelhttp.WithTracerProvider(hooks.tracerProvider), otelhttp.WithPropagators(propagators)))
	return mux
}

func newDemoMetrics(configuration config) *demoMetrics {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total measured application HTTP requests.",
	}, []string{"application", "namespace", "version", "status"})
	duration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Measured application HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"application", "namespace", "version", "status"})
	registry := prometheus.NewRegistry()
	registry.MustRegister(requests, duration)
	return &demoMetrics{
		requests: requests,
		duration: duration,
		handler:  promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}
}

func (runtime *requestRuntime) serveApplication(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = newRequestID()
	}
	writer.Header().Set("X-Request-ID", requestID)
	sequence := runtime.sequence.Add(1)
	if runtime.configuration.InjectLatency > 0 {
		runtime.hooks.sleep(runtime.configuration.InjectLatency)
	}

	statusCode := http.StatusOK
	state := "running"
	if shouldInjectFailure(sequence, runtime.configuration.InjectErrorRate) {
		statusCode = http.StatusInternalServerError
		state = "error"
	}
	status := strconv.Itoa(statusCode)
	runtime.metrics.requests.WithLabelValues(runtime.configuration.Name, runtime.configuration.Namespace, runtime.configuration.Version, status).Inc()
	elapsed := time.Since(started)
	runtime.metrics.duration.WithLabelValues(runtime.configuration.Name, runtime.configuration.Namespace, runtime.configuration.Version, status).Observe(elapsed.Seconds())
	writeJSON(writer, statusCode, response{
		Application: runtime.configuration.Name,
		Namespace:   runtime.configuration.Namespace,
		Owner:       runtime.configuration.Owner,
		Status:      state,
		Version:     runtime.configuration.Version,
	})
	spanContext := trace.SpanContextFromContext(request.Context())
	runtime.hooks.log.InfoContext(request.Context(), "http request",
		"request_id", requestID,
		"trace_id", spanContext.TraceID().String(),
		"span_id", spanContext.SpanID().String(),
		"method", request.Method,
		"route", "/",
		"status", statusCode,
		"latency_ms", float64(elapsed.Microseconds())/1000,
		"application", runtime.configuration.Name,
		"namespace", runtime.configuration.Namespace,
		"version", runtime.configuration.Version,
	)

	if runtime.configuration.CrashAfterRequests > 0 && sequence >= runtime.configuration.CrashAfterRequests && runtime.crashed.CompareAndSwap(false, true) {
		runtime.hooks.exit(1)
	}
}

func newRequestID() string {
	identifier := make([]byte, 16)
	if _, err := rand.Read(identifier); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(identifier)
}

func configureTracing(ctx context.Context, configuration config) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if configuration.OTLPEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(configuration.OTLPEndpoint), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	platformResource, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", configuration.Name),
		attribute.String("service.namespace", configuration.Namespace),
		attribute.String("service.version", configuration.Version),
		attribute.String("steadystate.application", configuration.Name),
		attribute.String("k8s.namespace.name", configuration.Namespace),
	))
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(platformResource),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

func shouldInjectFailure(sequence uint64, rate float64) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	return math.Floor(float64(sequence)*rate) > math.Floor(float64(sequence-1)*rate)
}

func writeJSON(writer http.ResponseWriter, statusCode int, value response) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		slog.Error("write response", "error", err)
	}
}
