package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// Crop returns the part of img inside r (in img's coordinates), clamped.
// If nothing overlaps it returns img unchanged.
func Crop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

// Stack places images top to bottom, separated by a thin grey rule.
func Stack(parts []*image.RGBA) *image.RGBA {
	if len(parts) == 1 {
		return parts[0]
	}
	const gap = 4
	w, h := 0, gap*(len(parts)-1)
	for _, p := range parts {
		w = max(w, p.Bounds().Dx())
		h += p.Bounds().Dy()
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{208, 208, 208, 255}}, image.Point{}, draw.Src)
	y := 0
	for _, p := range parts {
		dst := image.Rect(0, y, p.Bounds().Dx(), y+p.Bounds().Dy())
		draw.Draw(out, dst, p, p.Bounds().Min, draw.Src)
		y += p.Bounds().Dy() + gap
	}
	return out
}

// LooksBlank reports whether a grid of samples is all pure black, which is
// what a failed window capture looks like.
func LooksBlank(img *image.RGBA) bool {
	b := img.Bounds()
	for gy := 1; gy < 10; gy++ {
		for gx := 1; gx < 10; gx++ {
			c := img.RGBAAt(b.Min.X+b.Dx()*gx/10, b.Min.Y+b.Dy()*gy/10)
			if c.R != 0 || c.G != 0 || c.B != 0 {
				return false
			}
		}
	}
	return true
}

// SamePixels reports whether two images are identical (used to detect that
// scrolling reached the end).
func SamePixels(a, b *image.RGBA) bool {
	return a.Bounds().Size() == b.Bounds().Size() && bytes.Equal(a.Pix, b.Pix)
}

// EncodePNG encodes an opaque image as PNG.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Thumbnail scales img down to at most maxW pixels wide (nearest neighbour,
// good enough for a dashboard preview).
func Thumbnail(img image.Image, maxW int) image.Image {
	b := img.Bounds()
	if b.Dx() <= maxW {
		return img
	}
	h := b.Dy() * maxW / b.Dx()
	out := image.NewRGBA(image.Rect(0, 0, maxW, max(h, 1)))
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < maxW; x++ {
			out.Set(x, y, img.At(b.Min.X+x*b.Dx()/maxW, b.Min.Y+y*b.Dy()/out.Bounds().Dy()))
		}
	}
	return out
}
