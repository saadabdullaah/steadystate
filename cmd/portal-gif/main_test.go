package main

import (
	"image"
	"image/color"
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

func TestComposeFramesLetterboxesResponsiveScreenshots(t *testing.T) {
	desktop := image.NewRGBA(image.Rect(0, 0, 144, 90))
	mobile := image.NewRGBA(image.Rect(0, 0, 39, 84))
	desktop.Set(0, 0, color.White)
	mobile.Set(0, 0, color.White)

	animation, err := composeFrames([]image.Image{desktop, mobile})
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Image) != 2 || len(animation.Delay) != 2 {
		t.Fatalf("unexpected animation sizes: frames=%d delays=%d", len(animation.Image), len(animation.Delay))
	}
	for index, frame := range animation.Image {
		if frame.Bounds().Dx() != 144 || frame.Bounds().Dy() != 90 {
			t.Fatalf("frame %d bounds=%v, want 144x90", index, frame.Bounds())
		}
	}
}

func TestComposeFramesRejectsInvalidInputs(t *testing.T) {
	if _, err := composeFrames([]image.Image{image.NewRGBA(image.Rect(0, 0, 10, 10))}); err == nil {
		t.Fatal("single-frame animation was accepted")
	}
	if _, err := composeFrames([]image.Image{image.NewRGBA(image.Rect(0, 0, 4097, 10)), image.NewRGBA(image.Rect(0, 0, 10, 10))}); err == nil {
		t.Fatal("oversized animation was accepted")
	}
}
