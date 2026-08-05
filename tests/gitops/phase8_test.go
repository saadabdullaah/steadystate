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
	catalog = []byte(strings.Replace(string(catalog), "lifecycle: Active", "lifecycle: Retiring\n    deletionRequest: 6ba7b810-9dad-41d1-80b4-00c04fd430c8", 1))
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
