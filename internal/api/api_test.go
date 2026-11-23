package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"outlook-render/internal/activity"
	"outlook-render/internal/render"
)

const token = "test-token-0123456789"

func newServer(r render.Renderer) (*Server, http.Handler) {
	s := &Server{
		Renderer: r,
		Token:    func() string { return token },
		MaxBody:  1024,
		Activity: activity.New(10),
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return s, s.Handler()
}

func do(h http.Handler, method, target, contentType, body string, auth bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuth(t *testing.T) {
	_, h := newServer(&render.Fake{})
	if c := do(h, "GET", "/health", "", "", false).Code; c != 401 {
		t.Fatalf("no token: %d", c)
	}
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	if c := do(h, "GET", "/health", "", "", true).Code; c != 200 {
		t.Fatalf("good token: %d", c)
	}
	if !Authorized("bearer "+token, token) || Authorized("Bearer ", "") || Authorized("Basic "+token, token) {
		t.Fatal("Authorized() wrong")
	}
}

func TestHealth(t *testing.T) {
	s, h := newServer(&render.Fake{})
	s.Status = func() map[string]any { return map[string]any{"listen": "127.0.0.1:1"} }
	rec := do(h, "GET", "/health", "", "", true)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["renderer"] != "fake" || body["busy"] != false || body["listen"] != "127.0.0.1:1" {
		t.Fatalf("%v", body)
	}
}

func TestRenderHTML(t *testing.T) {
	fake := &render.Fake{}
	s, h := newServer(fake)
	rec := do(h, "POST", "/render?subject=Order+shipped&wait=100", "text/html; charset=utf-8", "<p>Hi</p>", true)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Render-Dpi") != "96" || rec.Header().Get("X-Render-Renderer") != "fake" {
		t.Fatalf("headers: %v", rec.Header())
	}
	if !strings.Contains(string(fake.LastEML), "Subject: Order shipped") || fake.LastOpts.WaitMs != 100 {
		t.Fatalf("eml/options not passed: %+v", fake.LastOpts)
	}
	recent := s.Activity.Recent()
	if len(recent) != 1 || recent[0].Subject != "Order shipped" || !recent[0].HasImage {
		t.Fatalf("activity: %+v", recent)
	}
}

func TestRenderEMLAndSubjectHeader(t *testing.T) {
	fake := &render.Fake{}
	_, h := newServer(fake)
	msg := "Subject: x\r\n\r\nbody"
	if c := do(h, "POST", "/render", "message/rfc822", msg, true).Code; c != 200 || string(fake.LastEML) != msg {
		t.Fatalf("eml passthrough: %d", c)
	}
	req := httptest.NewRequest("POST", "/render", strings.NewReader("<p>x</p>"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/html")
	req.Header.Set("X-Subject", "From header")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(string(fake.LastEML), "Subject: From header") {
		t.Fatal("X-Subject ignored")
	}
}

func TestRenderRejectsBadInput(t *testing.T) {
	_, h := newServer(&render.Fake{})
	cases := []struct {
		method, target, ct, body string
		want                     int
	}{
		{"POST", "/render", "text/html", "", 400},
		{"POST", "/render", "application/json", "{}", 400},
		{"POST", "/render", "", "x", 400},
		{"POST", "/render?wait=99999", "text/html", "x", 400},
		{"POST", "/render", "text/html", strings.Repeat("x", 2000), 413},
		{"GET", "/render", "", "", 405},
		{"GET", "/nope", "", "", 404},
	}
	for _, c := range cases {
		if got := do(h, c.method, c.target, c.ct, c.body, true).Code; got != c.want {
			t.Errorf("%s %s (%s): got %d want %d", c.method, c.target, c.ct, got, c.want)
		}
	}
}

type failing struct{}

func (failing) Render(context.Context, []byte, render.Options) (*render.Result, error) {
	return nil, render.Unavailable("Outlook is not running")
}
func (failing) Health() render.Health { return render.Health{} }
func (failing) Busy() bool            { return false }

func TestRendererErrorStatus(t *testing.T) {
	s, h := newServer(failing{})
	rec := do(h, "POST", "/render", "text/html", "x", true)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "not running") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if r := s.Activity.Recent(); len(r) != 1 || r[0].Status != 503 {
		t.Fatalf("activity: %+v", r)
	}
}

func TestHeaderCase(t *testing.T) {
	if headerCase("outlook_version") != "Outlook-Version" || headerCase("dpi") != "Dpi" {
		t.Fatal(headerCase("outlook_version"))
	}
}
