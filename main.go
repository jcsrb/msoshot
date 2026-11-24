// msoshot renders emails in classic Outlook for Windows and returns
// screenshots over HTTP. It runs as a tray app with a small dashboard.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jcsrb/msoshot/internal/activity"
	"github.com/jcsrb/msoshot/internal/api"
	"github.com/jcsrb/msoshot/internal/config"
	"github.com/jcsrb/msoshot/internal/dashboard"
	"github.com/jcsrb/msoshot/internal/eml"
	"github.com/jcsrb/msoshot/internal/listener"
	"github.com/jcsrb/msoshot/internal/outlook"
	"github.com/jcsrb/msoshot/internal/render"
	"github.com/jcsrb/msoshot/internal/ui"
	"github.com/jcsrb/msoshot/internal/winconsole"
)

const usage = `msoshot %s: renders emails in classic Outlook and returns screenshots.

Usage:
  msoshot                 tray app with a dashboard (the normal way to run it)
  msoshot --headless      server only, no window or tray; logs to the console
  msoshot --check         print what the app can see (Outlook, display, session) and exit
  msoshot --render-file email.html out.png [--query "pages=2&wait=2000"]
                                 render one file without the server (.html or .eml)

Options:
`

func main() {
	// Must happen before any window exists, or captures are blurry at >100% scaling.
	outlook.MakeDPIAware()

	var (
		headless   = flag.Bool("headless", false, "run the server without the tray and window")
		check      = flag.Bool("check", false, "print health as JSON and exit")
		renderFile = flag.Bool("render-file", false, "render <input> <output.png> and exit")
		query      = flag.String("query", "", "render options for --render-file, e.g. \"pages=2&wait=2000\"")
		fake       = flag.Bool("fake", false, "use a fake renderer (for development on any OS)")
		minimized  = flag.Bool("minimized", false, "start with the window hidden")
		host       = flag.String("host", "", "IPv4 address to listen on for this run (default: saved setting, initially 127.0.0.1)")
		port       = flag.Int("port", 0, "port for this run (default: saved setting, initially 8787)")
		cfgPath    = flag.String("config", "", "config file (default: <user config dir>/msoshot/config.json)")
		maxBodyMB  = flag.Int("max-body-mb", 10, "largest accepted request body in MB")
		version    = flag.Bool("version", false, "print the version and exit")
	)
	flag.Usage = func() {
		winconsole.Attach()
		fmt.Fprintf(os.Stderr, usage, api.Version)
		flag.PrintDefaults()
	}
	args := parseInterspersed(flag.CommandLine, os.Args[1:])

	cli := *check || *renderFile || *headless || *version
	if cli {
		winconsole.Attach()
	}
	if *version {
		fmt.Println("msoshot", api.Version)
		return
	}

	log := newLogger(cli)

	renderer, err := newRenderer(*fake, log)
	if err != nil {
		fatal(log, err)
	}

	switch {
	case *check:
		os.Exit(runCheck(renderer))
	case *renderFile:
		os.Exit(runRenderFile(renderer, args, *query))
	}

	path := *cfgPath
	if path == "" {
		if path, err = config.DefaultPath(); err != nil {
			fatal(log, err)
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		fatal(log, err)
	}
	if *host != "" {
		cfg.Host = *host
	}
	if *port != 0 {
		cfg.Port = *port
	}
	if err := cfg.Validate(); err != nil {
		fatal(log, err)
	}
	st := &state{cfg: *cfg, path: path}

	acts := activity.New(20)
	srv := &api.Server{
		Renderer: renderer,
		Token:    func() string { return st.get().Token },
		MaxBody:  int64(*maxBodyMB) << 20,
		Activity: acts,
		Log:      log,
	}
	lis := listener.New(srv.Handler(), log)
	srv.Status = func() map[string]any {
		addr, s := lis.Status()
		return map[string]any{"listen": addr, "listen_state": s}
	}
	st.listener = lis
	lis.Start(cfg.Host, cfg.Port)
	if cfg.Host == "0.0.0.0" {
		log.Warn("listening on every network interface; prefer a specific private address")
	}

	if *headless {
		fmt.Printf("msoshot %s listening on %s:%d (config: %s)\n", api.Version, cfg.Host, cfg.Port, path)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
		lis.Stop()
		return
	}

	dash := dashboard.New(dashboard.Deps{
		API:            srv,
		Activity:       acts,
		Config:         st.get,
		ApplyConfig:    st.apply,
		ListenerStatus: lis.Status,
		CopyText:       ui.CopyText,
		Autostart:      ui.Autostart{},
		Addresses:      config.LocalAddresses,
	})
	err = ui.Run(ui.Options{
		Dashboard:   dash,
		Minimized:   *minimized,
		CurlExample: func() string { return dashboard.CurlExample(st.get()) },
		Tooltip:     func() string { return tooltip(renderer, lis) },
		Log:         log,
	})
	lis.Stop()
	if err != nil {
		fatal(log, err)
	}
}

// parseInterspersed parses flags wherever they appear, so
// "--render-file in.html out.png --query pages=2" works. Go's flag package
// alone stops at the first non-flag argument. Returns the positional arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) []string {
	var positional []string
	for {
		_ = fs.Parse(args) // ExitOnError: exits on bad flags
		rest := fs.Args()
		if len(rest) == 0 {
			return positional
		}
		if rest[0] == "--" {
			return append(positional, rest[1:]...)
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// state holds the live config; the dashboard can change it.
type state struct {
	mu       sync.Mutex
	cfg      config.Config
	path     string
	listener *listener.Listener
}

func (s *state) get() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *state) apply(c config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := config.Save(s.path, &c); err != nil {
		return err
	}
	s.mu.Lock()
	old := s.cfg
	s.cfg = c
	s.mu.Unlock()
	if old.Host != c.Host || old.Port != c.Port {
		s.listener.Start(c.Host, c.Port)
	}
	return nil
}

func tooltip(r render.Renderer, lis *listener.Listener) string {
	addr, s := lis.Status()
	h := r.Health()
	switch {
	case r.Busy():
		return "msoshot: rendering…"
	case h["ok"] == false && h["error"] != nil:
		return truncate(fmt.Sprintf("msoshot: %v", h["error"]), 120)
	case s != "listening":
		return truncate("msoshot: "+s, 120)
	default:
		return "msoshot: listening on " + addr
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func newRenderer(fake bool, log *slog.Logger) (render.Renderer, error) {
	if fake {
		log.Info("using the fake renderer")
		return &render.Fake{Delay: 800 * time.Millisecond}, nil
	}
	return outlook.New(log)
}

func runCheck(r render.Renderer) int {
	deadline := time.Now().Add(15 * time.Second)
	h := r.Health()
	for h["status"] == "starting" && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		h = r.Health()
	}
	out, _ := json.MarshalIndent(h, "", "  ")
	fmt.Println(string(out))
	if h["ok"] == false {
		return 1
	}
	return 0
}

func runRenderFile(r render.Renderer, args []string, query string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: msoshot --render-file <email.html|email.eml> <out.png> [--query \"pages=2\"]")
		return 2
	}
	in, out := args[0], args[1]
	data, err := os.ReadFile(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad --query:", err)
		return 2
	}
	opt, err := render.ParseOptions(q)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	msg := data
	if !strings.EqualFold(filepath.Ext(in), ".eml") {
		subject := q.Get("subject")
		if subject == "" {
			subject = filepath.Base(in)
		}
		msg = eml.Build(data, subject, "utf-8", time.Now())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opt.WaitMs)*time.Millisecond+time.Duration(opt.Pages)*3*time.Second+60*time.Second)
	defer cancel()
	res, err := r.Render(ctx, msg, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "render failed:", err)
		return 1
	}
	if err := os.WriteFile(out, res.PNG, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	info, _ := json.MarshalIndent(res.Info, "", "  ")
	fmt.Printf("wrote %s\n%s\n", out, info)
	return 0
}

// newLogger writes to <user cache dir>/msoshot/msoshot.log, and
// also to stderr in console modes. Only metadata is logged, never email
// content, subjects or the token.
func newLogger(console bool) *slog.Logger {
	var writers []io.Writer
	if console {
		writers = append(writers, os.Stderr)
	}
	if dir, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(dir, "msoshot")
		if os.MkdirAll(dir, 0o700) == nil {
			path := filepath.Join(dir, "msoshot.log")
			if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
				_ = os.Rename(path, path+".old")
			}
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				writers = append(writers, f)
			}
		}
	}
	if len(writers) == 0 {
		writers = append(writers, io.Discard)
	}
	return slog.New(slog.NewTextHandler(io.MultiWriter(writers...), nil))
}

func fatal(log *slog.Logger, err error) {
	log.Error("fatal", "err", err)
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
