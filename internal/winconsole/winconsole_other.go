//go:build !windows

package winconsole

// Attach is a no-op off Windows: console programs already have a console.
func Attach() {}
