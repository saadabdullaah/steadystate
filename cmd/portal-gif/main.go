// Command portal-gif creates a deterministic animated GIF from ordered PNG frames.
package main

import (
	"fmt"
	"image"
	"image/color"
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
	defer func() { _ = frameRoot.Close() }()
	frames := make([]image.Image, 0, len(frameNames))
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
		frames = append(frames, frame)
	}
	animation, err := composeFrames(frames)
	if err != nil {
		fatal(err)
	}
	// #nosec G703 -- validatePaths requires a real, non-symlink parent and fixes the output filename below.
	outputRoot, err := os.OpenRoot(outputDirectory)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = outputRoot.Close() }()
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

func composeFrames(frames []image.Image) (*gif.GIF, error) {
	if len(frames) < 2 {
		return nil, fmt.Errorf("at least two frames are required")
	}
	const maximumDimension = 4096
	width, height := 0, 0
	for index, frame := range frames {
		if frame == nil || frame.Bounds().Empty() {
			return nil, fmt.Errorf("frame %d is empty", index+1)
		}
		width = max(width, frame.Bounds().Dx())
		height = max(height, frame.Bounds().Dy())
	}
	if width > maximumDimension || height > maximumDimension {
		return nil, fmt.Errorf("frame canvas %dx%d exceeds the %d-pixel dimension limit", width, height, maximumDimension)
	}

	bounds := image.Rect(0, 0, width, height)
	animation := &gif.GIF{LoopCount: 0}
	background := color.RGBA{R: 13, G: 20, B: 24, A: 255}
	for _, frame := range frames {
		canvas := image.NewRGBA(bounds)
		draw.Draw(canvas, bounds, &image.Uniform{C: background}, image.Point{}, draw.Src)
		offset := image.Pt((width-frame.Bounds().Dx())/2, (height-frame.Bounds().Dy())/2)
		target := image.Rectangle{Min: offset, Max: offset.Add(frame.Bounds().Size())}
		draw.Draw(canvas, target, frame, frame.Bounds().Min, draw.Src)
		paletted := image.NewPaletted(bounds, palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, bounds, canvas, image.Point{})
		animation.Image = append(animation.Image, paletted)
		animation.Delay = append(animation.Delay, 120)
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	return animation, nil
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
