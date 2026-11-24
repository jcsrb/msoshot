// Package eml wraps an HTML body in a minimal RFC 5322 message that classic
// Outlook opens as a received email, and reads the subject back out of one.
package eml

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"
)

// Build wraps html in a message. It deliberately does not set "X-Unsent: 1":
// that header makes Outlook open the message as an editable draft with a Send
// button, which renders slightly differently and is one click from sending.
func Build(html []byte, subject, charset string, date time.Time) []byte {
	if charset = sanitizeToken(charset); charset == "" {
		charset = "utf-8"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: msoshot <render@example.com>\r\n")
	fmt.Fprintf(&b, "To: Preview <preview@example.com>\r\n")
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", oneLine(subject)))
	fmt.Fprintf(&b, "Date: %s\r\n", date.UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@msoshot.invalid>\r\n", randomID())
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/html; charset=\"%s\"\r\n", charset)
	fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n\r\n")

	enc := base64.StdEncoding.EncodeToString(html)
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString("\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	b.WriteString("\r\n")
	return b.Bytes()
}

// Subject returns the decoded Subject header of a message, or "" if there is none.
func Subject(msg []byte) string {
	m, err := mail.ReadMessage(bytes.NewReader(msg))
	if err != nil {
		return ""
	}
	raw := m.Header.Get("Subject")
	dec := new(mime.WordDecoder)
	if s, err := dec.DecodeHeader(raw); err == nil {
		return s
	}
	return raw
}

// oneLine strips CR/LF so a subject can't inject extra headers.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func sanitizeToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (r == '-' || r == '_' || r == '.' || r == ':' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func randomID() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
