package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePathsConfinesFramesAndOutput(t *testing.T) {
	root := t.TempDir()
	screenshots := filepath.Join(root, "screenshots")
	if err := os.Mkdir(screenshots, 0o700); err != nil {
		t.Fatal(err)
	}
	frame := filepath.Join(screenshots, "01-overview.png")
	if err := os.WriteFile(frame, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "phase9-portal-golden-path.gif")
	outputRoot, frameRoot, names, err := validatePaths(output, []string{frame})
	if err != nil {
		t.Fatal(err)
	}
	if outputRoot != root || frameRoot != screenshots || len(names) != 1 || names[0] != filepath.Base(frame) {
		t.Fatalf("unexpected validated paths: output=%q frames=%q names=%v", outputRoot, frameRoot, names)
	}
	outside := filepath.Join(root, "outside.png")
	if err := os.WriteFile(outside, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := validatePaths(output, []string{outside}); err == nil {
		t.Fatal("frame outside the screenshots directory was accepted")
	}
	if _, _, _, err := validatePaths(filepath.Join(root, "unexpected.gif"), []string{frame}); err == nil {
		t.Fatal("unexpected output filename was accepted")
	}
}
