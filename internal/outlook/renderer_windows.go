//go:build windows

// Package outlook renders emails in classic Outlook for Windows through its
// COM interface and captures the message window with Win32/GDI.
package outlook

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"outlook-render/internal/render"
)

// Renderer drives classic Outlook. All COM and window work happens on one
// goroutine locked to one OS thread, because COM objects belong to the
// thread (apartment) that created them.
type Renderer struct {
	log     *slog.Logger
	tempDir string
	jobs    chan job
	busy    atomic.Bool

	healthMu sync.Mutex
	health   render.Health
}

type job struct {
	ctx    context.Context
	path   string
	opt    render.Options
	result chan jobResult // buffered: the worker never blocks on a caller that gave up
}

type jobResult struct {
	res *render.Result
	err error
}

// New starts the COM worker.
func New(log *slog.Logger) (render.Renderer, error) {
	r := &Renderer{
		log:     log,
		tempDir: filepath.Join(os.TempDir(), "outlook-render"),
		jobs:    make(chan job, 16),
		health:  render.Health{"ok": false, "status": "starting"},
	}
	if err := os.MkdirAll(r.tempDir, 0o700); err != nil {
		return nil, err
	}
	r.sweepTemp()
	ready := make(chan error, 1)
	go r.worker(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Renderer) worker(ready chan<- error) {
	runtime.LockOSThread() // never unlocked: this thread is the COM apartment for life
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		// S_FALSE (already initialised) is reported as an error by some versions; tolerate it.
		if oe, ok := err.(*ole.OleError); !ok || oe.Code() != 1 {
			ready <- fmt.Errorf("CoInitializeEx: %w", err)
			return
		}
	}
	ready <- nil
	r.refreshHealth()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case j := <-r.jobs:
			if j.ctx.Err() != nil { // caller already gave up
				r.removeLater(j.path)
				continue
			}
			r.busy.Store(true)
			res, err := r.renderOnce(j.ctx, j.path, j.opt)
			r.busy.Store(false)
			r.removeLater(j.path)
			j.result <- jobResult{res, err}
			r.refreshHealth()
		case <-ticker.C:
			r.refreshHealth()
		}
	}
}

// Render queues a render and waits for it, or for ctx to end.
func (r *Renderer) Render(ctx context.Context, eml []byte, opt render.Options) (*render.Result, error) {
	f, err := os.CreateTemp(r.tempDir, "render-*.eml")
	if err != nil {
		return nil, render.Failed("could not write temp file: %v", err)
	}
	_, err = f.Write(eml)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return nil, render.Failed("could not write temp file: %v", err)
	}

	j := job{ctx: ctx, path: f.Name(), opt: opt, result: make(chan jobResult, 1)}
	select {
	case r.jobs <- j:
	case <-ctx.Done():
		os.Remove(f.Name())
		return nil, render.TimedOut("timed out waiting in the queue")
	}
	select {
	case res := <-j.result:
		return res.res, res.err
	case <-ctx.Done():
		return nil, render.TimedOut("render took too long; an Outlook dialog may be waiting for input")
	}
}

func (r *Renderer) Busy() bool { return r.busy.Load() }

func (r *Renderer) Health() render.Health {
	r.healthMu.Lock()
	defer r.healthMu.Unlock()
	out := render.Health{}
	for k, v := range r.health {
		out[k] = v
	}
	return out
}

// refreshHealth runs on the worker thread. It never starts Outlook.
func (r *Renderer) refreshHealth() {
	sid := sessionID()
	h := render.Health{
		"ok":                  true,
		"renderer":            "outlook",
		"classic_installed":   classicInstalled(),
		"outlook_running":     outlookRunning(),
		"outlook_com":         false,
		"dpi":                 systemDPI(),
		"scale":               fmt.Sprintf("%.2f", float64(systemDPI())/96),
		"session_id":          sid,
		"interactive_session": sid != 0,
		"elevated":            windows.GetCurrentProcessToken().IsElevated(),
		"checked_at":          time.Now().Format(time.RFC3339),
	}
	if h["outlook_running"] == true {
		if app, err := attach(false, true); err == nil {
			h["outlook_com"] = true
			if v, err := getString(app, "Version"); err == nil {
				h["outlook_version"] = v
			}
			app.Release()
		}
	}
	switch {
	case h["classic_installed"] != true:
		h["ok"], h["error"] = false, "Classic Outlook is not installed (the new Outlook has no COM interface)."
	case sid == 0:
		h["ok"], h["error"] = false, "Not running in an interactive desktop session; screenshots would be blank."
	case h["outlook_running"] == true && h["outlook_com"] != true:
		h["ok"], h["error"] = false, "Outlook is running but can't be reached. Run this app at the same elevation as Outlook (usually: not as administrator)."
	case h["outlook_running"] != true:
		h["note"] = "Outlook is not running; the first render will start it."
	}
	r.healthMu.Lock()
	r.health = h
	r.healthMu.Unlock()
}

