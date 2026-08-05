package platformctl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestConfigRoundTripAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SteadyState", "config.yaml")
	config := NewConfig("local", filepath.Join("C:", "work", "steadystate"))
	if err := SaveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentContext != "local" || loaded.Contexts["local"].Profile != "standard" {
		t.Fatalf("unexpected config: %#v", loaded)
	}
	loaded.Contexts["local"] = Context{
		Repository: "saadabdullaah/steadystate", DefaultBranch: "main", CheckoutPath: t.TempDir(),
		ClusterName: "second", Profile: "minimal", HTTPPort: 9080, HTTPSPort: 9443,
	}
	if err := SaveConfig(path, loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected config backup: %v", err)
	}
}

func TestConfigRejectsNewerAndUnknownFields(t *testing.T) {
	tests := []string{
		"apiVersion: cli.steadystate.dev/v1alpha2\nkind: Config\ncurrentContext: local\ncontexts: {}\n",
		"apiVersion: cli.steadystate.dev/v1alpha1\nkind: Config\ncurrentContext: local\nunknown: true\ncontexts: {}\n",
	}
	for _, data := range tests {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); ExitCode(err) != ExitUsage {
			t.Fatalf("expected usage error for %q, got %v", data, err)
		}
	}
}

func TestRedactionAndMachineOutput(t *testing.T) {
	raw := "token: ghp_secret\npassword=orders-secret\nmessage: safe"
	redacted := Redact(raw)
	if strings.Contains(redacted, "ghp_secret") || strings.Contains(redacted, "orders-secret") {
		t.Fatalf("secret escaped redaction: %s", redacted)
	}
	var output bytes.Buffer
	printer := Printer{Format: "json", Writer: &output}
	if err := printer.Print(map[string]string{"status": "Ready"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") || !strings.Contains(output.String(), `"status": "Ready"`) {
		t.Fatalf("unexpected machine output: %q", output.String())
	}
}

func TestRootContractsAndTimeout(t *testing.T) {
	var output bytes.Buffer
	command := NewRootCommand(Options{Stdout: &output, Stderr: &output, Format: "json", Timeout: 5 * time.Millisecond})
	command.SetArgs([]string{"profile", "list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"name": "full"`) {
		t.Fatalf("profile output missing full: %s", output.String())
	}
	options := Options{Timeout: time.Nanosecond}
	ctx, cancel := options.commandContext(t.Context())
	defer cancel()
	<-ctx.Done()
	if ctx.Err() == nil {
		t.Fatal("command context did not enforce its timeout")
	}
	invalid := NewRootCommand(Options{})
	invalid.SetArgs([]string{"not-a-command"})
	if err := invalid.Execute(); ExitCode(err) != ExitUsage {
		t.Fatalf("unknown command should use exit code %d, got %d (%v)", ExitUsage, ExitCode(err), err)
	}
}

func TestSummarizeAndCatalogNamespace(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "demo", "namespace": "team-payments", "generation": int64(3)},
		"status": map[string]any{"phase": "Healthy", "observedGeneration": int64(3), "conditions": []any{
			map[string]any{"type": "Ready", "status": "True", "reason": "Available"},
		}},
	}}
	summary := Summarize("Application", object)
	if summary.Ready != "True" || summary.Phase != "Healthy" || summary.ObservedGeneration != 3 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	catalog := TenantCatalog{Tenants: []CatalogTenant{{Name: "payments", Applications: []CatalogApplication{{Name: "demo"}}, Databases: []CatalogDatabase{{Name: "orders"}}}}}
	for kind, name := range map[string]string{"Application": "demo", "Database": "orders"} {
		namespace, err := catalogNamespace(catalog, kind, name)
		if err != nil || namespace != "team-payments" {
			t.Fatalf("resolve %s: namespace=%q err=%v", kind, namespace, err)
		}
	}
}

func TestDoctorReadsNamesAndVersionPinsWithoutValues(t *testing.T) {
	names := decodeGitHubNames(`[{"name":"STEADYSTATE_BOT_PRIVATE_KEY"},{"name":"SOPS_AGE_KEY"}]`)
	if !names["STEADYSTATE_BOT_PRIVATE_KEY"] || !names["SOPS_AGE_KEY"] {
		t.Fatalf("unexpected GitHub names: %#v", names)
	}
	path := filepath.Join(t.TempDir(), "versions.env")
	if err := os.WriteFile(path, []byte("GO_VERSION=1.25.12\nKUBERNETES_VERSION=1.35.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readVersionPin(path, "KUBERNETES_VERSION"); got != "1.35.5" {
		t.Fatalf("unexpected pin %q", got)
	}
	if got := githubCLIVersion("gh version 2.97.0 (2026-07-01)\n"); got != "2.97.0" {
		t.Fatalf("unexpected GitHub CLI version %q", got)
	}
	if !versionAtLeast("2.98.1", "2.97.0") || versionAtLeast("2.96.9", "2.97.0") {
		t.Fatal("GitHub CLI security baseline comparison is incorrect")
	}
}

func TestWriteMachineOutputAndConfirmationStaySeparated(t *testing.T) {
	set := ChangeSet{RequestID: "6ba7b810-9dad-41d1-80b4-00c04fd430c8", ProposalDigest: "sha256:proposal", RenderDigest: "sha256:render", Files: []FileChange{{Path: CatalogRelativePath, Action: "update"}}}
	var stdout, stderr bytes.Buffer
	options := Options{Format: "json", Stdin: strings.NewReader("yes\n"), Stdout: &stdout, Stderr: &stderr}
	confirmed, err := confirmSubmission(&options, "team.create", "payments")
	if err != nil || !confirmed {
		t.Fatalf("confirmation failed: confirmed=%t err=%v", confirmed, err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "Submit team.create") {
		t.Fatalf("prompt polluted machine stdout: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if err := printChangeSummary(&options, set, "planned", "", "diff-body"); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("machine output is not valid JSON: %v\n%s", err, stdout.String())
	}
	if decoded["diff"] != "diff-body" {
		t.Fatalf("machine plan omitted diff: %#v", decoded)
	}
}

func TestConfigRejectsUnsafeDefaultBranch(t *testing.T) {
	config := NewConfig("local", t.TempDir())
	value := config.Contexts["local"]
	value.DefaultBranch = "main:refs/heads/injected"
	config.Contexts["local"] = value
	if err := config.Validate(); err == nil {
		t.Fatal("unsafe Git ref must be rejected")
	}
}
