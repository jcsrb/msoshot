package render

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"testing"
)

func TestParseOptions(t *testing.T) {
	o, err := ParseOptions(url.Values{})
	if err != nil || o != DefaultOptions() {
		t.Fatalf("defaults: %+v %v", o, err)
	}
	o, err = ParseOptions(url.Values{"wait": {"2000"}, "width": {"800"}, "crop": {"window"}, "pages": {"3"}})
	if err != nil || o.WaitMs != 2000 || o.Width != 800 || o.Crop != "window" || o.Pages != 3 {
		t.Fatalf("parsed: %+v %v", o, err)
	}
	if o, _ := ParseOptions(url.Values{"full": {"1"}}); o.Pages != 10 {
		t.Fatalf("full=1 should mean 10 pages, got %d", o.Pages)
	}
	for _, bad := range []url.Values{{"wait": {"-1"}}, {"width": {"abc"}}, {"crop": {"everything"}}, {"pages": {"21"}}} {
		if _, err := ParseOptions(bad); err == nil {
			t.Errorf("expected error for %v", bad)
		} else if e, ok := err.(*Error); !ok || e.Status != 400 {
			t.Errorf("expected 400 for %v, got %v", bad, err)
		}
	}
}

func fill(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func TestCropClamps(t *testing.T) {
	img := fill(10, 10, color.RGBA{1, 2, 3, 255})
	img.SetRGBA(8, 0, color.RGBA{9, 9, 9, 255})
	c := Crop(img, image.Rect(8, -2, 13, 2))
	if c.Bounds().Dx() != 2 || c.Bounds().Dy() != 2 || c.RGBAAt(0, 0) != (color.RGBA{9, 9, 9, 255}) {
		t.Fatalf("crop wrong: %v", c.Bounds())
	}
	if Crop(img, image.Rect(50, 50, 55, 55)) != img {
		t.Fatal("non-overlapping crop should return the input")
	}
}

func TestStack(t *testing.T) {
	a := fill(4, 2, color.RGBA{255, 0, 0, 255})
	b := fill(3, 3, color.RGBA{0, 0, 255, 255})
	s := Stack([]*image.RGBA{a, b})
	if s.Bounds().Dx() != 4 || s.Bounds().Dy() != 9 {
		t.Fatalf("size %v", s.Bounds())
	}
	grey := color.RGBA{208, 208, 208, 255}
	if s.RGBAAt(0, 0) != a.RGBAAt(0, 0) || s.RGBAAt(0, 2) != grey || s.RGBAAt(0, 6) != b.RGBAAt(0, 0) || s.RGBAAt(3, 6) != grey {
		t.Fatal("stack layout wrong")
	}
	if Stack([]*image.RGBA{a}) != a {
		t.Fatal("single part should be returned as is")
	}
}

func TestBlankAndSame(t *testing.T) {
	black := fill(50, 50, color.RGBA{0, 0, 0, 255})
	if !LooksBlank(black) {
		t.Fatal("black should look blank")
	}
	white := fill(50, 50, color.RGBA{255, 255, 255, 255})
	if LooksBlank(white) {
		t.Fatal("white is not blank")
	}
	if !SamePixels(white, fill(50, 50, color.RGBA{255, 255, 255, 255})) || SamePixels(white, black) {
		t.Fatal("SamePixels wrong")
	}
}

func TestEncodeAndThumbnail(t *testing.T) {
	img := fill(1500, 1350, color.RGBA{10, 20, 30, 255})
	data, err := EncodePNG(img)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := png.Decode(bytes.NewReader(data))
	if err != nil || dec.Bounds() != img.Bounds() {
		t.Fatalf("decode: %v", err)
	}
	th := Thumbnail(img, 300)
	if th.Bounds().Dx() != 300 || th.Bounds().Dy() != 270 {
		t.Fatalf("thumbnail %v", th.Bounds())
	}
}

func TestFake(t *testing.T) {
	f := &Fake{}
	res, err := f.Render(context.Background(), []byte("x"), DefaultOptions())
	if err != nil || len(res.PNG) == 0 || string(f.LastEML) != "x" {
		t.Fatalf("fake: %v", err)
	}
}
