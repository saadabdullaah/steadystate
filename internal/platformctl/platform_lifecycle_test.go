package platformctl

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestSelectPowerShellExecutable(t *testing.T) {
	lookPath := func(available map[string]string) func(string) (string, error) {
		return func(name string) (string, error) {
			if path, ok := available[name]; ok {
				return path, nil
			}
			return "", errors.New("not found")
		}
	}

	for _, test := range []struct {
		name      string
		goos      string
		available map[string]string
		want      string
		wantError bool
	}{
		{name: "prefers PowerShell 7", goos: "windows", available: map[string]string{"pwsh": `C:\\Program Files\\PowerShell\\7\\pwsh.exe`, "powershell.exe": `C:\\Windows\\powershell.exe`}, want: `C:\\Program Files\\PowerShell\\7\\pwsh.exe`},
		{name: "uses Windows PowerShell fallback", goos: "windows", available: map[string]string{"powershell.exe": `C:\\Windows\\powershell.exe`}, want: `C:\\Windows\\powershell.exe`},
		{name: "requires pwsh on Linux", goos: "linux", available: map[string]string{"powershell.exe": "/unsupported"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := selectPowerShellExecutable(test.goos, lookPath(test.available))
			if test.wantError {
				if err == nil {
					t.Fatalf("expected an error, got executable %q", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("executable=%q err=%v, want %q", got, err, test.want)
			}
		})
	}
}

func TestWindowsPowerShellArgumentsBypassOnlyFixedScriptPolicy(t *testing.T) {
	arguments := []string{"-NoProfile", "-File", `D:\\SteadyState\\scripts\\dev.ps1`, "bootstrap"}
	got := powerShellArguments(`C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe`, arguments)
	want := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", `D:\\SteadyState\\scripts\\dev.ps1`, "bootstrap"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("arguments=%q, want %q", got, want)
	}

	pwsh := powerShellArguments("pwsh", arguments)
	if strings.Join(pwsh, "|") != strings.Join(arguments, "|") {
		t.Fatalf("pwsh arguments changed: %q", pwsh)
	}
}

func TestSelectGitHubCLIPrefersVerifiedUserInstallation(t *testing.T) {
	home := `C:\Users\developer`
	localAppData := `C:\Users\developer\AppData\Local`
	userCLI := `C:\Users\developer\.local\bin\gh.exe`
	pathCLI := `C:\Program Files\GitHub CLI\gh.exe`
	exists := func(path string) bool { return path == userCLI }
	lookPath := func(string) (string, error) { return pathCLI, nil }
	if got := selectGitHubCLIExecutable("windows", home, localAppData, exists, lookPath); got != userCLI {
		t.Fatalf("executable=%q, want verified user installation %q", got, userCLI)
	}
}

func TestSelectGitHubCLIFallsBackToPath(t *testing.T) {
	want := "/usr/local/bin/gh"
	lookPath := func(string) (string, error) { return want, nil }
	if got := selectGitHubCLIExecutable("linux", "/home/developer", "", func(string) bool { return false }, lookPath); got != want {
		t.Fatalf("executable=%q, want PATH installation %q", got, want)
	}
}

func TestJoinPlatformPathUsesTargetOSSeparators(t *testing.T) {
	if got, want := joinPlatformPath("windows", `C:\Users\developer`, ".local", "bin", "gh.exe"), `C:\Users\developer\.local\bin\gh.exe`; got != want {
		t.Fatalf("Windows path=%q, want %q", got, want)
	}
	if got, want := joinPlatformPath("linux", "/home/developer", ".local", "bin", "gh"), "/home/developer/.local/bin/gh"; got != want {
		t.Fatalf("Linux path=%q, want %q", got, want)
	}
	if got := joinPlatformPath("windows", "", "Programs", "GitHub CLI", "gh.exe"); got != "" {
		t.Fatalf("empty trusted root produced relative candidate %q", got)
	}
}

func TestMissingFullProfileSecurityToolsAreBootstrapWarnings(t *testing.T) {
	for _, tool := range []string{"sops", "age"} {
		status, _, remediation := missingToolCheck(tool, "full")
		if status != "Warning" || !strings.Contains(remediation, "platformctl platform up") {
			t.Fatalf("%s status=%q remediation=%q", tool, status, remediation)
		}
	}
	status, _, _ := missingToolCheck("docker", "full")
	if status != "Fail" {
		t.Fatalf("Docker missing status=%q, want Fail", status)
	}
}

func TestPlatformUpPreflightBlockingBoundary(t *testing.T) {
	for _, test := range []struct {
		name     string
		blocking bool
	}{
		{name: "resource-budget", blocking: true},
		{name: "tool-pwsh", blocking: true},
		{name: "full-profile-steadystate.agekey", blocking: true},
		{name: "tool-sops", blocking: false},
		{name: "tool-age", blocking: false},
		{name: "github-cli-version", blocking: true},
		{name: "kubernetes", blocking: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := platformUpBlockingChecks[test.name] || strings.HasPrefix(test.name, "full-profile-")
			if got != test.blocking {
				t.Fatalf("blocking=%t, want %t", got, test.blocking)
			}
		})
	}
}

