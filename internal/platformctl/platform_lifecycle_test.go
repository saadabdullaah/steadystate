package platformctl

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

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
