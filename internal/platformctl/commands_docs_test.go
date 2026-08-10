package platformctl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandDocumentationGeneration(t *testing.T) {
	directory := t.TempDir()
	root := NewRootCommand(Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err := generateCommandDocs(root, directory); err != nil {
		t.Fatal(err)
	}
	secondDirectory := t.TempDir()
	if err := generateCommandDocs(NewRootCommand(Options{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}), secondDirectory); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"platformctl.md", "completions/platformctl.bash", "completions/_platformctl", "completions/platformctl.fish",
		"completions/platformctl.ps1", "man/platformctl-app-promote.1", "man/platformctl-app-abort.1",
	} {
		data, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(path)))
		if err != nil || len(data) == 0 {
			t.Fatalf("generated artifact %s is missing or empty: %v", path, err)
		}
		if strings.Contains(strings.ToLower(string(data)), "private_key") {
			t.Fatalf("generated docs leaked a secret-bearing key: %s", path)
		}
		second, secondErr := os.ReadFile(filepath.Join(secondDirectory, filepath.FromSlash(path)))
		if secondErr != nil || !bytes.Equal(data, second) {
			t.Fatalf("generated artifact %s is not deterministic: %v", path, secondErr)
		}
	}
	reference, _ := os.ReadFile(filepath.Join(directory, "platformctl.md"))
	for _, command := range []string{"platformctl init", "platformctl app doctor", "platformctl app promote", "platformctl service retire"} {
		if !strings.Contains(string(reference), "`"+command+"`") {
			t.Fatalf("reference omitted %s", command)
		}
	}
	if !strings.Contains(string(reference), "Tempo's raw OTLP/protobuf JSON response") {
		t.Fatal("reference omitted the trace ID encoding contract")
	}
}
