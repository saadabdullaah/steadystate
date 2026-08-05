package platformctl

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestDoctorConditionUsesTruthfulStates(t *testing.T) {
	for _, test := range []struct {
		status string
		want   string
	}{
		{"True", "Pass"},
		{"Unknown", "Warning"},
		{"False", "Fail"},
	} {
		object := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Ready", "status": test.status, "reason": "Fixture", "message": "sanitized"},
		}}}}
		if got := doctorCondition(object, "Ready"); got.Status != test.want || got.Details != "Fixture: sanitized" {
			t.Fatalf("status %s: unexpected check %#v", test.status, got)
		}
	}
}

func TestRouteHealthRequiresAcceptedAndResolvedReferences(t *testing.T) {
	route := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"parents": []any{
		map[string]any{"conditions": []any{
			map[string]any{"type": "Accepted", "status": "True"},
			map[string]any{"type": "ResolvedRefs", "status": "True"},
		}},
	}}}}
	if status, _ := routeHealth(route); status != "Pass" {
		t.Fatal("accepted route with resolved references should pass")
	}
	unstructured.RemoveNestedField(route.Object, "status", "parents")
	if status, _ := routeHealth(route); status != "Fail" {
		t.Fatal("route without parent conditions should fail")
	}
}

func TestApplicationDoctorContractOrder(t *testing.T) {
	checks := applicationDoctorFixture(t, "True", "Healthy", "True", "True")
	want := []string{
		"ContextAndOwnership", "GitOpsRevision", "ApplicationAndProvenance", "AdmissionAndPolicy",
		"WorkloadAndRollout", "GatewayAndEndpoints", "MetricsSLOAndAlerts", "LogsAndTraces", "DatabaseAndBackups",
	}
	if len(checks) != len(want) {
		t.Fatalf("expected nine doctor checks, got %#v", checks)
	}
	for index := range want {
		if checks[index].Name != want[index] || checks[index].Status != "Pass" {
			t.Fatalf("check %d: want %s Pass, got %#v", index, want[index], checks[index])
		}
	}
}

func TestApplicationDoctorFailureFixtures(t *testing.T) {
	for _, test := range []struct {
		name, security, rollout, route, database, failedCheck string
	}{
		{"unsigned-admission", "False", "Healthy", "True", "True", "AdmissionAndPolicy"},
		{"canary-no-traffic", "True", "Degraded", "True", "True", "WorkloadAndRollout"},
		{"rejected-route", "True", "Healthy", "False", "True", "GatewayAndEndpoints"},
		{"unready-database", "True", "Healthy", "True", "False", "DatabaseAndBackups"},
	} {
		t.Run(test.name, func(t *testing.T) {
			checks := applicationDoctorFixture(t, test.security, test.rollout, test.route, test.database)
			for _, check := range checks {
				if check.Name == test.failedCheck {
					if check.Status != "Fail" || check.Remediation == "" || len(check.Evidence) == 0 {
						t.Fatalf("failure diagnosis is incomplete: %#v", check)
					}
					return
				}
			}
			t.Fatalf("expected failed check %s in %#v", test.failedCheck, checks)
		})
	}
}

func applicationDoctorFixture(t *testing.T, securityStatus, rolloutPhase, routeStatus, databaseStatus string) []DoctorCheck {
	t.Helper()
	revision := strings.Repeat("a", 40)
	application := doctorObject(applicationGVR, "Application", "team-payments", "demo", map[string]any{
		"spec": map[string]any{
			"deployment":    map[string]any{"strategy": "canary"},
			"observability": map[string]any{"metrics": true, "logs": true, "traces": true},
			"databaseRef":   map[string]any{"name": "orders"},
		},
		"status": map[string]any{
			"observedGeneration": int64(1), "activeVersion": "v0.8.0", "resolvedImageDigest": "sha256:" + strings.Repeat("b", 64), "resolvedGitRevision": revision,
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True", "reason": "Available"},
				map[string]any{"type": "ServiceHealth", "status": "True", "reason": "RouteAndWorkloadReady"},
				map[string]any{"type": "SecurityPolicyReady", "status": securityStatus, "reason": "FixtureAdmission"},
			},
		},
	})
	application.SetGeneration(1)
	application.SetAnnotations(map[string]string{"steadystate.dev/source-revision": revision})
	objects := []runtime.Object{
		doctorObject(argoAppGVR, "Application", "argocd", "payments", map[string]any{"status": map[string]any{"sync": map[string]any{"revision": revision}}}),
		doctorObject(policyReportGVR, "PolicyReport", "team-payments", "demo-policy", nil),
		doctorObject(rolloutGVR, "Rollout", "team-payments", "demo", map[string]any{"status": map[string]any{"phase": rolloutPhase}}),
		doctorObject(analysisRunGVR, "AnalysisRun", "team-payments", "demo-analysis", nil),
		doctorObject(replicaSetGVR, "ReplicaSet", "team-payments", "demo-candidate", nil),
		doctorObject(podGVR, "Pod", "team-payments", "demo-pod", nil),
		doctorObject(httpRouteGVR, "HTTPRoute", "team-payments", "demo", map[string]any{
			"spec": map[string]any{"rules": []any{
				map[string]any{"backendRefs": []any{
					map[string]any{"name": "demo-stable", "weight": int64(90)},
					map[string]any{"name": "demo-canary", "weight": int64(10)},
				}},
			}},
			"status": map[string]any{"parents": []any{
				map[string]any{"conditions": []any{
					map[string]any{"type": "Accepted", "status": routeStatus},
					map[string]any{"type": "ResolvedRefs", "status": routeStatus},
				}},
			}},
		}),
		doctorObject(databaseGVR, "Database", "team-payments", "orders", map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": databaseStatus, "reason": "FixtureDatabase"}}}}),
		doctorObject(backupGVR, "Backup", "team-payments", "orders-backup", nil),
	}
	listKinds := map[schema.GroupVersionResource]string{
		policyReportGVR: "PolicyReportList", analysisRunGVR: "AnalysisRunList", backupGVR: "BackupList",
		replicaSetGVR: "ReplicaSetList", podGVR: "PodList",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"success","data":{"result":[]}}`))
	}))
	defer server.Close()
	client := &ClusterClient{dynamic: dynamicClient, core: kubernetesfake.NewClientset(), rest: &rest.Config{Host: server.URL}}
	return runApplicationDoctor(t.Context(), Context{CheckoutPath: brokerFixture(t)}, client, "team-payments", "demo", application)
}

func doctorObject(gvr schema.GroupVersionResource, kind, namespace, name string, fields map[string]any) *unstructured.Unstructured {
	object := map[string]any{
		"apiVersion": gvr.GroupVersion().String(), "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": namespace, "labels": map[string]any{"app.kubernetes.io/instance": "demo", "steadystate.dev/database": "orders"}},
	}
	for key, value := range fields {
		object[key] = value
	}
	return &unstructured.Unstructured{Object: object}
}