func TestHostedFullLifecycleStabilizesBeforeGitOpsWorkloads(t *testing.T) {
	stages := platformUpStagesForEnvironment("full", true)
	want := []string{
		"tools", "check-versions", "build-images", "bootstrap", "stabilize-hosted-kind",
		"start-backup-store", "load-images", "deploy-gitops", "test-gitops", "verify-data",
	}
	if len(stages) != len(want) {
		t.Fatalf("stage count=%d, want %d: %+v", len(stages), len(want), stages)
	}
	for index := range want {
		if stages[index].Command != want[index] {
			t.Fatalf("stage[%d]=%q, want %q", index, stages[index].Command, want[index])
		}
	}
}

func TestHostedStabilizationDoesNotChangeLocalOrSmallerProfiles(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile string
		hosted  bool
	}{
		{name: "local full", profile: "full", hosted: false},
		{name: "hosted standard", profile: "standard", hosted: true},
		{name: "hosted minimal", profile: "minimal", hosted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, stage := range platformUpStagesForEnvironment(test.profile, test.hosted) {
				if stage.Command == "stabilize-hosted-kind" {
					t.Fatalf("unexpected hosted stabilization in %s lifecycle", test.name)
				}
			}
		})
	}
}

func TestResourceBudgetSupportsStandardButRejectsFullAtEightGiB(t *testing.T) {
	available := int64(78 * (1 << 30) / 10)
	standard := resourceBudgetCheck("standard", available)
	if standard.Status != "Pass" {
		t.Fatalf("standard status=%s details=%s", standard.Status, standard.Details)
	}
	full := resourceBudgetCheck("full", available)
	if full.Status != "Fail" || !strings.Contains(full.Remediation, "at least 9 GiB") {
		t.Fatalf("full check=%+v", full)
	}
}

func TestFullProfileAgeIdentityAcceptsEnvironmentCustody(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY", "present")
	if !fullProfileAgeIdentityAvailable(t.TempDir() + "/missing") {
		t.Fatal("environment-custodied identity was rejected")
	}
}

func TestStageLogWriterStreamsRedactedCompleteLines(t *testing.T) {
	var output bytes.Buffer
	writer := newStageLogWriter(&output, &sync.Mutex{}, "tools")

	if _, err := writer.Write([]byte("Downloading pinned ")); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("partial line was emitted: %q", output.String())
	}
	if _, err := writer.Write([]byte("archive\npassword=secret\r50%\r")); err != nil {
		t.Fatal(err)
	}
	writer.Flush()

	got := output.String()
	for _, expected := range []string{
		"[tools] Downloading pinned archive\n",
		"[tools] password= <redacted>\n",
		"[tools] 50%\n",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output %q does not contain %q", got, expected)
		}
	}
	if strings.Contains(got, "secret") || strings.Contains(writer.Tail(), "secret") {
		t.Fatalf("credential value leaked: output=%q tail=%q", got, writer.Tail())
	}
}

func TestStageLogWriterBoundsFailureTail(t *testing.T) {
	writer := newStageLogWriter(&bytes.Buffer{}, &sync.Mutex{}, "bootstrap")
	for index := 0; index < 25; index++ {
		_, _ = writer.Write([]byte("line\n"))
	}
	if got := strings.Count(writer.Tail(), "line"); got != 20 {
		t.Fatalf("tail contains %d lines, want 20", got)
	}
}
