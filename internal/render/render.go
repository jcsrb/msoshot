// Package render defines what a renderer does, the options a client can pass,
// and portable image helpers (crop, stack, blank check, PNG).
package render

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Options are the validated query parameters of a render request.
type Options struct {
	WaitMs int    // wait after the window opens before capturing
	Width  int    // window size in logical (96-DPI) pixels
	Height int    //
	Crop   string // "body" (message body only) or "window" (whole Outlook window)
	Pages  int    // scrolled screens stacked into one image; 1 = visible part only
}

// DefaultOptions are used for anything the client doesn't set.
func DefaultOptions() Options {
	return Options{WaitMs: 1500, Width: 1000, Height: 900, Crop: "body", Pages: 1}
}

// ParseOptions validates query parameters, starting from the defaults.
func ParseOptions(q url.Values) (Options, error) {
	o := DefaultOptions()
	ints := []struct {
		key      string
		dst      *int
		min, max int
	}{
		{"wait", &o.WaitMs, 0, 30000},
		{"width", &o.Width, 300, 4000},
		{"height", &o.Height, 200, 4000},
		{"pages", &o.Pages, 1, 20},
	}
	for _, f := range ints {
		raw := q.Get(f.key)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < f.min || n > f.max {
			return o, BadRequest("%s must be an integer from %d to %d", f.key, f.min, f.max)
		}
		*f.dst = n
	}
	switch strings.ToLower(q.Get("full")) {
	case "1", "true", "yes", "on":
		o.Pages = max(o.Pages, 10)
	}
	if c := q.Get("crop"); c != "" {
		if c != "body" && c != "window" {
			return o, BadRequest("crop must be body or window")
		}
		o.Crop = c
	}
	return o, nil
}

// Result is a finished screenshot plus facts about how it was taken. Info is
// returned to clients as X-Render-* headers.
type Result struct {
	PNG  []byte
	Info map[string]string
}

// Health is a snapshot of whether rendering can work, shown in /health and the dashboard.
type Health map[string]any

// Renderer opens an .eml in Outlook and returns a screenshot.
type Renderer interface {
	// Render may be called concurrently; implementations serialise internally.
	Render(ctx context.Context, eml []byte, opt Options) (*Result, error)
	// Health returns a cached snapshot and never blocks on a render.
	Health() Health
	// Busy reports whether a render is in progress.
	Busy() bool
}

// Error carries an HTTP status for the API layer.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func BadRequest(format string, a ...any) error  { return &Error{400, fmt.Sprintf(format, a...)} }
func Unavailable(format string, a ...any) error { return &Error{503, fmt.Sprintf(format, a...)} }
func TimedOut(format string, a ...any) error    { return &Error{504, fmt.Sprintf(format, a...)} }
func Failed(format string, a ...any) error      { return &Error{500, fmt.Sprintf(format, a...)} }
