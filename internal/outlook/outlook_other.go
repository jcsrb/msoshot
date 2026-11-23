//go:build !windows

// Package outlook renders emails in classic Outlook for Windows. On other
// systems it only reports that it is unavailable; use the fake renderer there.
package outlook

import (
	"errors"
	"log/slog"

	"outlook-render/internal/render"
)

// New is only implemented on Windows.
func New(log *slog.Logger) (render.Renderer, error) {
	return nil, errors.New("the Outlook renderer only works on Windows; use --fake to try the app here")
}

// MakeDPIAware is a no-op off Windows.
func MakeDPIAware() {}
