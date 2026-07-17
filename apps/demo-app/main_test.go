package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"STEADYSTATE_APP_NAME":      "payments",
		"STEADYSTATE_APP_NAMESPACE": "team-a",
		"STEADYSTATE_APP_OWNER":     "payments-team",
		"STEADYSTATE_APP_VERSION":   "v1.2.3",
		"PORT":                      "9090",
		"INJECT_ERROR_RATE":         "0.125",
		"INJECT_LATENCY_MS":         "250",
		"CRASH_AFTER_REQUESTS":      "42",
	}
	configuration, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Name != "payments" || configuration.Namespace != "team-a" || configuration.Owner != "payments-team" ||
		configuration.Version != "v1.2.3" || configuration.Port != 9090 || configuration.InjectErrorRate != 0.125 ||
		configuration.InjectLatency != 250*time.Millisecond || configuration.CrashAfterRequests != 42 {
		t.Fatalf("unexpected configuration: %#v", configuration)
	}
}

func TestLoadConfigRejectsInvalidPort(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"nope", "0", "65536"} {
		if _, err := loadConfig(func(key string) string {
			if key == "PORT" {
				return value
			}
			return ""
		}); err == nil {
			t.Errorf("PORT=%q was accepted", value)
		}
	}
}

func TestLoadConfigInjectionBoundaries(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"INJECT_ERROR_RATE":    "1",
		"INJECT_LATENCY_MS":    "60000",
		"CRASH_AFTER_REQUESTS": "0",
	}
	configuration, err := loadConfig(func(key string) string { return valid[key] })
	if err != nil {
		t.Fatal(err)
	}
	if configuration.InjectErrorRate != 1 || configuration.InjectLatency != 60*time.Second || configuration.CrashAfterRequests != 0 {
		t.Fatalf("unexpected boundary configuration: %#v", configuration)
	}

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "negative error rate", key: "INJECT_ERROR_RATE", value: "-0.01"},
		{name: "error rate above one", key: "INJECT_ERROR_RATE", value: "1.01"},
		{name: "NaN error rate", key: "INJECT_ERROR_RATE", value: "NaN"},
		{name: "infinite error rate", key: "INJECT_ERROR_RATE", value: "+Inf"},
		{name: "non-decimal error rate", key: "INJECT_ERROR_RATE", value: "invalid"},
		{name: "negative latency", key: "INJECT_LATENCY_MS", value: "-1"},
		{name: "latency above maximum", key: "INJECT_LATENCY_MS", value: "60001"},
		{name: "fractional latency", key: "INJECT_LATENCY_MS", value: "1.5"},
		{name: "negative crash threshold", key: "CRASH_AFTER_REQUESTS", value: "-1"},
		{name: "fractional crash threshold", key: "CRASH_AFTER_REQUESTS", value: "1.5"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := loadConfig(func(key string) string {
				if key == test.key {
					return test.value
				}
				return ""
			}); err == nil {
				t.Fatalf("%s=%q was accepted", test.key, test.value)
			}
		})
	}
}

func TestEndpoints(t *testing.T) {
	t.Parallel()
	ready := &atomic.Bool{}
	ready.Store(true)
	handler := newHandler(config{Name: "demo", Namespace: "apps", Owner: "platform", Version: "v0.4.0"}, ready)

	for _, test := range []struct {
		path   string
		status int
		state  string
	}{
		{path: "/", status: http.StatusOK, state: "running"},
		{path: "/healthz", status: http.StatusOK, state: "healthy"},
		{path: "/readyz", status: http.StatusOK, state: "ready"},
		{path: "/metrics", status: http.StatusOK},
		{path: "/missing", status: http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.status {
			t.Errorf("%s returned %d, want %d", test.path, recorder.Code, test.status)
		}
		if test.state != "" {
			body := response{}
			if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Application != "demo" || body.Version != "v0.4.0" || body.Status != test.state {
				t.Errorf("%s returned unexpected body %#v", test.path, body)
			}
		}
	}

	ready.Store(false)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("not-ready endpoint returned %d", recorder.Code)
	}
}

