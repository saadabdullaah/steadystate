package platformctl

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestRunCommandInProcessTreeStopsNestedChildAtDeadline(t *testing.T) {
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.Command("powershell.exe", "-NoProfile", "-Command", "& powershell.exe -NoProfile -Command 'Start-Sleep -Seconds 60'")
	} else {
		command = exec.Command("sh", "-c", "sh -c 'sleep 60'")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := runCommandInProcessTree(ctx, command)
	if err == nil {
		t.Fatal("expected deadline error")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("nested process tree exceeded bounded shutdown: %s", elapsed)
	}
}
