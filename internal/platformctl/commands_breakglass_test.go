package platformctl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestBreakGlassPatchContracts(t *testing.T) {
	rollout := breakGlassRollout()
	promote, err := breakGlassPatch("promote", rollout)
	if err != nil {
		t.Fatal(err)
	}
	promoteJSON, _ := json.Marshal(promote)
	for _, required := range []string{"pauseConditions", "controllerPause", "currentStepIndex", `"value":3`} {
		if !strings.Contains(string(promoteJSON), required) {
			t.Fatalf("promote patch omitted %q: %s", required, promoteJSON)
		}
	}
	abort, err := breakGlassPatch("abort", rollout)
	if err != nil {
		t.Fatal(err)
	}
	abortJSON, _ := json.Marshal(abort)
	if !strings.Contains(string(abortJSON), `"/status/abort","value":true`) {
		t.Fatalf("unexpected abort patch: %s", abortJSON)
	}
}

func TestBreakGlassRejectsUnsafeTargets(t *testing.T) {
	rollout := breakGlassRollout()
	rollout.SetResourceVersion("")
	if _, err := breakGlassPatch("abort", rollout); ExitCode(err) != ExitConflict {
		t.Fatalf("missing resource version should conflict: %v", err)
	}
	rollout = breakGlassRollout()
	_ = unstructured.SetNestedField(rollout.Object, "stable", "status", "currentPodHash")
	if _, err := breakGlassPatch("abort", rollout); ExitCode(err) != ExitUnhealthy {
		t.Fatalf("stable rollout should not abort: %v", err)
	}
	rollout = breakGlassRollout()
	unstructured.RemoveNestedField(rollout.Object, "spec", "strategy", "canary")
	if _, err := breakGlassPatch("promote", rollout); ExitCode(err) != ExitUnhealthy {
		t.Fatalf("non-canary rollout should not promote: %v", err)
	}
}

func TestBreakGlassAuditIsPrivateAndFailsClosed(t *testing.T) {
	directory := t.TempDir()
	options := Options{AuditDir: directory}
	audit := BreakGlassAudit{
		APIVersion: breakGlassAuditVersion, RequestID: "51b0d6d8-87dc-4ddf-bb09-e8a731983146", Timestamp: time.Unix(1, 0).UTC(),
		Actor: "developer", Action: "abort", Reason: Redact("token: should-not-escape"), Namespace: "team-payments",
		Application: "demo", TargetUID: "uid", ResourceVersion: "7", Before: map[string]any{"phase": "Progressing"}, Result: "Attempted",
	}
	path, err := writeBreakGlassAudit(&options, audit)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "should-not-escape") || !json.Valid(data) {
		t.Fatalf("audit is unsafe or invalid: %s", data)
	}
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.AuditDir = blocker
	if _, err := writeBreakGlassAudit(&options, audit); err == nil {
		t.Fatal("audit failure must prevent the mutation path")
	}
}

func breakGlassRollout() *unstructured.Unstructured {
	rollout := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Rollout",
		"metadata": map[string]any{"name": "demo", "namespace": "team-payments", "resourceVersion": "42", "uid": "rollout-uid"},
		"spec":     map[string]any{"strategy": map[string]any{"canary": map[string]any{}}, "paused": false},
		"status": map[string]any{
			"phase": "Progressing", "currentPodHash": "candidate", "stableRS": "stable", "currentStepIndex": int64(2),
			"controllerPause": true, "pauseConditions": []any{map[string]any{"reason": "InconclusiveAnalysis"}},
			"canary": map[string]any{"currentStepAnalysisRunStatus": map[string]any{"status": "Inconclusive"}},
		},
	}}
	rollout.SetUID(types.UID("rollout-uid"))
	return rollout
}
