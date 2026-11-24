package render

import (
	"image"
	"image/color"
	"testing"
)

// page builds a w×h capture of a tall "document" scrolled to offset, with
// static frame rows (top and bottom) that don't scroll.
func page(doc *image.RGBA, offset, w, h, frame int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{200, 200, 200, 255} // frame
			if y >= frame && y < h-frame {
				c = doc.RGBAAt(x, offset+y)
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// document has distinct content on every row, with a blank band in the middle.
func document(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if (y < 300 || y > 360) && x == (y*7)%w {
				c = color.RGBA{byte(y), byte(y >> 8), 0, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestTrimOverlap(t *testing.T) {
	doc := document(120, 1000)
	const w, h, frame = 120, 200, 3
	a := page(doc, 0, w, h, frame)
	b := page(doc, 170, w, h, frame) // scrolled 170 of 200 rows: 30 overlap
	got, n, cut := TrimOverlap(a, b)
	if cut != frame || n != 30-frame {
		t.Fatalf("removed %d, cut %d; want %d and %d", n, cut, 30-frame, frame)
	}
	// prev without its bottom frame, then got, must be the document itself.
	joined := Join([]*image.RGBA{Crop(a, image.Rect(0, 0, w, h-cut)), got})
	for y := frame; y < joined.Bounds().Dy()-frame; y++ {
		for x := 0; x < w; x++ {
			if joined.RGBAAt(x, y) != doc.RGBAAt(x, y) {
				t.Fatalf("joined row %d differs from the document", y)
			}
		}
	}
}

func TestTrimOverlapNone(t *testing.T) {
	doc := document(120, 1000)
	a := page(doc, 0, 120, 200, 3)
	b := page(doc, 200, 120, 200, 3) // exact page step: nothing repeated
	if _, n, _ := TrimOverlap(a, b); n != 0 {
		t.Fatalf("removed %d rows, want 0", n)
	}
}

func TestTrimOverlapIgnoresBlank(t *testing.T) {
	doc := document(120, 1000)
	// Both captures meet inside the blank band only; it must not count.
	a := page(doc, 140, 120, 200, 0) // ends at 340 (inside blank 300..360)
	b := page(doc, 320, 120, 200, 0) // starts at 320
	if _, n, _ := TrimOverlap(a, b); n != 0 {
		t.Fatalf("removed %d rows of blank space, want 0", n)
	}
}

func TestTrimOverlapSizeMismatch(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 10, 50))
	b := image.NewRGBA(image.Rect(0, 0, 11, 50))
	if got, n, _ := TrimOverlap(a, b); n != 0 || got != b {
		t.Fatal("expected no change for different sizes")
	}
}
