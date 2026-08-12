// Command portal-gif creates a deterministic animated GIF from ordered PNG frames.
package main

import (
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: portal-gif OUTPUT.gif FRAME.png FRAME.png [...]")
		os.Exit(2)
	}
	outputDirectory, frameDirectory, frameNames, err := validatePaths(os.Args[1], os.Args[2:])
	if err != nil {
		fatal(err)
	}
	// #nosec G703 -- validatePaths confines this root to a real, non-symlink acceptance screenshots directory.
	frameRoot, err := os.OpenRoot(frameDirectory)
	if err != nil {
		fatal(err)
	}
	defer frameRoot.Close()
	animation := &gif.GIF{LoopCount: 0}
	var bounds image.Rectangle
	for _, name := range frameNames {
		file, err := frameRoot.Open(name)
		if err != nil {
			fatal(err)
		}
		frame, err := png.Decode(file)
		_ = file.Close()
		if err != nil {
			fatal(err)
		}
		if bounds.Empty() {
			bounds = frame.Bounds()
		} else if frame.Bounds().Dx() != bounds.Dx() || frame.Bounds().Dy() != bounds.Dy() {
			fatal(fmt.Errorf("frame %s dimensions differ from the first frame", name))
		}
		paletted := image.NewPaletted(bounds, palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, bounds, frame, frame.Bounds().Min)
		animation.Image = append(animation.Image, paletted)
		animation.Delay = append(animation.Delay, 120)
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	// #nosec G703 -- validatePaths requires a real, non-symlink parent and fixes the output filename below.
	outputRoot, err := os.OpenRoot(outputDirectory)
	if err != nil {
		fatal(err)
	}
	defer outputRoot.Close()
	output, err := outputRoot.Create("phase9-portal-golden-path.gif")
	if err != nil {
		fatal(err)
	}
	if err = gif.EncodeAll(output, animation); err != nil {
		_ = output.Close()
		fatal(err)
	}
	if err = output.Close(); err != nil {
		fatal(err)
	}
}

func validatePaths(outputArgument string, frameArguments []string) (string, string, []string, error) {
	output, err := filepath.Abs(outputArgument)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve output path: %w", err)
	}
	output = filepath.Clean(output)
	if filepath.Base(output) != "phase9-portal-golden-path.gif" {
		return "", "", nil, fmt.Errorf("output filename must be phase9-portal-golden-path.gif")
	}
	outputDirectory := filepath.Dir(output)
	if err := requireRealDirectory(outputDirectory); err != nil {
		return "", "", nil, fmt.Errorf("validate output directory: %w", err)
	}
	if info, statErr := os.Lstat(output); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", nil, fmt.Errorf("output must not be a symbolic link")
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", "", nil, fmt.Errorf("validate output: %w", statErr)
	}
	frameDirectory := filepath.Join(outputDirectory, "screenshots")
	if err := requireRealDirectory(frameDirectory); err != nil {
		return "", "", nil, fmt.Errorf("validate screenshot directory: %w", err)
	}
	names := make([]string, 0, len(frameArguments))
	for _, argument := range frameArguments {
		frame, absErr := filepath.Abs(argument)
		if absErr != nil {
			return "", "", nil, fmt.Errorf("resolve frame path: %w", absErr)
		}
		frame = filepath.Clean(frame)
		if filepath.Dir(frame) != frameDirectory || filepath.Ext(frame) != ".png" {
			return "", "", nil, fmt.Errorf("frame %q must be a PNG directly inside the acceptance screenshots directory", filepath.Base(frame))
		}
		info, statErr := os.Lstat(frame)
		if statErr != nil {
			return "", "", nil, fmt.Errorf("validate frame %q: %w", filepath.Base(frame), statErr)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", nil, fmt.Errorf("frame %q must be a regular non-symlink file", filepath.Base(frame))
		}
		names = append(names, filepath.Base(frame))
	}
	return outputDirectory, frameDirectory, names, nil
}

func requireRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a real directory", path)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
