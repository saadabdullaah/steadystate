package platformctl

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"sigs.k8s.io/yaml"
)

func TestFullStackScaffoldAndActivationAreDeterministic(t *testing.T) {
	root := brokerFixture(t)
	request := NewChangeRequest("service.scaffold", testBaseSHA, ChangeParameters{Team: "checkout", Name: "checkout", Template: "full-stack", Version: "v0.1.0", CreateTeam: true, WithDatabase: true})
	first, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.RenderDigest != second.RenderDigest {
		t.Fatal("service scaffold is not deterministic")
	}
	for _, change := range first.Files {
		if strings.HasPrefix(change.Path, "gitops/applications/") {
			t.Fatalf("scaffold PR activated an Application: %s", change.Path)
		}
	}
	if err := ApplyChangeSet(root, first); err != nil {
		t.Fatal(err)
	}
	lockData, err := os.ReadFile(filepath.Join(root, "services", "checkout", "web", "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock map[string]any
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatalf("generated npm lock is invalid: %v", err)
	}
	if lock["lockfileVersion"] != float64(3) {
		t.Fatalf("unexpected lockfile version: %#v", lock["lockfileVersion"])
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := catalogTenant(catalog, "checkout")
	if len(tenant.Services) != 1 || tenant.Services[0].Lifecycle != "Scaffolded" || len(tenant.Applications) != 0 || len(tenant.Databases) != 1 {
		t.Fatalf("unexpected scaffold topology: %#v", tenant)
	}
	activation := NewChangeRequest("service.activate", testBaseSHA, ChangeParameters{Team: "checkout", Name: "checkout", Version: "v0.1.0"})
	activationSet, err := RenderChange(root, activation)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, activationSet); err != nil {
		t.Fatal(err)
	}
	web, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(applicationManifestPath("checkout"))))
	api, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(applicationManifestPath("checkout-api"))))
	if !strings.Contains(string(web), "tag: checkout-web-v0.1.0") || !strings.Contains(string(api), "tag: checkout-api-v0.1.0") {
		t.Fatalf("activation tags are not component-scoped:\n%s\n%s", web, api)
	}
	if !strings.Contains(string(web), "networkIsolation: false") || !strings.Contains(string(api), "databaseRef:\n    name: checkout") {
		t.Fatalf("full-stack connectivity contract is missing:\n%s\n%s", web, api)
	}
}

func TestServiceRetirementUsesTwoReviewedRequests(t *testing.T) {
	root := brokerFixture(t)
	scaffold := NewChangeRequest("service.scaffold", testBaseSHA, ChangeParameters{Team: "notes", Name: "notes", Template: "go-api", Version: "v0.1.0", CreateTeam: true})
	set, err := RenderChange(root, scaffold)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	activate := NewChangeRequest("service.activate", testBaseSHA, ChangeParameters{Team: "notes", Name: "notes", Version: "v0.1.0"})
	set, err = RenderChange(root, activate)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	retire := NewChangeRequest("service.retire", testBaseSHA, ChangeParameters{Team: "notes", Name: "notes"})
	set, err = RenderChange(root, retire)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	wrong := NewChangeRequest("service.finalize", testBaseSHA, ChangeParameters{Team: "notes", Name: "notes", DeletionRequest: uuid.NewString(), ApprovalRevision: testBaseSHA})
	if _, err := RenderChange(root, wrong); ExitCode(err) != ExitConflict {
		t.Fatalf("wrong retirement request should fail: %v", err)
	}
	finalize := NewChangeRequest("service.finalize", testBaseSHA, ChangeParameters{Team: "notes", Name: "notes", DeletionRequest: retire.RequestID, ApprovalRevision: testBaseSHA})
	set, err = RenderChange(root, finalize)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tenants) != 1 || catalog.Tenants[0].Name != "payments" {
		t.Fatalf("owned service topology was not fully removed: %#v", catalog.Tenants)
	}
}

