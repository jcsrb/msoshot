// Package api is the network-facing HTTP API: POST /render and GET /health,
// both behind a bearer token. The dashboard is NOT served here.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"outlook-render/internal/activity"
	"outlook-render/internal/eml"
	"outlook-render/internal/render"
)

const Version = "0.1.0"

type Server struct {
	Renderer render.Renderer
	Token    func() string // read on every request, so the token can be changed at runtime
	MaxBody  int64
	Activity *activity.Log
	Log      *slog.Logger
	// Status adds listener/server facts to /health. Optional.
	Status func() map[string]any
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/render", s.render)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "unknown path; try GET /health or POST /render")
	})
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Authorized(r.Header.Get("Authorization"), s.Token()) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "missing or wrong token; send Authorization: Bearer <token>")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Authorized checks "Bearer <token>" in constant time.
func Authorized(header, token string) bool {
	const prefix = "bearer "
	if token == "" || len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	given := strings.TrimSpace(header[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(given), []byte(token)) == 1
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "use GET")
		return
	}
	out := map[string]any{}
	for k, v := range s.Renderer.Health() {
		out[k] = v
	}
	out["server"] = "outlook-render " + Version
	out["busy"] = s.Renderer.Busy()
	if s.Status != nil {
		for k, v := range s.Status() {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	opt, err := render.ParseOptions(r.URL.Query())
	if err != nil {
		writeRenderError(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.MaxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("body larger than %d bytes", s.MaxBody))
			return
		}
		writeError(w, http.StatusBadRequest, "could not read body: "+err.Error())
		return
	}
	msg, err := MessageFromRequest(r.Header.Get("Content-Type"), r.URL.Query().Get("subject"), r.Header.Get("X-Subject"), body)
	if err != nil {
		writeRenderError(w, err)
		return
	}

	client, _, _ := net.SplitHostPort(r.RemoteAddr)
	png, info, status, errMsg := s.Do(r.Context(), client, msg, opt)
	if status != http.StatusOK {
		writeError(w, status, errMsg)
		return
	}
	keys := make([]string, 0, len(info))
	for k := range info {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.Header().Set("X-Render-"+headerCase(k), oneLine(info[k]))
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", fmt.Sprint(len(png)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// Do renders a message and records it in the activity log. Used by the API
// and by the dashboard's test button.
func (s *Server) Do(ctx context.Context, client string, msg []byte, opt render.Options) (png []byte, info map[string]string, status int, errMsg string) {
	timeout := time.Duration(opt.WaitMs)*time.Millisecond + time.Duration(opt.Pages)*3*time.Second + 60*time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	res, err := s.Renderer.Render(ctx, msg, opt)
	entry := activity.Entry{
		Time:       started,
		Client:     client,
		Subject:    eml.Subject(msg),
		DurationMs: time.Since(started).Milliseconds(),
	}
	if err != nil {
		status, errMsg = http.StatusInternalServerError, err.Error()
		var re *render.Error
		if errors.As(err, &re) {
			status = re.Status
		}
		entry.Status, entry.Error = status, errMsg
		s.Activity.Add(entry, nil)
		// Metadata only: subjects and bodies are never logged.
		s.Log.Warn("render failed", "client", client, "status", status, "ms", entry.DurationMs, "err", errMsg)
		return nil, nil, status, errMsg
	}
	entry.Status, entry.Info = http.StatusOK, res.Info
	s.Activity.Add(entry, res.PNG)
	s.Log.Info("rendered", "client", client, "ms", entry.DurationMs, "bytes", len(res.PNG))
	return res.PNG, res.Info, http.StatusOK, ""
}

// MessageFromRequest accepts a whole message (message/rfc822) or HTML plus a subject.
func MessageFromRequest(contentType, querySubject, headerSubject string, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, render.BadRequest("empty body; POST an .eml (message/rfc822) or an HTML document (text/html)")
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, render.BadRequest("Content-Type must be message/rfc822 or text/html")
	}
	switch mediaType {
	case "message/rfc822":
		return body, nil
	case "text/html":
		subject := firstNonEmpty(querySubject, headerSubject, "Outlook render test")
		return eml.Build(body, subject, firstNonEmpty(params["charset"], "utf-8"), time.Now()), nil
	default:
		return nil, render.BadRequest("Content-Type must be message/rfc822 (an .eml) or text/html, got %s", mediaType)
	}
}

func writeRenderError(w http.ResponseWriter, err error) {
	var re *render.Error
	if errors.As(err, &re) {
		writeError(w, re.Status, re.Msg)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// headerCase turns "outlook_version" into "Outlook-Version".
func headerCase(k string) string {
	parts := strings.FieldsFunc(k, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "-")
}

func oneLine(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
