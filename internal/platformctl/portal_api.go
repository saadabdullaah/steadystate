package platformctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type portalEnvelope struct {
	APIVersion string    `json:"apiVersion"`
	ObservedAt time.Time `json:"observedAt"`
	Data       any       `json:"data"`
	Warnings   []string  `json:"warnings,omitempty"`
}

type portalPlanInput struct {
	Operation            string           `json:"operation"`
	Parameters           ChangeParameters `json:"parameters"`
	DataLossConfirmation string           `json:"dataLossConfirmation,omitempty"`
}

type portalPlanView struct {
	PlanID         string    `json:"planID"`
	RequestID      string    `json:"requestID"`
	Operation      string    `json:"operation"`
	BaseSHA        string    `json:"baseSHA"`
	ProposalDigest string    `json:"proposalDigest"`
	RenderDigest   string    `json:"renderDigest"`
	ChangedPaths   []string  `json:"changedPaths"`
	Diff           string    `json:"diff"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

func (s *portalServer) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	if path == "meta" && r.Method == http.MethodGet {
		s.handleMeta(w)
		return
	}
	if path == "readiness" && r.Method == http.MethodGet {
		s.handleReadiness(w, r)
		return
	}
	if path == "overview" && r.Method == http.MethodGet {
		s.handleOverview(w, r)
		return
	}
	if path == "catalog" && r.Method == http.MethodGet {
		s.handleCatalog(w, r)
		return
	}
	if path == "teams" && r.Method == http.MethodGet {
		s.handleTeams(w, r)
		return
	}
	if path == "services" && r.Method == http.MethodGet {
		s.handleServices(w, r)
		return
	}
	if strings.HasPrefix(path, "services/") && r.Method == http.MethodGet {
		name := strings.TrimPrefix(path, "services/")
		if !s.requirePortalName(w, r, name, 48) {
			return
		}
		s.handleService(w, r, name)
		return
	}
	if path == "requests" && r.Method == http.MethodGet {
		s.handleRequests(w, r, "")
		return
	}
	if strings.HasPrefix(path, "requests/") && r.Method == http.MethodGet {
		s.handleRequests(w, r, strings.TrimPrefix(path, "requests/"))
		return
	}
	if path == "plans" && r.Method == http.MethodPost {
		s.handlePlan(w, r)
		return
	}
	if strings.HasPrefix(path, "plans/") && strings.HasSuffix(path, "/submit") && r.Method == http.MethodPost {
		s.handlePlanSubmit(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "plans/"), "/submit"))
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && parts[0] == "teams" {
		team := parts[1]
		if !s.requirePortalName(w, r, team, 58) {
			return
		}
		if len(parts) == 2 && r.Method == http.MethodGet {
			s.handleTeam(w, r, team)
			return
		}
		if len(parts) >= 4 && parts[2] == "applications" {
			application := parts[3]
			if !s.requirePortalName(w, r, application, 63) {
				return
			}
			if len(parts) == 4 && r.Method == http.MethodGet {
				s.handleApplication(w, r, team, application, "")
				return
			}
			if len(parts) == 5 && r.Method == http.MethodGet {
				s.handleApplication(w, r, team, application, parts[4])
				return
			}
			if len(parts) == 6 && parts[4] == "break-glass" && parts[5] == "plan" && r.Method == http.MethodPost {
				s.handleBreakGlassPlan(w, r, team, application)
				return
			}
			if len(parts) == 6 && parts[4] == "break-glass" && parts[5] == "execute" && r.Method == http.MethodPost {
				s.handleBreakGlassExecute(w, r, team, application)
				return
			}
		}
		if len(parts) >= 4 && parts[2] == "databases" {
			database := parts[3]
			if !s.requirePortalName(w, r, database, 63) {
				return
			}
			if len(parts) == 4 && r.Method == http.MethodGet {
				s.handleDatabase(w, r, team, database, false)
				return
			}
			if len(parts) == 5 && parts[4] == "backups" && r.Method == http.MethodGet {
				s.handleDatabase(w, r, team, database, true)
				return
			}
		}
	}
	s.writeError(w, r, http.StatusNotFound, "route_not_found", "portal API route was not found", "Use a documented portal action.")
}

func (s *portalServer) requirePortalName(w http.ResponseWriter, r *http.Request, name string, maximum int) bool {
	if validName(name, maximum) {
		return true
	}
	s.writeError(w, r, http.StatusBadRequest, "invalid_resource_name", "resource name is not a valid DNS label", "Use the exact catalog resource name.")
	return false
}

func (s *portalServer) envelope(data any, warnings ...string) portalEnvelope {
	return portalEnvelope{APIVersion: portalAPIVersion, ObservedAt: time.Now().UTC(), Data: data, Warnings: warnings}
}

func (s *portalServer) handleMeta(w http.ResponseWriter) {
	build := s.options.Build
	if build.PortalVersion == "" {
		build.PortalVersion = portalVersion
	}
	if build.PortalAssetsDigest == "" {
		build.PortalAssetsDigest = s.assetDigest
	}
	s.writeJSON(w, http.StatusOK, s.envelope(map[string]any{"build": build, "context": s.contextName, "profile": s.selected.Profile, "repository": s.selected.Repository, "csrfToken": s.csrf, "mode": "Local owner", "links": map[string]string{"argo": fmt.Sprintf("http://argocd.localtest.me:%d", s.selected.HTTPPort), "grafana": fmt.Sprintf("http://grafana.localtest.me:%d", s.selected.HTTPPort)}}))
}

func (s *portalServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	checks := runDoctor(ctx, s.selected)
	state := "Ready"
	for _, check := range checks {
		if check.Status == "Fail" {
			state = "Needs attention"
			break
		}
	}
	s.writeJSON(w, http.StatusOK, s.envelope(map[string]any{"state": state, "checks": checks}))
}

func (s *portalServer) handleCatalog(w http.ResponseWriter, r *http.Request) {
	catalog, err := LoadCatalog(s.selected.CheckoutPath)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, s.envelope(catalog))
}

func (s *portalServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	catalog, err := LoadCatalog(s.selected.CheckoutPath)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	applications, databases, services := 0, 0, 0
	for _, tenant := range catalog.Tenants {
		applications += len(tenant.Applications)
		databases += len(tenant.Databases)
		services += len(tenant.Services)
	}
	data := map[string]any{"context": s.contextName, "profile": s.selected.Profile, "repository": s.selected.Repository, "counts": map[string]int{"teams": len(catalog.Tenants), "services": services, "applications": applications, "databases": databases}, "health": "Unavailable", "resources": []ResourceSummary{}}
	warnings := []string{}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	client, clientErr := NewClusterClient(s.selected)
	if clientErr != nil {
		warnings = append(warnings, ErrorMessage(clientErr))
	} else {
		items, listErr := client.List(ctx, applicationGVR, "", "")
		if listErr != nil {
			warnings = append(warnings, ErrorMessage(listErr))
		} else {
			summaries := make([]ResourceSummary, 0, len(items))
			healthy := true
			for index := range items {
				summary := Summarize("Application", &items[index])
				summaries = append(summaries, summary)
				healthy = healthy && summary.Ready == "True"
			}
			data["resources"] = summaries
			if healthy {
				data["health"] = "Healthy"
			} else {
				data["health"] = "Needs attention"
			}
		}
	}
	s.writeJSON(w, http.StatusOK, s.envelope(data, warnings...))
}

func (s *portalServer) handleTeams(w http.ResponseWriter, r *http.Request) {
	catalog, err := LoadCatalog(s.selected.CheckoutPath)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, s.envelope(catalog.SortedTenants()))
}

func (s *portalServer) handleServices(w http.ResponseWriter, r *http.Request) {
	catalog, err := LoadCatalog(s.selected.CheckoutPath)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	values := []map[string]any{}
	for _, tenant := range catalog.SortedTenants() {
		for _, service := range tenant.Services {
			values = append(values, map[string]any{"team": tenant.Name, "service": service})
		}
	}
	s.writeJSON(w, http.StatusOK, s.envelope(values))
}

func (s *portalServer) handleService(w http.ResponseWriter, r *http.Request, name string) {
	catalog, err := LoadCatalog(s.selected.CheckoutPath)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	tenant, service, err := findCatalogService(catalog, name)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, s.envelope(map[string]any{"team": tenant.Name, "service": service, "namespace": "team-" + tenant.Name}))
}

func (s *portalServer) handleTeam(w http.ResponseWriter, r *http.Request, name string) {
	catalog, err := LoadCatalog(s.selected.CheckoutPath)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	var tenant *CatalogTenant
	for index := range catalog.Tenants {
		if catalog.Tenants[index].Name == name {
			tenant = &catalog.Tenants[index]
			break
		}
	}
	if tenant == nil {
		s.writeError(w, r, http.StatusNotFound, "not_found", "Team is not present in the Git catalog", "Refresh the catalog or create the Team through a reviewed proposal.")
		return
	}
	data := map[string]any{"catalog": tenant, "namespace": "team-" + name}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if client, clientErr := NewClusterClient(s.selected); clientErr == nil {
		if object, getErr := client.Get(ctx, teamGVR, "", name); getErr == nil {
			data["status"] = Summarize("Team", object)
		}
	}
	s.writeJSON(w, http.StatusOK, s.envelope(data))
}

func (s *portalServer) handleApplication(w http.ResponseWriter, r *http.Request, team, name, view string) {
	namespace := "team-" + team
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client, err := NewClusterClient(s.selected)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	application, err := client.Get(ctx, applicationGVR, namespace, name)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	switch view {
	case "":
		spec, _, _ := unstructured.NestedMap(application.Object, "spec")
		status, _, _ := unstructured.NestedMap(application.Object, "status")
		s.writeJSON(w, http.StatusOK, s.envelope(map[string]any{"summary": Summarize("Application", application), "spec": spec, "status": status, "generation": application.GetGeneration(), "createdAt": application.GetCreationTimestamp()}))
	case "rollout":
		strategy, _, _ := unstructured.NestedString(application.Object, "spec", "deployment", "strategy")
		data := map[string]any{"strategy": strategy}
		gvr := deploymentGVR
		if strategy == "canary" {
			gvr = rolloutGVR
		}
		workload, workErr := client.Get(ctx, gvr, namespace, name)
		if workErr != nil {
			s.portalError(w, r, workErr)
			return
		}
		workStatus, _, _ := unstructured.NestedMap(workload.Object, "status")
		data["status"] = workStatus
		analyses, _ := client.List(ctx, analysisRunGVR, namespace, "app.kubernetes.io/instance="+name)
		data["analyses"] = safeObjectSummaries(analyses)
		s.writeJSON(w, http.StatusOK, s.envelope(data))
	case "doctor":
		s.writeJSON(w, http.StatusOK, s.envelope(runApplicationDoctor(ctx, s.selected, client, namespace, name, application)))
	case "policy":
		items, listErr := client.List(ctx, policyReportGVR, namespace, "app.kubernetes.io/instance="+name)
		if listErr != nil {
			s.portalError(w, r, listErr)
			return
		}
		s.writeJSON(w, http.StatusOK, s.envelope(safeObjectSummaries(items)))
	case "logs":
		now := time.Now().UTC()
		raw, proxyErr := client.ServiceProxy(ctx, "loki", 3100, "/loki/api/v1/query_range", url.Values{"query": []string{fmt.Sprintf(`{namespace=%q,application=%q}`, namespace, name)}, "limit": []string{"500"}, "start": []string{strconv.FormatInt(now.Add(-time.Hour).UnixNano(), 10)}, "end": []string{strconv.FormatInt(now.UnixNano(), 10)}})
		s.writeBackend(w, r, raw, proxyErr)
	case "traces":
		raw, proxyErr := client.ServiceProxy(ctx, "tempo", 3200, "/api/search", url.Values{"tags": []string{fmt.Sprintf("service.name=%s service.namespace=%s", name, namespace)}, "limit": []string{"100"}})
		s.writeBackend(w, r, raw, proxyErr)
	case "slo":
		query := fmt.Sprintf(`sum(rate(http_requests_total{namespace=%q,application=%q}[5m]))`, namespace, name)
		raw, proxyErr := client.ServiceProxy(ctx, "monitoring-kube-prometheus-prometheus", 9090, "/api/v1/query", url.Values{"query": []string{query}})
		s.writeBackend(w, r, raw, proxyErr)
	default:
		s.writeError(w, r, http.StatusNotFound, "view_not_found", "Application view was not found", "Use overview, rollout, logs, traces, slo, policy, or doctor.")
	}
}

func (s *portalServer) handleDatabase(w http.ResponseWriter, r *http.Request, team, name string, backups bool) {
	namespace := "team-" + team
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	client, err := NewClusterClient(s.selected)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	if backups {
		items, listErr := client.List(ctx, backupGVR, namespace, "steadystate.dev/database="+name)
		if listErr != nil {
			s.portalError(w, r, listErr)
			return
		}
		s.writeJSON(w, http.StatusOK, s.envelope(safeObjectSummaries(items)))
		return
	}
	database, err := client.Get(ctx, databaseGVR, namespace, name)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	spec, _, _ := unstructured.NestedMap(database.Object, "spec")
	status, _, _ := unstructured.NestedMap(database.Object, "status")
	s.writeJSON(w, http.StatusOK, s.envelope(map[string]any{"summary": Summarize("Database", database), "spec": spec, "status": status, "generation": database.GetGeneration(), "createdAt": database.GetCreationTimestamp()}))
}

func safeObjectSummaries(items []unstructured.Unstructured) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for index := range items {
		status, _, _ := unstructured.NestedMap(items[index].Object, "status")
		result = append(result, map[string]any{"name": items[index].GetName(), "namespace": items[index].GetNamespace(), "generation": items[index].GetGeneration(), "createdAt": items[index].GetCreationTimestamp(), "status": status})
	}
	return result
}

func (s *portalServer) writeBackend(w http.ResponseWriter, r *http.Request, raw json.RawMessage, err error) {
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	if len(raw) > 1024*1024 {
		s.writeError(w, r, http.StatusBadGateway, "response_too_large", "backend response exceeded the 1 MiB portal limit", "Narrow the query time range.")
		return
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		s.writeError(w, r, http.StatusBadGateway, "invalid_backend_response", "backend returned invalid JSON", "Inspect the observability backend.")
		return
	}
	s.writeJSON(w, http.StatusOK, s.envelope(value))
}

func (s *portalServer) handleRequests(w http.ResponseWriter, r *http.Request, requestID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	raw, err := runExternal(ctx, s.selected.CheckoutPath, "gh", "run", "list", "--repo", s.selected.Repository, "--workflow", "platform-change.yml", "--event", "workflow_dispatch", "--limit", "30", "--json", "databaseId,displayTitle,status,conclusion,url,createdAt")
	if err != nil {
		s.portalError(w, r, exitError(ExitRemote, "query GitHub platform requests: %v", err))
		return
	}
	var runs []map[string]any
	if err := json.Unmarshal([]byte(raw), &runs); err != nil {
		s.portalError(w, r, err)
		return
	}
	if requestID != "" {
		for _, run := range runs {
			if strings.Contains(fmt.Sprint(run["displayTitle"]), requestID) {
				s.writeJSON(w, http.StatusOK, s.envelope(run))
				return
			}
		}
		s.writeError(w, r, http.StatusNotFound, "not_found", "request run was not found", "Verify the request ID and GitHub authentication.")
		return
	}
	s.writeJSON(w, http.StatusOK, s.envelope(runs))
}

func (s *portalServer) handlePlan(w http.ResponseWriter, r *http.Request) {
	var input portalPlanInput
	if !s.decode(w, r, &input) {
		return
	}
	if input.Parameters.Force && (input.Parameters.Force != input.Parameters.AcknowledgeDataLoss || input.DataLossConfirmation != "DELETE "+input.Parameters.Name) {
		s.writeError(w, r, http.StatusUnprocessableEntity, "data_loss_confirmation_required", "force deletion requires explicit data-loss acknowledgement and exact resource-name confirmation", "Select the data-loss acknowledgement and type DELETE "+input.Parameters.Name+" exactly.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	base, err := repositoryBaseSHA(ctx, s.selected)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	request := NewChangeRequest(input.Operation, base, input.Parameters)
	set, err := RenderChange(s.selected.CheckoutPath, request)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	diff, err := ChangeSetDiff(set)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	proposalDigest, _ := request.Digest()
	planID, err := randomToken(16)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	renderDigest := set.RenderDigest
	paths := make([]string, 0, len(set.Files))
	for _, file := range set.Files {
		paths = append(paths, file.Path)
	}
	expires := time.Now().UTC().Add(10 * time.Minute)
	s.mu.Lock()
	s.expirePlansLocked()
	if len(s.plans) >= 100 {
		s.mu.Unlock()
		s.writeError(w, r, http.StatusTooManyRequests, "plan_capacity", "too many active plans", "Wait for old plans to expire.")
		return
	}
	s.plans[planID] = &portalPlan{Request: request, Digest: renderDigest, Diff: diff, Files: set.Files, Expires: expires}
	s.mu.Unlock()
	s.writeJSON(w, http.StatusCreated, s.envelope(portalPlanView{PlanID: planID, RequestID: request.RequestID, Operation: request.Operation, BaseSHA: base, ProposalDigest: proposalDigest, RenderDigest: renderDigest, ChangedPaths: paths, Diff: diff, ExpiresAt: expires}))
}

func (s *portalServer) handlePlanSubmit(w http.ResponseWriter, r *http.Request, planID string) {
	var confirmation struct {
		Confirm bool `json:"confirm"`
	}
	if !s.decode(w, r, &confirmation) {
		return
	}
	if !confirmation.Confirm {
		s.writeError(w, r, http.StatusUnprocessableEntity, "confirmation_required", "plan submission requires explicit confirmation", "Review the diff and confirm submission.")
		return
	}
	s.mu.Lock()
	plan := s.plans[planID]
	if plan == nil || plan.Used || time.Now().After(plan.Expires) {
		s.mu.Unlock()
		s.writeError(w, r, http.StatusConflict, "plan_expired", "plan is expired, missing, or already used", "Create and review a fresh plan.")
		return
	}
	plan.Used = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	base, err := repositoryBaseSHA(ctx, s.selected)
	if err != nil || base != plan.Request.BaseSHA {
		s.portalError(w, r, exitError(ExitConflict, "proposal base is stale; create a fresh plan"))
		return
	}
	set, err := RenderChange(s.selected.CheckoutPath, plan.Request)
	if err != nil || set.RenderDigest != plan.Digest {
		s.portalError(w, r, exitError(ExitConflict, "proposal changed after planning"))
		return
	}
	if strings.HasSuffix(plan.Request.Operation, ".finalize") {
		if err := verifyFinalization(ctx, s.selected, plan.Request.Operation, plan.Request.Parameters, base); err != nil {
			s.portalError(w, r, err)
			return
		}
	}
	encoded, err := plan.Request.Encode()
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	runURL, err := dispatchChange(ctx, s.selected, plan.Request, encoded)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	s.broadcast("request.updated", map[string]any{"requestID": plan.Request.RequestID, "status": "Dispatched", "url": runURL})
	s.writeJSON(w, http.StatusAccepted, s.envelope(map[string]any{"requestID": plan.Request.RequestID, "operation": plan.Request.Operation, "status": "Dispatched", "url": runURL}))
}

func (s *portalServer) expirePlansLocked() {
	now := time.Now()
	for key, plan := range s.plans {
		if plan.Used || now.After(plan.Expires) {
			delete(s.plans, key)
		}
	}
	for key, plan := range s.breakPlans {
		if plan.Used || now.After(plan.Expires) {
			delete(s.breakPlans, key)
		}
	}
}

func (s *portalServer) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is invalid: "+err.Error(), "Correct the form and retry.")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "request contains trailing JSON", "Submit one JSON object.")
		return false
	}
	return true
}

func (s *portalServer) portalError(w http.ResponseWriter, r *http.Request, err error) {
	code, status := ExitCode(err), http.StatusBadGateway
	switch code {
	case ExitUsage:
		status = http.StatusBadRequest
	case ExitAuth:
		status = http.StatusForbidden
	case ExitNotFound:
		status = http.StatusNotFound
	case ExitUnhealthy:
		status = http.StatusUnprocessableEntity
	case ExitConflict:
		status = http.StatusConflict
	case ExitTimeout:
		status = http.StatusGatewayTimeout
	}
	s.writeError(w, r, status, "dependency_error_"+strconv.Itoa(code), ErrorMessage(err), "Use the readiness or application doctor view for remediation.")
}

func (s *portalServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "event streaming accepts GET only", "Reconnect through the portal application.")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, r, http.StatusInternalServerError, "stream_unsupported", "streaming is unavailable", "Refresh the page manually.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	channel := make(chan portalEvent, 16)
	s.mu.Lock()
	s.listeners[channel] = struct{}{}
	replay := append([]portalEvent(nil), s.events...)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.listeners, channel); s.mu.Unlock() }()
	last, _ := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64)
	for _, event := range replay {
		if event.ID > last {
			writeSSE(w, event)
		}
	}
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-channel:
			writeSSE(w, event)
			flusher.Flush()
		case now := <-ticker.C:
			s.mu.Lock()
			s.nextEventID++
			event := portalEvent{ID: s.nextEventID, Type: "heartbeat", Time: now.UTC()}
			s.mu.Unlock()
			writeSSE(w, event)
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, event portalEvent) {
	raw, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, raw)
}

func (s *portalServer) broadcast(kind string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEventID++
	event := portalEvent{ID: s.nextEventID, Type: kind, Time: time.Now().UTC(), Data: data}
	s.events = append(s.events, event)
	if len(s.events) > 128 {
		s.events = s.events[len(s.events)-128:]
	}
	for channel := range s.listeners {
		select {
		case channel <- event:
		default:
		}
	}
}

func (s *portalServer) handleBreakGlassPlan(w http.ResponseWriter, r *http.Request, team, application string) {
	var input struct {
		Action string `json:"action"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if input.Action != "promote" && input.Action != "abort" {
		s.writeError(w, r, http.StatusBadRequest, "invalid_action", "break-glass action must be promote or abort", "Choose a supported action.")
		return
	}
	namespace := "team-" + team
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	client, err := NewClusterClient(s.selected)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	app, err := client.Get(ctx, applicationGVR, namespace, application)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	strategy, _, _ := unstructured.NestedString(app.Object, "spec", "deployment", "strategy")
	if strategy != "canary" {
		s.portalError(w, r, exitError(ExitUnhealthy, "Application does not use canary delivery"))
		return
	}
	rollout, err := client.Get(ctx, rolloutGVR, namespace, application)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	if _, err = breakGlassPatch(input.Action, rollout); err != nil {
		s.portalError(w, r, err)
		return
	}
	token, err := randomToken(16)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	expires := time.Now().UTC().Add(2 * time.Minute)
	s.mu.Lock()
	s.breakPlans[token] = &portalBreakPlan{Namespace: namespace, Application: application, Action: input.Action, UID: string(rollout.GetUID()), ResourceVersion: rollout.GetResourceVersion(), Before: rolloutState(rollout), Expires: expires}
	s.mu.Unlock()
	s.writeJSON(w, http.StatusCreated, s.envelope(map[string]any{"operationToken": token, "action": input.Action, "application": application, "namespace": namespace, "targetUID": rollout.GetUID(), "resourceVersion": rollout.GetResourceVersion(), "before": rolloutState(rollout), "expiresAt": expires}))
}

