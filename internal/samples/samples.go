// Package samples embeds made-up example emails used by the dashboard's
// test button and the docs.
package samples

import _ "embed"

// HTML is a fictional newsletter that trips several classic Outlook quirks on purpose.
//
//go:embed sample.html
var HTML []byte

// EML is a complete fictional message.
//
//go:embed sample.eml
var EML []byte
