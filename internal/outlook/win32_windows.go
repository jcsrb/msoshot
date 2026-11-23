//go:build windows

package outlook

import (
	"fmt"
	"image"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procPrintWindow                   = user32.NewProc("PrintWindow")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procGetClassNameW                 = user32.NewProc("GetClassNameW")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procFindWindowExW                 = user32.NewProc("FindWindowExW")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procGdiFlush           = gdi32.NewProc("GdiFlush")
)

const (
	swRestore           = 9
	swpNoZOrder         = 0x0004
	swpNoActivate       = 0x0010
	monitorDefaultNear  = 2
	pwRenderFullContent = 2
	wmVScroll           = 0x0115
	sbPageDown          = 3
	srcCopy             = 0x00CC0020
	captureBlt          = 0x40000000
	dibRGBColors        = 0
	biRGB               = 0
)

// Outlook's top-level window class for message windows, and the class of the
// Word-based message body inside it.
const (
	classInspector = "rctrl_renwnd32"
	classBody      = "_WwG"
)

type hwnd = uintptr

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) toImage() image.Rectangle {
	return image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// MakeDPIAware asks for per-monitor-v2 DPI awareness. It must run before any
// window is touched, or window sizes and screenshots are virtualised (scaled
// and blurry) on displays above 100%. Harmless if awareness is already set.
func MakeDPIAware() {
	if procSetProcessDpiAwarenessContext.Find() == nil {
		const perMonitorV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
		procSetProcessDpiAwarenessContext.Call(perMonitorV2)
	}
}

func systemDPI() int {
	if procGetDpiForSystem.Find() != nil {
		return 96
	}
	d, _, _ := procGetDpiForSystem.Call()
	if d == 0 {
		return 96
	}
	return int(d)
}

func windowDPI(h hwnd) int {
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(h); d != 0 {
			return int(d)
		}
	}
	return systemDPI()
}

func windowRect(h hwnd) rect {
	var r rect
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}

func isVisible(h hwnd) bool {
	v, _, _ := procIsWindowVisible.Call(h)
	return v != 0
}

func className(h hwnd) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func windowText(h hwnd) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func foregroundWindow() hwnd {
	h, _, _ := procGetForegroundWindow.Call()
	return h
}

func setForeground(h hwnd) {
	procSetForegroundWindow.Call(h)
}

func findWindowEx(parent, after hwnd, class *uint16) hwnd {
	h, _, _ := procFindWindowExW.Call(parent, after, uintptr(unsafe.Pointer(class)), 0)
	return h
}

// findDescendant returns the largest visible descendant of parent with the
// given class, searching depth-first.
func findDescendant(parent hwnd, class string) hwnd {
	var best hwnd
	var bestArea int64
	var walk func(p hwnd, depth int)
	walk = func(p hwnd, depth int) {
		if depth > 12 {
			return
		}
		for child := findWindowEx(p, 0, nil); child != 0; child = findWindowEx(p, child, nil) {
			if isVisible(child) && className(child) == class {
				r := windowRect(child)
				if area := int64(r.Right-r.Left) * int64(r.Bottom-r.Top); area > bestArea {
					best, bestArea = child, area
				}
			}
			walk(child, depth+1)
		}
	}
	walk(parent, 0)
	return best
}

// findTopLevel returns a visible top-level window of the class whose title contains text.
func findTopLevel(class, text string) hwnd {
	cls, _ := windows.UTF16PtrFromString(class)
	needle := strings.ToLower(text)
	for h := findWindowEx(0, 0, cls); h != 0; h = findWindowEx(0, h, cls) {
		if isVisible(h) && strings.Contains(strings.ToLower(windowText(h)), needle) {
			return h
		}
	}
	return 0
}

// resizeOnScreen un-maximises the window, moves it to the top-left of its
// monitor's work area and sizes it, clamped to that area so the whole window
// is on screen (needed for the screen-copy fallback). Returns the real rect.
func resizeOnScreen(h hwnd, width, height int) rect {
	procShowWindow.Call(h, swRestore)
	x, y := 0, 0
	if mon, _, _ := procMonitorFromWindow.Call(h, monitorDefaultNear); mon != 0 {
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
			x, y = int(mi.Work.Left), int(mi.Work.Top)
			width = min(width, int(mi.Work.Right-mi.Work.Left))
			height = min(height, int(mi.Work.Bottom-mi.Work.Top))
		}
	}
	procSetWindowPos.Call(h, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpNoZOrder|swpNoActivate)
	return windowRect(h)
}

func scrollPageDown(h hwnd) {
	procSendMessageW.Call(h, wmVScroll, sbPageDown, 0)
}

// gdiCapture creates a w×h top-down 32-bit bitmap, lets draw paint into it,
// and returns the pixels as an opaque RGBA image.
func gdiCapture(w, h int, draw func(memDC uintptr) bool) (*image.RGBA, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("window has no size (%dx%d)", w, h)
	}
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      -int32(h), // negative = top-down rows
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(memDC, uintptr(unsafe.Pointer(&bi)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil, fmt.Errorf("CreateDIBSection failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)
	defer procSelectObject.Call(memDC, old)

	if !draw(memDC) {
		return nil, fmt.Errorf("drawing into the bitmap failed")
	}
	procGdiFlush.Call()

	src := unsafe.Slice((*byte)(bits), w*h*4)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		// GDI gives BGRX with the X (alpha) byte left at zero: swap and force opaque.
		img.Pix[i*4] = src[i*4+2]
		img.Pix[i*4+1] = src[i*4+1]
		img.Pix[i*4+2] = src[i*4]
		img.Pix[i*4+3] = 255
	}
	return img, nil
}

// captureWindow grabs the whole window. PrintWindow works even when the
// window is partly covered; some GPU-drawn windows come back black, so then
// it brings the window to the front and copies the screen instead.
func captureWindow(h hwnd, looksBlank func(*image.RGBA) bool) (*image.RGBA, string, error) {
	r := windowRect(h)
	w, ht := int(r.Right-r.Left), int(r.Bottom-r.Top)
	img, err := gdiCapture(w, ht, func(memDC uintptr) bool {
		ok, _, _ := procPrintWindow.Call(h, memDC, pwRenderFullContent)
		return ok != 0
	})
	if err == nil && !looksBlank(img) {
		return img, "printwindow", nil
	}

	setForeground(h)
	time.Sleep(400 * time.Millisecond)
	r = windowRect(h)
	w, ht = int(r.Right-r.Left), int(r.Bottom-r.Top)
	img, err = gdiCapture(w, ht, func(memDC uintptr) bool {
		screenDC, _, _ := procGetDC.Call(0)
		defer procReleaseDC.Call(0, screenDC)
		ok, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(ht), screenDC,
			uintptr(r.Left), uintptr(r.Top), srcCopy|captureBlt)
		return ok != 0
	})
	if err != nil {
		return nil, "", err
	}
	return img, "screen", nil
}