func (s *portalServer) handleBreakGlassExecute(w http.ResponseWriter, r *http.Request, team, application string) {
	var input struct {
		OperationToken string `json:"operationToken"`
		Action         string `json:"action"`
		Reason         string `json:"reason"`
		Confirmation   string `json:"confirmation"`
	}
	if !s.decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Reason) == "" || input.Confirmation != application {
		s.writeError(w, r, http.StatusUnprocessableEntity, "confirmation_required", "reason and exact Application-name confirmation are required", "Enter a reason and the exact Application name.")
		return
	}
	s.mu.Lock()
	plan := s.breakPlans[input.OperationToken]
	if plan == nil || plan.Used || time.Now().After(plan.Expires) || plan.Application != application || plan.Namespace != "team-"+team || plan.Action != input.Action {
		s.mu.Unlock()
		s.writeError(w, r, http.StatusConflict, "operation_expired", "break-glass operation is stale or invalid", "Inspect the Rollout and create a fresh operation.")
		return
	}
	plan.Used = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client, err := NewClusterClient(s.selected)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	rollout, err := client.Get(ctx, rolloutGVR, plan.Namespace, application)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	if rollout.GetResourceVersion() != plan.ResourceVersion || string(rollout.GetUID()) != plan.UID {
		s.portalError(w, r, exitError(ExitConflict, "Rollout changed after break-glass planning"))
		return
	}
	operations, err := breakGlassPatch(input.Action, rollout)
	if err != nil {
		s.portalError(w, r, err)
		return
	}
	requestID := uuid.NewString()
	audit := BreakGlassAudit{APIVersion: breakGlassAuditVersion, RequestID: requestID, Timestamp: time.Now().UTC(), Actor: localActor(), CLI: s.options.Build, Context: s.contextName, Action: input.Action, Reason: Redact(strings.TrimSpace(input.Reason)), Namespace: plan.Namespace, Application: application, TargetUID: plan.UID, ResourceVersion: plan.ResourceVersion, Before: plan.Before, Result: "Attempted"}
	auditPath, err := writeBreakGlassAudit(s.options, audit)
	if err != nil {
		s.portalError(w, r, exitError(ExitRemote, "write break-glass audit: %v", err))
		return
	}
	note := fmt.Sprintf("%s requested for Application %s/%s: %s", input.Action, plan.Namespace, application, audit.Reason)
	if err = client.RecordRolloutEvent(ctx, rollout, requestID, "Attempted", "PlatformctlBreakGlassAttempted", note, corev1.EventTypeNormal); err != nil {
		s.portalError(w, r, err)
		return
	}
	updated, err := client.PatchStatus(ctx, rolloutGVR, plan.Namespace, application, plan.ResourceVersion, operations)
	if err != nil {
		audit.Result = "Failed"
		audit.Error = ErrorMessage(err)
		_, _ = writeBreakGlassAudit(s.options, audit)
		_ = client.RecordRolloutEvent(ctx, rollout, requestID, "Failed", "PlatformctlBreakGlassFailed", note+": "+audit.Error, corev1.EventTypeWarning)
		s.portalError(w, r, err)
		return
	}
	audit.Result = "Completed"
	audit.After = rolloutState(updated)
	if _, err = writeBreakGlassAudit(s.options, audit); err != nil {
		s.portalError(w, r, err)
		return
	}
	if err = client.RecordRolloutEvent(ctx, updated, requestID, "Completed", "PlatformctlBreakGlassCompleted", note, corev1.EventTypeNormal); err != nil {
		s.portalError(w, r, err)
		return
	}
	s.broadcast("rollout.updated", map[string]any{"team": team, "application": application, "action": input.Action})
	s.writeJSON(w, http.StatusOK, s.envelope(BreakGlassResult{RequestID: requestID, Action: input.Action, Namespace: plan.Namespace, Name: application, Result: "Completed", AuditPath: auditPath}))
}