func TestDeterministicFailureRatios(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		rate         float64
		requests     int
		wantFailures int
	}{
		{name: "disabled", rate: 0, requests: 100, wantFailures: 0},
		{name: "ten percent", rate: 0.10, requests: 100, wantFailures: 10},
		{name: "twenty five percent", rate: 0.25, requests: 100, wantFailures: 25},
		{name: "all", rate: 1, requests: 100, wantFailures: 100},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := testRuntimeHandler(config{InjectErrorRate: test.rate}, runtimeHooks{})
			failures := 0
			for range test.requests {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
				if recorder.Code == http.StatusInternalServerError {
					failures++
				}
			}
			if failures != test.wantFailures {
				t.Fatalf("observed %d failures, want %d", failures, test.wantFailures)
			}
		})
	}
}

func TestConcurrentFailureRatioIsRaceSafe(t *testing.T) {
	t.Parallel()
	handler := testRuntimeHandler(config{InjectErrorRate: 0.10}, runtimeHooks{})
	var failures atomic.Int32
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code == http.StatusInternalServerError {
				failures.Add(1)
			}
		}()
	}
	wait.Wait()
	if failures.Load() != 10 {
		t.Fatalf("observed %d concurrent failures, want 10", failures.Load())
	}
}

func TestHealthAndMetricsExcludeInjectionAndMeasurements(t *testing.T) {
	t.Parallel()
	var sleeps atomic.Int32
	var exits atomic.Int32
	handler := testRuntimeHandler(config{
		Name: "demo", Namespace: "apps", Version: "v0.4.0",
		InjectErrorRate: 1, InjectLatency: time.Second, CrashAfterRequests: 1,
	}, runtimeHooks{
		sleep: func(duration time.Duration) {
			if duration != time.Second {
				t.Errorf("slept for %s, want 1s", duration)
			}
			sleeps.Add(1)
		},
		exit: func(code int) {
			if code != 1 {
				t.Errorf("exit code %d, want 1", code)
			}
			exits.Add(1)
		},
	})
	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, recorder.Code)
		}
	}
	if sleeps.Load() != 0 || exits.Load() != 0 {
		t.Fatalf("excluded endpoints triggered injection: sleeps=%d exits=%d", sleeps.Load(), exits.Load())
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusInternalServerError || sleeps.Load() != 1 || exits.Load() != 1 {
		t.Fatalf("application request did not trigger configured injection: status=%d sleeps=%d exits=%d", recorder.Code, sleeps.Load(), exits.Load())
	}

	metrics := httptest.NewRecorder()
	handler.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(metrics.Body.String(), `status="200"`) {
		t.Fatal("health or metrics requests were included in RED measurements")
	}
}

func TestREDMetricsCountersAndHistograms(t *testing.T) {
	t.Parallel()
	handler := testRuntimeHandler(config{Name: "demo", Namespace: "apps", Version: "v0.4.0", InjectErrorRate: 0.5}, runtimeHooks{})
	for range 4 {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	metrics := recorder.Body.String()
	for _, expected := range []string{
		`http_requests_total{application="demo",namespace="apps",status="200",version="v0.4.0"} 2`,
		`http_requests_total{application="demo",namespace="apps",status="500",version="v0.4.0"} 2`,
		`http_request_duration_seconds_count{application="demo",namespace="apps",status="200",version="v0.4.0"} 2`,
		`http_request_duration_seconds_count{application="demo",namespace="apps",status="500",version="v0.4.0"} 2`,
	} {
		if !strings.Contains(metrics, expected) {
			t.Errorf("metrics output is missing %q\n%s", expected, metrics)
		}
	}
}

func TestCrashThresholdIsOneShot(t *testing.T) {
	t.Parallel()
	var exits atomic.Int32
	handler := testRuntimeHandler(config{CrashAfterRequests: 3}, runtimeHooks{exit: func(int) { exits.Add(1) }})
	for _, path := range []string{"/healthz", "/metrics", "/", "/"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	}
	if exits.Load() != 0 {
		t.Fatalf("crashed before the third measured request: %d exits", exits.Load())
	}
	for range 2 {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	}
	if exits.Load() != 1 {
		t.Fatalf("crash threshold produced %d exits, want 1", exits.Load())
	}
}

func testRuntimeHandler(configuration config, hooks runtimeHooks) http.Handler {
	ready := &atomic.Bool{}
	ready.Store(true)
	return newHandlerWithRuntime(configuration, ready, hooks)
}
