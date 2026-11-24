package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsrb/msoshot/internal/activity"
	"github.com/jcsrb/msoshot/internal/api"
	"github.com/jcsrb/msoshot/internal/config"
	"github.com/jcsrb/msoshot/internal/render"
)

type fixture struct {
	cfg     config.Config
	copied  string
	applied int
	h       http.Handler
	act     *activity.Log
}

func newFixture() *fixture {
	f := &fixture{cfg: config.Config{Host: "127.0.0.1", Port: 8787, Token: "abcdefghijklmnopqrstuvwxyz0123456789"}}
	f.act = activity.New(10)
	srv := &api.Server{Renderer: &render.Fake{}, Token: func() string { return f.cfg.Token }, MaxBody: 1 << 20,
		Activity: f.act, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.h = New(Deps{
		API:      srv,
		Activity: f.act,
		Config:   func() config.Config { return f.cfg },
		ApplyConfig: func(c config.Config) error {
			if err := c.Validate(); err != nil {
				return err
			}
			f.cfg = c
			f.applied++
			return nil
		},
		ListenerStatus: func() (string, string) { return "127.0.0.1:8787", "listening" },
		CopyText:       func(s string) bool { f.copied = s; return true },
		Addresses:      config.LocalAddresses,
	})
	return f
}

func (f *fixture) call(method, path, body string) map[string]any {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestIndexAndStatusHideToken(t *testing.T) {
	f := newFixture()
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "msoshot") {
		t.Fatalf("index: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest("GET", "/ui/status", nil))
	if strings.Contains(rec.Body.String(), f.cfg.Token) {
		t.Fatal("status must not contain the full token")
	}
	s := f.call("GET", "/ui/status", "")
	if s["server"].(map[string]any)["state"] != "listening" {
		t.Fatalf("%v", s)
	}
}

func TestTestRenderAndImage(t *testing.T) {
	f := newFixture()
	r := f.call("POST", "/ui/test", "")
	if r["ok"] != true {
		t.Fatalf("%v", r)
	}
	id := int(r["id"].(float64))
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest("GET", "/ui/image/"+itoa(id), nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image: %d", rec.Code)
	}
	if got := f.act.Recent()[0]; got.Client != "dashboard" || got.Subject != "Sample: Outlook quirks" {
		t.Fatalf("%+v", got)
	}
}

func TestSettingsCopyToken(t *testing.T) {
	f := newFixture()
	if r := f.call("POST", "/ui/settings", `{"host":"not-an-ip","port":8787}`); r["ok"] != false {
		t.Fatal("invalid host accepted")
	}
	if r := f.call("POST", "/ui/settings", `{"host":"127.0.0.1","port":9000}`); r["ok"] != true || f.cfg.Port != 9000 {
		t.Fatalf("%v", r)
	}
	f.call("POST", "/ui/copy", `{"what":"curl"}`)
	if !strings.Contains(f.copied, "Bearer "+f.cfg.Token) || !strings.Contains(f.copied, ":9000/render") {
		t.Fatalf("copied: %s", f.copied)
	}
	old := f.cfg.Token
	f.call("POST", "/ui/token/new", "")
	if f.cfg.Token == old || len(f.cfg.Token) < 40 {
		t.Fatal("token not replaced")
	}
	if r := f.call("POST", "/ui/autostart", `{"enabled":true}`); r["ok"] != false {
		t.Fatal("autostart without support should fail")
	}
}

type fakeAutostart struct{ on bool }

func (a *fakeAutostart) Enabled() (bool, error) { return a.on, nil }
func (a *fakeAutostart) Set(on bool) error {
	if on && a.on {
		return errors.New("already")
	}
	a.on = on
	return nil
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestAutostart(t *testing.T) {
	f := newFixture()
	a := &fakeAutostart{}
	f.h = New(Deps{API: &api.Server{Renderer: &render.Fake{}}, Activity: f.act, Config: func() config.Config { return f.cfg },
		ListenerStatus: func() (string, string) { return "", "" }, Addresses: config.LocalAddresses, Autostart: a})
	if r := f.call("POST", "/ui/autostart", `{"enabled":true}`); r["ok"] != true || !a.on {
		t.Fatalf("%v", r)
	}
	if s := f.call("GET", "/ui/status", ""); s["autostart"] != true {
		t.Fatalf("%v", s["autostart"])
	}
}