func TestGeneratedTemplateToolchains(t *testing.T) {
	if os.Getenv("STEADYSTATE_TEMPLATE_TOOLCHAIN_TEST") != "1" {
		t.Skip("set STEADYSTATE_TEMPLATE_TOOLCHAIN_TEST=1 for external toolchain validation")
	}
	root := brokerFixture(t)
	sourceRoot := repositoryRoot(t)
	for _, file := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(sourceRoot, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	request := NewChangeRequest("service.scaffold", testBaseSHA, ChangeParameters{Team: "toolchain", Name: "toolchain", Template: "full-stack", Version: "v0.1.0", CreateTeam: true, WithDatabase: true})
	set, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	commands := []*exec.Cmd{
		exec.Command("npm", "--prefix", "services/toolchain/web", "ci", "--ignore-scripts", "--no-audit", "--no-fund"), // #nosec G204 -- fixed test-only command.
		exec.Command("npm", "--prefix", "services/toolchain/web", "test"),                                              // #nosec G204 -- fixed test-only command.
		exec.Command("npm", "--prefix", "services/toolchain/web", "run", "build"),                                      // #nosec G204 -- fixed test-only command.
	}
	for _, command := range commands {
		command.Dir, command.Env = root, os.Environ()
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			t.Fatalf("%v failed: %v\n%s", command.Args, runErr, output)
		}
	}
	copyDirectory(t, filepath.Join(root, "services", "toolchain", "web", "dist"), filepath.Join(root, "services", "toolchain", "web", "server", "dist"))
	command := exec.Command("go", "test", "./services/toolchain/...") // #nosec G204 -- fixed test-only command.
	command.Dir, command.Env = root, os.Environ()
	if output, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("%v failed: %v\n%s", command.Args, runErr, output)
	}
}

