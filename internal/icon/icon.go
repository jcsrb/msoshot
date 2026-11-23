// Package icon draws the app's neutral envelope icon at runtime, so the repo
// needs no binary image files.
package icon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// PNG returns a size×size icon: a white envelope on a blue rounded square.
func PNG(size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	bg := color.NRGBA{47, 91, 211, 255}
	fg := color.NRGBA{255, 255, 255, 255}
	radius := s * 0.22
	stroke := math.Max(1.2, s*0.07)

	// envelope rectangle and flap, in pixels
	ex0, ey0, ex1, ey1 := s*0.2, s*0.3, s*0.8, s*0.72
	midX, flapY := s*0.5, s*0.53

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			a := roundedRectCoverage(px, py, s, radius)
			if a == 0 {
				continue
			}
			c := bg
			if onEnvelope(px, py, ex0, ey0, ex1, ey1, midX, flapY, stroke) {
				c = fg
			}
			c.A = uint8(255 * a)
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func roundedRectCoverage(px, py, s, r float64) float64 {
	cx := math.Max(r, math.Min(s-r, px))
	cy := math.Max(r, math.Min(s-r, py))
	d := math.Hypot(px-cx, py-cy)
	return math.Max(0, math.Min(1, r-d+0.5))
}

func onEnvelope(px, py, x0, y0, x1, y1, midX, flapY, w float64) bool {
	inside := px >= x0-w/2 && px <= x1+w/2 && py >= y0-w/2 && py <= y1+w/2
	if !inside {
		return false
	}
	// outline
	if px <= x0+w/2 || px >= x1-w/2 || py <= y0+w/2 || py >= y1-w/2 {
		return true
	}
	// flap: lines from top corners to the middle
	return distToSegment(px, py, x0, y0, midX, flapY) <= w/2 || distToSegment(px, py, x1, y0, midX, flapY) <= w/2
}

func distToSegment(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}
