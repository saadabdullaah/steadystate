package platformctl

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

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
		{name: "github-cli-version", blocking: false},
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