func TestServiceReleasePlanBindsVersionAndSourceSHA(t *testing.T) {
	root := brokerFixture(t)
	runGit := func(arguments ...string) string {
		command := exec.Command("git", arguments...) // #nosec G204 -- fixed test harness with internally supplied arguments.
		command.Dir = root
		// Git may detach automatic maintenance after a write. Disable it for this
		// disposable repository so no background process can race t.TempDir cleanup.
		command.Env = append(os.Environ(),
			"GIT_CONFIG_COUNT=3",
			"GIT_CONFIG_KEY_0=gc.auto", "GIT_CONFIG_VALUE_0=0",
			"GIT_CONFIG_KEY_1=maintenance.auto", "GIT_CONFIG_VALUE_1=false",
			"GIT_CONFIG_KEY_2=maintenance.autoDetach", "GIT_CONFIG_VALUE_2=false",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", arguments, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	runGit("init", "--initial-branch=main")
	runGit("config", "user.name", "SteadyState Tests")
	runGit("config", "user.email", "tests@steadystate.dev")
	runGit("add", ".")
	runGit("commit", "-m", "baseline")
	baseSHA := runGit("rev-parse", "HEAD")
	request := NewChangeRequest("service.scaffold", baseSHA, ChangeParameters{Team: "catalog", Name: "catalog", Template: "full-stack", Version: "v0.1.0", CreateTeam: true, WithDatabase: true})
	set, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "scaffold")
	sourceSHA := runGit("rev-parse", "HEAD")
	items, err := buildServiceReleasePlan(root, baseSHA, sourceSHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].SemverTag != "catalog-api-v0.1.0" || items[1].SemverTag != "catalog-web-v0.1.0" {
		t.Fatalf("unexpected release matrix: %#v", items)
	}
	if items[0].SHATag != "catalog-api-sha-"+sourceSHA || items[1].SHATag != "catalog-web-sha-"+sourceSHA {
		t.Fatalf("source tags are not immutable: %#v", items)
	}
}

const testBaseSHA = "0123456789abcdef0123456789abcdef01234567"

func TestChangeRequestStrictEncodingAndLimits(t *testing.T) {
	request := NewChangeRequest("team.create", testBaseSHA, teamParameters("orders"))
	encoded, err := request.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeChangeRequest(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.RequestID != request.RequestID || decoded.RendererVersion != RendererVersion {
		t.Fatalf("unexpected decoded request: %#v", decoded)
	}

	raw, _ := json.Marshal(request)
	unknown := strings.TrimSuffix(string(raw), "}") + `,"actor":"untrusted"}`
	if _, err := DecodeChangeRequest(base64.StdEncoding.EncodeToString([]byte(unknown))); ExitCode(err) != ExitUsage {
		t.Fatalf("unknown proposal field should fail closed: %v", err)
	}
	if _, err := DecodeChangeRequest(strings.Repeat("A", MaxProposalBase64Bytes+1)); ExitCode(err) != ExitUsage {
		t.Fatalf("oversized proposal should fail closed: %v", err)
	}
	if _, err := DecodeChangeRequest("not-base64!"); ExitCode(err) != ExitUsage {
		t.Fatalf("malformed Base64 should fail: %v", err)
	}
	request.SchemaVersion = "v1alpha2"
	if err := request.Validate(); ExitCode(err) != ExitUsage {
		t.Fatalf("newer schema should fail closed: %v", err)
	}
	request.SchemaVersion = ChangeRequestSchema
	request.BaseSHA = "main"
	if err := request.Validate(); ExitCode(err) != ExitUsage {
		t.Fatalf("short base must fail: %v", err)
	}
	request.BaseSHA = testBaseSHA
	request.Parameters.Name = "../../escape"
	if err := request.Validate(); ExitCode(err) != ExitUsage {
		t.Fatalf("path-like name should fail: %v", err)
	}
	request.Parameters.Name = "orders"
	request.Parameters.Force = true
	if err := request.Validate(); ExitCode(err) != ExitUsage {
		t.Fatalf("unacknowledged force must fail: %v", err)
	}
}

func TestRendererCreatesOnlyDerivedTeamFilesDeterministically(t *testing.T) {
	root := brokerFixture(t)
	request := NewChangeRequest("team.create", testBaseSHA, teamParameters("orders"))
	first, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.RenderDigest != second.RenderDigest || first.ProposalDigest != second.ProposalDigest {
		t.Fatal("renderer is not deterministic")
	}
	expected := map[string]bool{CatalogRelativePath: true, "gitops/teams/orders/kustomization.yaml": true, "gitops/teams/orders/team.yaml": true}
	for _, change := range first.Files {
		if !expected[change.Path] {
			t.Fatalf("unexpected path %s", change.Path)
		}
		delete(expected, change.Path)
	}
	if len(expected) != 0 {
		t.Fatalf("missing rendered paths: %#v", expected)
	}
	if err := ApplyChangeSet(root, first); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if tenant, err := catalogTenant(catalog, "orders"); err != nil || tenant.Lifecycle != "Active" {
		t.Fatalf("created Team is not active: %#v %v", tenant, err)
	}
}

func TestRepositoryCatalogUsesCanonicalBrokerEncoding(t *testing.T) {
	root := repositoryRoot(t)
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := yaml.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	tracked, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(CatalogRelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tracked, canonical) {
		t.Fatal("tracked tenant catalog is not in canonical broker encoding")
	}
}

func TestProtectedApplicationDeletionRequiresTwoRequests(t *testing.T) {
	root := brokerFixture(t)
	approval := NewChangeRequest("app.delete", testBaseSHA, ChangeParameters{Team: "payments", Name: "demo"})
	set, err := RenderChange(root, approval)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(applicationManifestPath("demo"))))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "Prune=false") || !strings.Contains(string(manifest), approval.RequestID) {
		t.Fatalf("approval did not make only the selected CR pruneable:\n%s", manifest)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := catalogTenant(catalog, "payments")
	entry, _ := applicationEntry(tenant, "demo")
	if entry.Lifecycle != "Retiring" || entry.DeletionRequest != approval.RequestID {
		t.Fatalf("unexpected retirement state: %#v", entry)
	}

	wrong := NewChangeRequest("app.finalize", testBaseSHA, ChangeParameters{Team: "payments", Name: "demo", DeletionRequest: uuid.NewString(), ApprovalRevision: testBaseSHA})
	if _, err := RenderChange(root, wrong); ExitCode(err) != ExitConflict {
		t.Fatalf("wrong deletion request should fail: %v", err)
	}
	finalize := NewChangeRequest("app.finalize", testBaseSHA, ChangeParameters{Team: "payments", Name: "demo", DeletionRequest: approval.RequestID, ApprovalRevision: testBaseSHA})
	finalSet, err := RenderChange(root, finalize)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, finalSet); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(applicationManifestPath("demo")))); !os.IsNotExist(err) {
		t.Fatalf("finalized manifest still exists: %v", err)
	}
}

