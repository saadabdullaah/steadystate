package platformctl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testPortalServer(t *testing.T) *portalServer {
	t.Helper()
	server, err := newPortalServer(&Options{Build: BuildInfo{Version: "v1.0.0"}}, Context{Repository: "saadabdullaah/steadystate", CheckoutPath: t.TempDir(), Profile: "full", HTTPPort: 8080}, "test")
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestPortalLaunchIsOneTimeAndCreatesStrictSession(t *testing.T) {
	server := testPortalServer(t)
	handler := server.handler()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/launch/"+server.launchToken, nil)
	request.Host = "127.0.0.1:4173"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("launch status=%d body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe session cookie: %#v", cookies)
	}
	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodGet, request.URL.String(), nil)
	replayRequest.Host = request.Host
	handler.ServeHTTP(replay, replayRequest)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("launch replay status=%d", replay.Code)
	}
}

func TestPortalRejectsHostAndCSRFViolations(t *testing.T) {
	server := testPortalServer(t)
	handler := server.handler()
	cookie := &http.Cookie{Name: "steadystate_portal_session", Value: server.session}
	badHost := httptest.NewRequest(http.MethodGet, "http://evil.example/api/v1/meta", nil)
	badHost.Host = "evil.example"
	badHost.AddCookie(cookie)
	badHostResponse := httptest.NewRecorder()
	handler.ServeHTTP(badHostResponse, badHost)
	if badHostResponse.Code != http.StatusForbidden {
		t.Fatalf("bad host status=%d", badHostResponse.Code)
	}
	badCSRF := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/plans", strings.NewReader(`{}`))
	badCSRF.Host = "127.0.0.1"
	badCSRF.Header.Set("Content-Type", "application/json")
	badCSRF.Header.Set("Origin", "http://127.0.0.1")
	badCSRF.AddCookie(cookie)
	badCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(badCSRFResponse, badCSRF)
	if badCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("bad csrf status=%d", badCSRFResponse.Code)
	}
}

func TestPortalMetaContainsNoCredentialMaterial(t *testing.T) {
	server := testPortalServer(t)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/meta", nil)
	request.Host = "127.0.0.1"
	request.AddCookie(&http.Cookie{Name: "steadystate_portal_session", Value: server.session})
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("meta status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"privatekey", "kubeconfig", "sops_age_key", "github_token"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("meta leaked forbidden key %q", forbidden)
		}
	}
}

func TestPortalServesEmbeddedIndexWithoutCanonicalRedirect(t *testing.T) {
	server := testPortalServer(t)
	for _, path := range []string{"/", "/teams/payments"} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil)
		request.Host = "127.0.0.1"
		request.AddCookie(&http.Cookie{Name: "steadystate_portal_session", Value: server.session})
		response := httptest.NewRecorder()
		server.handler().ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Location") != "" {
			t.Fatalf("path %s status=%d location=%q", path, response.Code, response.Header().Get("Location"))
		}
		if !strings.Contains(response.Body.String(), "<!doctype html>") {
			t.Fatalf("path %s did not return the embedded portal index", path)
		}
	}
}

func TestPortalSessionExpiresServerSide(t *testing.T) {
	server := testPortalServer(t)
	server.sessionUntil = time.Now().Add(-time.Second)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/meta", nil)
	request.Host = "127.0.0.1"
	request.AddCookie(&http.Cookie{Name: "steadystate_portal_session", Value: server.session})
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", response.Code)
	}
}

func TestPortalRejectsUntrustedResourcePath(t *testing.T) {
	server := testPortalServer(t)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/services/not_valid", nil)
	request.Host = "127.0.0.1"
	request.AddCookie(&http.Cookie{Name: "steadystate_portal_session", Value: server.session})
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid resource status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPortalStrictJSONHeadersAndRateLimit(t *testing.T) {
	server := testPortalServer(t)
	handler := server.handler()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/plans", strings.NewReader(`{"operation":"team.create","parameters":{},"unexpected":true}`))
	request.Host = "127.0.0.1"
	request.Header.Set("Origin", "http://127.0.0.1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-SteadyState-CSRF", server.csrf)
	request.AddCookie(&http.Cookie{Name: "steadystate_portal_session", Value: server.session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON status=%d body=%s", response.Code, response.Body.String())
	}
	for header, expected := range map[string]string{"X-Frame-Options": "DENY", "X-Content-Type-Options": "nosniff", "Referrer-Policy": "no-referrer", "Cache-Control": "no-store"} {
		if response.Header().Get(header) != expected {
			t.Fatalf("header %s=%q", header, response.Header().Get(header))
		}
	}
	server.writeTimes = nil
	for count := 0; count < 10; count++ {
		if !server.allowWrite() {
			t.Fatalf("write %d was rejected early", count)
		}
	}
	if server.allowWrite() {
		t.Fatal("eleventh write in one minute was allowed")
	}
}

func TestPortalEnforcesForceDeletionConfirmationAtBackend(t *testing.T) {
	server := testPortalServer(t)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/plans", strings.NewReader(`{"operation":"database.delete","parameters":{"team":"payments","name":"orders","force":true,"acknowledgeDataLoss":true}}`))
	request.Host = "127.0.0.1"
	request.Header.Set("Origin", "http://127.0.0.1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-SteadyState-CSRF", server.csrf)
	request.AddCookie(&http.Cookie{Name: "steadystate_portal_session", Value: server.session})
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "data_loss_confirmation_required") {
		t.Fatalf("force confirmation status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPortalEventStreamRejectsMutatingMethods(t *testing.T) {
	server := testPortalServer(t)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/events", strings.NewReader(`{}`))
	request.Host = "127.0.0.1"
	request.Header.Set("Origin", "http://127.0.0.1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-SteadyState-CSRF", server.csrf)
	request.AddCookie(&http.Cookie{Name: "steadystate_portal_session", Value: server.session})
	response := httptest.NewRecorder()
	server.handler().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("event mutation status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPortalPlanExpirationAndCapacityCleanup(t *testing.T) {
	server := testPortalServer(t)
	server.plans["expired"] = &portalPlan{Expires: time.Now().Add(-time.Second)}
	server.breakPlans["expired"] = &portalBreakPlan{Expires: time.Now().Add(-time.Second)}
	server.mu.Lock()
	server.expirePlansLocked()
	server.mu.Unlock()
	if len(server.plans) != 0 || len(server.breakPlans) != 0 {
		t.Fatal("expired plans were retained")
	}
}

func TestPlatformLifecycleStagesPreserveDataBoundary(t *testing.T) {
	up := platformUpStages("full")
	expected := []string{"tools", "check-versions", "bootstrap", "start-backup-store", "build-images", "load-images", "deploy-gitops", "verify-gitops", "verify-data"}
	if len(up) != len(expected) {
		t.Fatalf("up stages=%v", up)
	}
	for index, name := range expected {
		if up[index].Command != name {
			t.Fatalf("stage %d=%s want %s", index, up[index].Command, name)
		}
	}
	down := platformDownStages("full")
	if down[len(down)-1].Command != "stop-backup-store" {
		t.Fatalf("down stages=%v", down)
	}
	for _, stage := range down {
		if strings.Contains(strings.ToLower(stage.Command), "purge") {
			t.Fatalf("unsafe purge stage: %v", stage)
		}
	}
}

func TestPortalAssetDigestIsStableAndCanonical(t *testing.T) {
	first, second := portalAssetsDigest(), portalAssetsDigest()
	if first != second || !strings.HasPrefix(first, "sha256:") || len(first) != 71 {
		t.Fatalf("asset digest %q / %q", first, second)
	}
}