// renderOnce opens the file in Outlook, captures it and closes it. It never
// sends, saves or moves anything and never quits Outlook.
func (r *Renderer) renderOnce(ctx context.Context, path string, opt render.Options) (res *render.Result, err error) {
	if !classicInstalled() {
		return nil, render.Unavailable("Classic Outlook is not installed (the new Outlook has no COM interface).")
	}
	if sessionID() == 0 {
		return nil, render.Unavailable("Not running in an interactive desktop session; start the app from your logged-in desktop.")
	}
	running := outlookRunning()
	app, err := attach(true, running)
	if err != nil {
		if running {
			return nil, render.Unavailable("Outlook is running but can't be reached (%v). Run this app at the same elevation as Outlook (usually: not as administrator).", err)
		}
		return nil, render.Unavailable("Could not start Outlook: %v", err)
	}
	defer release(app)
	version, _ := getString(app, "Version")

	item, inspector, openMethod, err := r.open(app, path)
	if err != nil {
		return nil, render.Failed("%v", err)
	}
	defer func() {
		// Close without saving (olDiscard = 1). Runs on success, failure and timeout.
		if inspector != nil {
			_ = call(inspector, "Close", 1)
		} else if item != nil {
			_ = call(item, "Close", 1)
		}
		release(inspector, item)
	}()

	_ = call(inspector, "Activate")
	time.Sleep(300 * time.Millisecond)

	win := hwndOf(inspector)
	if win == 0 {
		if fg := foregroundWindow(); className(fg) == classInspector {
			win = fg
		}
	}
	if win == 0 {
		if subject, _ := getString(item, "Subject"); subject != "" {
			win = findTopLevel(classInspector, subject)
		}
	}
	if win == 0 {
		return nil, render.Failed("opened the message but could not find its window")
	}

	dpi := windowDPI(win)
	scale := float64(dpi) / 96
	winRect := resizeOnScreen(win, int(float64(opt.Width)*scale), int(float64(opt.Height)*scale))

	if !sleepCtx(ctx, time.Duration(opt.WaitMs)*time.Millisecond) {
		return nil, render.TimedOut("cancelled")
	}

	body := findDescendant(win, classBody)
	crop := opt.Crop
	if crop == "body" && body == 0 {
		crop = "window"
	}

	var shots []*image.RGBA
	captureMethod, scrollMethod := "", ""
	for page := 1; page <= opt.Pages; page++ {
		full, method, err := captureWindow(win, render.LooksBlank)
		if err != nil {
			return nil, render.Failed("capture failed: %v", err)
		}
		captureMethod = method
		shot := full
		if crop == "body" {
			wr, br := windowRect(win), windowRect(body)
			area := br.toImage().Sub(image.Pt(int(wr.Left), int(wr.Top)))
			shot = render.Crop(full, area)
		}
		if len(shots) > 0 && render.SamePixels(shot, shots[len(shots)-1]) {
			break // scrolling reached the end
		}
		shots = append(shots, shot)
		if page == opt.Pages || ctx.Err() != nil {
			break
		}
		scrollMethod = scroll(inspector, body, win)
		time.Sleep(400 * time.Millisecond)
	}

	final := render.Stack(shots)
	png, err := render.EncodePNG(final)
	if err != nil {
		return nil, render.Failed("PNG encoding failed: %v", err)
	}
	info := map[string]string{
		"outlook_version": version,
		"open_method":     openMethod,
		"capture_method":  captureMethod,
		"crop":            crop,
		"dpi":             fmt.Sprint(dpi),
		"scale":           fmt.Sprintf("%.2f", scale),
		"pages":           fmt.Sprint(len(shots)),
		"image_width":     fmt.Sprint(final.Bounds().Dx()),
		"image_height":    fmt.Sprint(final.Bounds().Dy()),
		"window_width":    fmt.Sprint(winRect.Right - winRect.Left),
		"window_height":   fmt.Sprint(winRect.Bottom - winRect.Top),
	}
	if scrollMethod != "" {
		info["scroll_method"] = scrollMethod
	}
	return &render.Result{PNG: png, Info: info}, nil
}

