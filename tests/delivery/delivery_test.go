package delivery_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestDemoVersionContract(t *testing.T) {
	root := repositoryRoot(t)
	version := strings.TrimSpace(read(t, filepath.Join(root, "apps", "demo-app", "VERSION")))
	if !regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`).MatchString(version) {
		t.Fatalf("demo VERSION %q is not strict semver", version)
	}
	if version != "v0.5.1" {
		t.Fatalf("Phase 5 security patch must declare v0.5.1, got %q", version)
	}
	manifest := read(t, filepath.Join(root, "gitops", "applications", "demo", "application.yaml"))
	tagPattern := regexp.MustCompile(`(?m)^    tag: (v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))$`)
	tagMatches := tagPattern.FindAllStringSubmatch(manifest, -1)
	if len(tagMatches) != 1 || len(tagMatches[0]) != 2 {
		t.Fatal("the demo manifest must contain exactly one strict semver image tag")
	}
	manifestVersion := tagMatches[0][1]
	if manifestVersion != version && manifestVersion != "v0.5.0" {
		t.Fatalf("demo manifest tag %q must be the released v0.5.0 baseline or match VERSION %q", manifestVersion, version)
	}
}

func TestDemoReleaseWorkflowContract(t *testing.T) {
	root := repositoryRoot(t)
	workflow := read(t, filepath.Join(root, ".github", "workflows", "demo-release.yml"))
	required := []string{
		"group: demo-release",
		"cancel-in-progress: false",
		"workflow_dispatch: # Recovery only; no user-controlled build inputs.",
		"Get-ChildItem -LiteralPath apps/demo-app -Filter '*.go' -File",
		"contents: read",
		"packages: write",
		"platforms: linux/amd64",
		"provenance: false",
		"sbom: false",
		"client-id: ${{ vars.STEADYSTATE_BOT_CLIENT_ID }}",
		"private-key: ${{ secrets.STEADYSTATE_BOT_PRIVATE_KEY }}",
		"permission-contents: write",
		"permission-pull-requests: write",
		"automation/demo-app-$env:VERSION",
		"chore(gitops): deploy demo app $env:VERSION",
		"gitops/applications/demo/application.yaml",
		"sha-$env:GITHUB_SHA",
		"$version-bad",
		"sha-$env:GITHUB_SHA-bad",
		"build-args: INJECT_ERROR_RATE=0.10",
		"Good and bad image variants must have distinct digests.",
		"all good and bad semver/SHA tags must exist or all must be absent",
		"go test -race ./apps/demo-app/...",
		"manifest unknown|not found",
		"detect-demo-runtime-impact",
		"needs: impact",
		"if: needs.impact.outputs.release == 'true'",
		"'apps/demo-app/VERSION' -in $changed",
	}
	for _, value := range required {
		if !strings.Contains(workflow, value) {
			t.Errorf("demo release workflow is missing %q", value)
		}
	}

	pins := []string{
		"actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0",
		"actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1",
		"docker/setup-buildx-action@bb05f3f5519dd87d3ba754cc423b652a5edd6d2c",
		"docker/login-action@af1e73f918a031802d376d3c8bbc3fe56130a9b0",
		"docker/build-push-action@f9f3042f7e2789586610d6e8b85c8f03e5195baf",
	}
	for _, pin := range pins {
		if !strings.Contains(workflow, pin) {
			t.Errorf("demo release workflow is missing pin %q", pin)
		}
	}
	if strings.Contains(workflow, ":latest") {
		t.Fatal("demo release workflow must not publish a mutable latest tag")
	}
	if strings.Count(workflow, "docker/build-push-action@f9f3042f7e2789586610d6e8b85c8f03e5195baf") != 2 {
		t.Fatal("demo release workflow must build exactly the good and bad immutable variants")
	}
	for _, path := range []string{"apps/demo-app/main.go", "apps/demo-app/Dockerfile", "apps/demo-app/VERSION", "go.mod", "go.sum"} {
		if !strings.Contains(workflow, "      - "+path) {
			t.Errorf("demo release trigger is missing %s", path)
		}
	}
}

func TestDemoTelemetryImageContract(t *testing.T) {
	root := repositoryRoot(t)
	source := read(t, filepath.Join(root, "apps", "demo-app", "main.go"))
	for _, value := range []string{
		"INJECT_ERROR_RATE",
		"INJECT_LATENCY_MS",
		"CRASH_AFTER_REQUESTS",
		"http_requests_total",
		"http_request_duration_seconds",
		`mux.Handle("/metrics"`,
	} {
		if !strings.Contains(source, value) {
			t.Errorf("demo telemetry source is missing %q", value)
		}
	}
	dockerfile := read(t, filepath.Join(root, "apps", "demo-app", "Dockerfile"))
	for _, value := range []string{"ARG INJECT_ERROR_RATE=0", "ENV INJECT_ERROR_RATE=${INJECT_ERROR_RATE}"} {
		if !strings.Contains(dockerfile, value) {
			t.Errorf("demo image contract is missing %q", value)
		}
	}
}

func TestPhase4DemoProgressiveDeliveryContract(t *testing.T) {
	root := repositoryRoot(t)
	manifest := read(t, filepath.Join(root, "gitops", "applications", "demo", "application.yaml"))
	for _, value := range []string{"strategy: canary", "automaticRollback: true", "metrics: true"} {
		if !strings.Contains(manifest, value) {
			t.Errorf("Phase 4 demo manifest is missing %q", value)
		}
	}
	for _, weight := range []string{"10", "25", "50", "100"} {
		if strings.Count(manifest, "- weight: "+weight+"\n        pause: 30s") != 1 {
			t.Errorf("Phase 4 demo manifest must contain exactly one %s%%/30s step", weight)
		}
	}
	phase3 := read(t, filepath.Join(root, "scripts", "phase3-acceptance.ps1"))
	for _, value := range []string{"strategy: rolling", "strategy: canary", "metrics: false", "metrics: true"} {
		if !strings.Contains(phase3, value) {
			t.Errorf("Phase 3 acceptance rolling override is missing %q", value)
		}
	}
	phase4 := read(t, filepath.Join(root, "scripts", "phase4-controller-test.ps1"))
	if !strings.Contains(phase4, "$application.status.observedGeneration -eq [int64]$application.metadata.generation") {
		t.Fatal("Phase 4 acceptance must reject stale Application status generations")
	}
}

func TestPhase5DemoObservabilityContract(t *testing.T) {
	t.Parallel()
	root := repositoryRoot(t)
	manifest := read(t, filepath.Join(root, "gitops", "applications", "demo", "application.yaml"))
	for _, value := range []string{"metrics: true", "logs: true", "traces: true"} {
		if !strings.Contains(manifest, value) {
			t.Errorf("Phase 5 demo manifest is missing %q", value)
		}
	}
	source := read(t, filepath.Join(root, "apps", "demo-app", "main.go"))
	for _, value := range []string{"X-Request-ID", "trace_id", "span_id", "OTEL_EXPORTER_OTLP_ENDPOINT", "http.route"} {
		if !strings.Contains(source, value) {
			t.Errorf("Phase 5 demo telemetry source is missing %q", value)
		}
	}
}

func TestDemoVersionBumpGate(t *testing.T) {
	root := repositoryRoot(t)
	ci := read(t, filepath.Join(root, ".github", "workflows", "ci.yml"))
	for _, value := range []string{"if: github.event_name == 'pull_request'", "github.event.pull_request.base.sha", "check-demo-version.ps1"} {
		if !strings.Contains(ci, value) {
			t.Errorf("CI demo version gate is missing %q", value)
		}
	}

	guard := read(t, filepath.Join(root, "scripts", "check-demo-version.ps1"))
	for _, path := range []string{"apps/demo-app/main.go", "apps/demo-app/Dockerfile", "go.mod", "go.sum", "apps/demo-app/VERSION"} {
		if !strings.Contains(guard, "'"+path+"'") {
			t.Errorf("demo version guard is missing %s", path)
		}
	}
	for _, value := range []string{"git diff --name-only", "$BaseRevision...HEAD", "strict vMAJOR.MINOR.PATCH", "value was not bumped"} {
		if !strings.Contains(guard, value) {
			t.Errorf("demo version guard is missing %q", value)
		}
	}
	for _, value := range []string{"go list -deps", "$demoModules", "$affectedModules", "$buildDirectiveChanged"} {
		if !strings.Contains(guard, value) {
			t.Errorf("demo version guard is missing dependency-aware check %q", value)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
