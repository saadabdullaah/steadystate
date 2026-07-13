package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"STEADYSTATE_APP_NAME":      "payments",
		"STEADYSTATE_APP_NAMESPACE": "team-a",
		"STEADYSTATE_APP_OWNER":     "payments-team",
		"STEADYSTATE_APP_VERSION":   "v1.2.3",
		"PORT":                      "9090",
	}
	configuration, err := loadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Name != "payments" || configuration.Namespace != "team-a" || configuration.Owner != "payments-team" || configuration.Version != "v1.2.3" || configuration.Port != 9090 {
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

func TestEndpoints(t *testing.T) {
	t.Parallel()
	ready := &atomic.Bool{}
	ready.Store(true)
	handler := newHandler(config{Name: "demo", Namespace: "apps", Owner: "platform", Version: "v0.1.0"}, ready)

	for _, test := range []struct {
		path   string
		status int
		state  string
	}{
		{path: "/", status: http.StatusOK, state: "running"},
		{path: "/healthz", status: http.StatusOK, state: "healthy"},
		{path: "/readyz", status: http.StatusOK, state: "ready"},
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
			if body.Application != "demo" || body.Version != "v0.1.0" || body.Status != test.state {
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
