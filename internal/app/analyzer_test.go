package app

import (
	"image"
	"image/color"
	"testing"
)

func TestDetectRectangles(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 400, 300))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	drawOutline := func(minX, minY, maxX, maxY int) {
		for x := minX; x <= maxX; x++ {
			img.SetGray(x, minY, color.Gray{Y: 0})
			img.SetGray(x, maxY, color.Gray{Y: 0})
		}
		for y := minY; y <= maxY; y++ {
			img.SetGray(minX, y, color.Gray{Y: 0})
			img.SetGray(maxX, y, color.Gray{Y: 0})
		}
	}
	drawOutline(30, 30, 70, 65)
	drawOutline(100, 30, 140, 65)
	objects := detectRectangles(img)
	if len(objects) != 2 {
		t.Fatalf("wanted 2 rectangle candidates, got %d", len(objects))
	}
	for _, object := range objects {
		if object.X < 0 || object.Y < 0 || object.X+object.W > 1 || object.Y+object.H > 1 {
			t.Fatalf("invalid normalized object: %#v", object)
		}
	}
}

func TestAnalysisMessage(t *testing.T) {
	if got := analysisMessage(0); got == "" {
		t.Fatal("empty guidance")
	}
	if got := analysisMessage(3); got == "" {
		t.Fatal("empty result")
	}
}
