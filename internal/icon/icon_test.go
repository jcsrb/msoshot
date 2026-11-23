package icon

import (
	"bytes"
	"image/png"
	"testing"
)

func TestPNG(t *testing.T) {
	for _, size := range []int{16, 32, 256} {
		img, err := png.Decode(bytes.NewReader(PNG(size)))
		if err != nil || img.Bounds().Dx() != size {
			t.Fatalf("size %d: %v", size, err)
		}
	}
}
