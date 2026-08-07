package gitops_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPhase8CatalogAndProtectedPruneContracts(t *testing.T) {
	root := repositoryRoot(t)
	rendered := string(run(t, root, "helm", "template", "steadystate-root", filepath.Join(root, "gitops", "clusters", "local"), "--set-string", "gitRevision="+testRevision))
	for _, expected := range []string{
		"name: payments", "path: gitops/teams/payments", "path: gitops/applications/demo",
		"prune: true", "RespectIgnoreDifferences=true",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("root catalog render is missing %q", expected)
		}
	}
	if strings.Contains(rendered, "resources-finalizer.argocd.argoproj.io") {
		t.Fatal("active tenant must not carry the Argo cascade finalizer")
	}
	for _, manifest := range []string{"gitops/teams/payments/team.yaml", "gitops/applications/demo/application.yaml", "gitops/databases/orders/database.yaml"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifest)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "argocd.argoproj.io/sync-options: Prune=false") {
			t.Fatalf("active resource %s is not prune-protected", manifest)
		}
	}
}

func TestPhase8AcceptanceCanRenderOneCatalogTenant(t *testing.T) {
	root := repositoryRoot(t)
	rendered := string(run(t, root, "helm", "template", "steadystate-root", filepath.Join(root, "gitops", "clusters", "local"),
		"--set-string", "gitRevision="+testRevision, "--set-string", "tenantFilter=xyz"))
	if !strings.Contains(rendered, "name: xyz") || !strings.Contains(rendered, "path: gitops/teams/xyz") {
		t.Fatal("filtered root render is missing the selected xyz tenant")
	}
	for _, excluded := range []string{"name: payments", "path: gitops/teams/payments", "path: gitops/databases/orders", "path: gitops/applications/demo"} {
		if strings.Contains(rendered, excluded) {
			t.Fatalf("filtered root render retained unselected tenant content %q", excluded)
		}
	}
}

func TestPhase8RetiringTeamEnablesExactArgoCascade(t *testing.T) {
	root := repositoryRoot(t)
	source := filepath.Join(root, "gitops", "clusters", "local")
	chart := filepath.Join(t.TempDir(), "local")
	if err := os.CopyFS(chart, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(chart, "catalog", "tenants.yaml")
	catalog, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog = []byte(strings.Replace(
		string(catalog),
		"  lifecycle: Active\n  name: payments",
		"  deletionRequest: 6ba7b810-9dad-41d1-80b4-00c04fd430c8\n  lifecycle: Retiring\n  name: payments",
		1,
	))
	if err := os.WriteFile(catalogPath, catalog, 0o600); err != nil {
		t.Fatal(err)
	}
	rendered := string(run(t, root, "helm", "template", "steadystate-root", chart, "--set-string", "gitRevision="+testRevision))
	if strings.Count(rendered, "resources-finalizer.argocd.argoproj.io") != 1 {
		t.Fatal("retiring Team must enable exactly one tenant-child cascade finalizer")
	}
}

func TestPhase8BrokerWorkflowTrustBoundary(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "platform-change.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, expected := range []string{
		"schema_version:", "request_id:", "base_sha:", "proposal:",
		"permission-contents: write", "permission-pull-requests: write",
		"INITIATING_ACTOR: ${{ github.actor }}", "Proposal-Digest:",
		"automation/platform/", "persist-credentials: false", "cancel-in-progress: false",
		"git status --porcelain=v1 --untracked-files=all",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("broker workflow is missing %q", expected)
		}
	}
	validation := strings.Index(workflow, "Validate request and render before App authentication")
	token := strings.Index(workflow, "Create repository-scoped delivery token")
	if validation < 0 || token < 0 || validation >= token {
		t.Fatal("App token is minted before proposal validation")
	}
	if strings.Contains(workflow, "permission-actions: write") || strings.Contains(workflow, "permission-administration") {
		t.Fatal("broker App token requests an out-of-scope permission")
	}
}

func TestPhase8AcceptanceWorkflowContract(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "phase8.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, expected := range []string{
		"name: Phase 8 acceptance", "timeout-minutes: 90", "cancel-in-progress: false",
		"pull_request:", "github.event.pull_request.head.repo.full_name == github.repository",
		"PHASE8_SOURCE_SHA: ${{ github.event.pull_request.head.sha || github.sha }}",
		"permission-contents: write", "permission-pull-requests: write",
		"./scripts/phase8-acceptance.ps1 -Stage Prepare",
		"./scripts/phase8-acceptance.ps1 -Stage Test",
		"./scripts/phase8-acceptance.ps1 -Stage Finalize",
		"./scripts/phase8-acceptance.ps1 -Stage CaptureFailure",
		"PHASE8_ACCEPTANCE_AUTOMERGE: \"true\"",
		"-TenantFilter xyz",
		"vhs docs/demonstrations/phase8-zero-to-live.tape",
		"phase8-acceptance-${{ env.PHASE8_SOURCE_SHA }}", "if-no-files-found: error",
		"Capture failure evidence before cleanup", "Delete disposable acceptance branch",
		"Destroy disposable cluster through platformctl",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("Phase 8 workflow is missing %q", expected)
		}
	}
	if strings.Contains(workflow, "permission-actions: write") || strings.Contains(workflow, "cancel-in-progress: true") {
		t.Fatal("Phase 8 acceptance expands App permissions or permits cancellation")
	}
}

func TestPhase8AcceptanceSafetyAndEvidenceContract(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "phase8-acceptance.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, expected := range []string{
		"^acceptance/phase8-[0-9]+-[0-9]+$", "Acceptance auto-merge refuses non-acceptance branches.",
		"Acceptance auto-merge can never target main or a normal branch.",
		"broker validate --proposal", "broker apply --proposal", "gh pr merge", "--base $State.branch",
		"repos/$Repository/pulls/$Number", "author.type -cne 'Bot'", "Get-AppBotLogin",
		"service.retire", "service.finalize", "Wait-ArgoRevision", "retained-object-inventory.txt",
		"Wait-ControlPlaneStable 300", "control-plane-stability.txt",
		"git branch -D acceptance-request", "Could not remove the temporary local acceptance branch.",
		"TestApplicationDoctorFailureFixtures|TestBreakGlass", "app-authored-two-pr-finalizer-retirement",
		"no-residual-live-or-request-resources", "Assert-NoSecrets", "schemaVersion=1;phase='8'",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("Phase 8 acceptance script is missing %q", expected)
		}
	}
	if strings.Contains(script, "gh pr merge $number --repo $Repository --base main") {
		t.Fatal("Phase 8 acceptance can auto-merge into main")
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"PHASE8_ACCEPTANCE_STAGE ?= Test", "-Phase8AcceptanceStage $(PHASE8_ACCEPTANCE_STAGE)", "phase8-acceptance"} {
		if !strings.Contains(string(makefile), expected) {
			t.Fatalf("Phase 8 Make contract is missing %q", expected)
		}
	}
	tape, err := os.ReadFile(filepath.Join(root, "docs", "demonstrations", "phase8-zero-to-live.tape"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Output .artifacts/phase8/acceptance/phase8-zero-to-live.gif", "RESULT .* PASSED"} {
		if !strings.Contains(string(tape), expected) {
			t.Fatalf("Phase 8 tape is missing %q", expected)
		}
	}
}