// open displays the message. First choice: Session.OpenSharedItem, which
// reads the file with Outlook's normal (Word-based) reader without importing
// it. Fallback: outlook.exe /eml <file>, then pick up the new window.
func (r *Renderer) open(app *ole.IDispatch, path string) (item, inspector *ole.IDispatch, method string, err error) {
	inspectors, err := getObject(app, "Inspectors")
	if err != nil {
		return nil, nil, "", err
	}
	defer release(inspectors)
	before, _ := getInt(inspectors, "Count")

	openErr := func() error {
		session, err := getObject(app, "Session")
		if err != nil {
			return err
		}
		defer release(session)
		item, err = callObject(session, "OpenSharedItem", path)
		if err != nil {
			return err
		}
		if err := call(item, "Display", false); err != nil {
			return err
		}
		inspector, err = getObject(item, "GetInspector")
		return err
	}()
	if openErr == nil {
		return item, inspector, "OpenSharedItem", nil
	}
	release(inspector, item)
	item, inspector = nil, nil
	r.log.Warn("OpenSharedItem failed, trying outlook.exe /eml", "err", openErr)

	exe := outlookExe()
	if exe == "" {
		return nil, nil, "", fmt.Errorf("OpenSharedItem failed (%v) and OUTLOOK.EXE was not found for the /eml fallback", openErr)
	}
	if err := exec.Command(exe, "/eml", path).Start(); err != nil {
		return nil, nil, "", fmt.Errorf("OpenSharedItem failed (%v); starting outlook.exe /eml failed: %v", openErr, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if n, _ := getInt(inspectors, "Count"); n > before {
			inspector, err = callObject(inspectors, "Item", n)
			if err != nil {
				return nil, nil, "", err
			}
			item, _ = getObject(inspector, "CurrentItem")
			return item, inspector, "outlook /eml", nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, nil, "", fmt.Errorf("could not open the message. OpenSharedItem: %v; outlook /eml: no window appeared", openErr)
}

// scroll moves down one screen: Word's object model first, then a plain
// scroll message to the body window.
func scroll(inspector *ole.IDispatch, body, win hwnd) string {
	err := func() error {
		doc, err := getObject(inspector, "WordEditor")
		if err != nil {
			return err
		}
		defer release(doc)
		wins, err := getObject(doc, "Windows")
		if err != nil {
			return err
		}
		defer release(wins)
		w, err := callObject(wins, "Item", 1)
		if err != nil {
			return err
		}
		defer release(w)
		return call(w, "LargeScroll", 1)
	}()
	if err == nil {
		return "word"
	}
	target := body
	if target == 0 {
		target = win
	}
	scrollPageDown(target)
	return "wm_vscroll"
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func outlookExe() string {
	for _, key := range []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\OUTLOOK.EXE`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths\OUTLOOK.EXE`,
	} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		p, _, err := k.GetStringValue("")
		k.Close()
		if err == nil && p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func outlookRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), "OUTLOOK.EXE") {
			return true
		}
	}
	return false
}

func sessionID() uint32 {
	var sid uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sid); err != nil {
		return 0
	}
	return sid
}

// removeLater deletes a temp file. Outlook may still hold it open for a
// moment after the window closes, so retry briefly in the background; the
// startup sweep catches anything left.
func (r *Renderer) removeLater(path string) {
	go func() {
		for i := 0; i < 20; i++ {
			if err := os.Remove(path); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func (r *Renderer) sweepTemp() {
	entries, _ := os.ReadDir(r.tempDir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > time.Hour {
			os.Remove(filepath.Join(r.tempDir, e.Name()))
		}
	}
}
