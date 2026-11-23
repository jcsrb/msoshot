package render

import (
	"context"
	"image"
	"image/color"
	"sync"
	"time"
)

// Fake returns a small placeholder image. It lets the server and dashboard
// run on any OS for development.
type Fake struct {
	mu       sync.Mutex
	busy     bool
	LastEML  []byte
	LastOpts Options
	Delay    time.Duration
}

func (f *Fake) Render(ctx context.Context, eml []byte, opt Options) (*Result, error) {
	f.mu.Lock()
	f.busy = true
	f.LastEML, f.LastOpts = eml, opt
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.busy = false; f.mu.Unlock() }()

	select {
	case <-time.After(f.Delay):
	case <-ctx.Done():
		return nil, TimedOut("render cancelled")
	}

	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 300; x++ {
			c := color.RGBA{244, 244, 240, 255}
			if y < 40 {
				c = color.RGBA{46, 125, 50, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	png, err := EncodePNG(img)
	if err != nil {
		return nil, err
	}
	return &Result{PNG: png, Info: map[string]string{"renderer": "fake", "dpi": "96", "scale": "1.00"}}, nil
}

func (f *Fake) Health() Health {
	return Health{"ok": true, "renderer": "fake", "outlook": "not checked (fake renderer)"}
}

func (f *Fake) Busy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy
}
