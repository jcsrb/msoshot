// Package dashboard serves the small status UI shown in the app window. It is
// only reachable through the app's embedded web view, never over the network,
// so it can show the token and run test renders without authentication.
package dashboard

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jcsrb/outlook-render/internal/activity"
	"github.com/jcsrb/outlook-render/internal/api"
	"github.com/jcsrb/outlook-render/internal/config"
	"github.com/jcsrb/outlook-render/internal/eml"
	"github.com/jcsrb/outlook-render/internal/render"
	"github.com/jcsrb/outlook-render/internal/samples"
)

//go:embed index.html
var indexHTML []byte

// Autostart toggles starting the app at login. Optional.
type Autostart interface {
	Enabled() (bool, error)
	Set(enabled bool) error
}

type Deps struct {
	API            *api.Server
	Activity       *activity.Log
	Config         func() config.Config
	ApplyConfig    func(config.Config) error // validate, save and restart the listener
	ListenerStatus func() (addr, state string)
	CopyText       func(string) bool
	Autostart      Autostart
	Addresses      func() []config.Address
}

type handler struct{ d Deps }

func New(d Deps) http.Handler {
	h := &handler{d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.index)
	mux.HandleFunc("GET /ui/status", h.status)
	mux.HandleFunc("GET /ui/token", h.token)
	mux.HandleFunc("GET /ui/image/{id}", h.image)
	mux.HandleFunc("POST /ui/test", h.test)
	mux.HandleFunc("POST /ui/settings", h.settings)
	mux.HandleFunc("POST /ui/token/new", h.newToken)
	mux.HandleFunc("POST /ui/copy", h.copy)
	mux.HandleFunc("POST /ui/autostart", h.autostart)
	return mux
}

func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

// CurlExample is the command shown in the dashboard and copied from the tray.
func CurlExample(c config.Config) string {
	return fmt.Sprintf("curl -X POST 'http://%s:%d/render?subject=Test' \\\n"+
		"  -H 'Authorization: Bearer %s' \\\n"+
		"  -H 'Content-Type: text/html' \\\n"+
		"  --data-binary @email.html -o outlook.png", c.Host, c.Port, c.Token)
}

func tokenHint(t string) string {
	if len(t) < 10 {
		return "…"
	}
	return t[:4] + "…" + t[len(t)-4:]
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	c := h.d.Config()
	addr, state := h.d.ListenerStatus()
	out := map[string]any{
		"version": api.Version,
		"health":  h.d.API.Renderer.Health(),
		"busy":    h.d.API.Renderer.Busy(),
		"server": map[string]any{
			"host":       c.Host,
			"port":       c.Port,
			"addr":       addr,
			"state":      state,
			"token_hint": tokenHint(c.Token),
		},
		"addresses": h.d.Addresses(),
		"curl":      strings.Replace(CurlExample(c), c.Token, tokenHint(c.Token), 1),
		"recent":    h.d.Activity.Recent(),
	}
	if h.d.Autostart != nil {
		if on, err := h.d.Autostart.Enabled(); err == nil {
			out["autostart"] = on
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) token(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"token": h.d.Config().Token})
}

func (h *handler) image(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	png, ok := h.d.Activity.PNG(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(png)
}

func (h *handler) test(w http.ResponseWriter, r *http.Request) {
	msg := eml.Build(samples.HTML, "Sample: Outlook quirks", "utf-8", timeNow())
	_, _, status, errMsg := h.d.API.Do(context.WithoutCancel(r.Context()), "dashboard", msg, render.DefaultOptions())
	if status != http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": errMsg})
		return
	}
	recent := h.d.Activity.Recent()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": recent[0].ID})
}

func (h *handler) settings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "bad JSON"})
		return
	}
	c := h.d.Config()
	c.Host, c.Port = strings.TrimSpace(in.Host), in.Port
	if err := h.d.ApplyConfig(c); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) newToken(w http.ResponseWriter, r *http.Request) {
	c := h.d.Config()
	c.Token = config.NewToken()
	if err := h.d.ApplyConfig(c); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) copy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		What string `json:"what"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	c := h.d.Config()
	var text string
	switch in.What {
	case "token":
		text = c.Token
	case "curl":
		text = CurlExample(c)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "what must be token or curl"})
		return
	}
	ok := h.d.CopyText != nil && h.d.CopyText(text)
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok})
}

func (h *handler) autostart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	if h.d.Autostart == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "not supported"})
		return
	}
	if err := h.d.Autostart.Set(in.Enabled); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