func TestTeamRetirementProtectsCascadeAndRejectsUnexpectedLeafFiles(t *testing.T) {
	root := brokerFixture(t)
	request := NewChangeRequest("team.delete", testBaseSHA, ChangeParameters{Name: "payments"})
	set, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := catalogTenant(catalog, "payments")
	if tenant.Lifecycle != "Retiring" || tenant.Applications[0].Lifecycle != "Retiring" || tenant.Databases[0].Lifecycle != "Retiring" {
		t.Fatalf("cascade was not fully approved: %#v", tenant)
	}

	extra := filepath.Join(root, "gitops", "applications", "demo", "unexpected.txt")
	if err := os.WriteFile(extra, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	finalize := NewChangeRequest("team.finalize", testBaseSHA, ChangeParameters{Name: "payments", DeletionRequest: request.RequestID, ApprovalRevision: testBaseSHA})
	if _, err := RenderChange(root, finalize); ExitCode(err) != ExitConflict {
		t.Fatalf("unexpected leaf file should fail closed: %v", err)
	}
}

func TestDatabaseDeletionRejectsActiveReferences(t *testing.T) {
	root := brokerFixture(t)
	request := NewChangeRequest("database.delete", testBaseSHA, ChangeParameters{Team: "payments", Name: "orders"})
	if _, err := RenderChange(root, request); ExitCode(err) != ExitConflict {
		t.Fatalf("referenced Database deletion should fail: %v", err)
	}
}

func TestProposalDigestBindsRequestIDAndParameters(t *testing.T) {
	id := uuid.NewString()
	first := NewChangeRequest("team.create", testBaseSHA, teamParameters("orders"))
	first.RequestID = id
	second := first
	second.Parameters.Name = "billing"
	firstDigest, _ := first.Digest()
	secondDigest, _ := second.Digest()
	if firstDigest == secondDigest {
		t.Fatal("request ID reuse with different parameters produced the same digest")
	}
}

func TestApplyChangeSetRejectsWorktreeRace(t *testing.T) {
	root := brokerFixture(t)
	request := NewChangeRequest("team.create", testBaseSHA, teamParameters("orders"))
	set, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(CatalogRelativePath)), []byte("changed after planning\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyChangeSet(root, set); ExitCode(err) != ExitConflict {
		t.Fatalf("worktree race should fail closed: %v", err)
	}
}

func TestDatabaseUpdatePreservesImmutableFieldsAndRejectsShrink(t *testing.T) {
	root := brokerFixture(t)
	manifestPath := filepath.Join(root, filepath.FromSlash(databaseManifestPath("orders")))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "    storageClassName: local-path", "    storageClassName: durable\n  recovery:\n    sourceServerName: historical-archive", 1))
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	parameters := ChangeParameters{Team: "payments", Name: "orders", Instances: 1, StorageSize: "2Gi", BackupSchedule: "0 0 2 * * *", BackupRetention: "7d"}
	request := NewChangeRequest("database.update", testBaseSHA, parameters)
	set, err := RenderChange(root, request)
	if err != nil {
		t.Fatal(err)
	}
	var rendered string
	for _, change := range set.Files {
		if change.Path == databaseManifestPath("orders") {
			rendered = string(change.Content)
		}
	}
	if !strings.Contains(rendered, "storageClassName: durable") || !strings.Contains(rendered, "sourceServerName: historical-archive") {
		t.Fatalf("immutable Database fields were not preserved:\n%s", rendered)
	}

	parameters.StorageSize = "512Mi"
	request = NewChangeRequest("database.update", testBaseSHA, parameters)
	if _, err := RenderChange(root, request); ExitCode(err) != ExitUsage {
		t.Fatalf("storage shrink should fail closed: %v", err)
	}
}

func teamParameters(name string) ChangeParameters {
	return ChangeParameters{Name: name, Owners: []string{name + "-owner"}, AllowedRepositories: []string{"ghcr.io/saadabdullaah/" + name + "-*"}, CPUQuota: "2", MemoryQuota: "4Gi"}
}

func brokerFixture(t *testing.T) string {
	t.Helper()
	source := repositoryRoot(t)
	destination := t.TempDir()
	for _, path := range []string{"gitops/clusters/local/catalog", "gitops/teams/payments", "gitops/applications/demo", "gitops/databases/orders", "apps/demo-app"} {
		copyDirectory(t, filepath.Join(source, filepath.FromSlash(path)), filepath.Join(destination, filepath.FromSlash(path)))
	}
	return destination
}

func copyDirectory(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			copyDirectory(t, filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()))
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func catalogTenant(catalog TenantCatalog, name string) (*CatalogTenant, error) {
	for index := range catalog.Tenants {
		if catalog.Tenants[index].Name == name {
			return &catalog.Tenants[index], nil
		}
	}
	return nil, exitError(ExitNotFound, "Team not found")
}
