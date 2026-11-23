package eml

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestBuildRoundTrips(t *testing.T) {
	html := []byte("<p>Hi ü</p>" + strings.Repeat("x", 200))
	msg := Build(html, "Weekly digest", "utf-8", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	s := string(msg)
	for _, want := range []string{
		"Subject: Weekly digest\r\n",
		"Content-Type: text/html; charset=\"utf-8\"\r\n",
		"Content-Transfer-Encoding: base64\r\n",
		"Date: Fri, 02 Jan 2026 03:04:05 +0000\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(s, "X-Unsent") {
		t.Error("must not mark the message as a draft")
	}
	body := s[strings.Index(s, "\r\n\r\n")+4:]
	for _, line := range strings.Split(strings.TrimSpace(body), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("base64 line longer than 76: %d", len(line))
		}
	}
	got, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
	if err != nil || !bytes.Equal(got, html) {
		t.Fatalf("body did not round-trip: %v", err)
	}
	if Subject(msg) != "Weekly digest" {
		t.Errorf("Subject() = %q", Subject(msg))
	}
}

func TestNonASCIISubject(t *testing.T) {
	msg := Build([]byte("x"), "Café ☕", "", time.Now())
	if !strings.Contains(string(msg), "Subject: =?utf-8?q?") {
		t.Errorf("subject not encoded: %s", msg)
	}
	if Subject(msg) != "Café ☕" {
		t.Errorf("Subject() = %q", Subject(msg))
	}
}

func TestSubjectCannotInjectHeaders(t *testing.T) {
	msg := string(Build([]byte("x"), "Hi\r\nBcc: someone@example.com", "utf-8", time.Now()))
	if strings.Contains(msg, "\r\nBcc:") {
		t.Fatal("header injection")
	}
}

func TestCharsetSanitised(t *testing.T) {
	msg := string(Build([]byte("x"), "s", "utf-8\"\r\nX: y", time.Now()))
	if !strings.Contains(msg, "charset=\"utf-8X:y\"") {
		t.Fatalf("charset not sanitised: %s", msg)
	}
	if !strings.Contains(string(Build([]byte("x"), "s", "\"\"", time.Now())), "charset=\"utf-8\"") {
		t.Fatal("empty charset should default to utf-8")
	}
}

func TestSubjectOfGarbage(t *testing.T) {
	if Subject([]byte("not a message")) != "" {
		t.Fatal("expected empty subject")
	}
}
