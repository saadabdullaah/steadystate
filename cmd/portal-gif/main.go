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
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: portal-gif OUTPUT.gif FRAME.png FRAME.png [...]")
		os.Exit(2)
	}
	animation := &gif.GIF{LoopCount: 0}
	var bounds image.Rectangle
	for _, path := range os.Args[2:] {
		file, err := os.Open(path)
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
			fatal(fmt.Errorf("frame %s dimensions differ from the first frame", path))
		}
		paletted := image.NewPaletted(bounds, palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, bounds, frame, frame.Bounds().Min)
		animation.Image = append(animation.Image, paletted)
		animation.Delay = append(animation.Delay, 120)
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	output, err := os.Create(os.Args[1])
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
