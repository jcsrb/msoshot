//go:build windows

package outlook

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// Every Outlook object is an *ole.IDispatch that we own exactly one
// reference to. Objects returned by get/call must be released by the caller.
// (go-ole's VARIANT.ToIDispatch does not AddRef, so releasing the IDispatch
// releases the reference the VARIANT held.)

const (
	rpcCallRejected     = 0x80010001
	rpcServerRetryLater = 0x8001010A
)

// retry repeats a COM call while Outlook says it is busy.
func retry(f func() (*ole.VARIANT, error)) (*ole.VARIANT, error) {
	for i := 0; ; i++ {
		v, err := f()
		if err == nil || i >= 20 || !isBusy(err) {
			return v, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func isBusy(err error) bool {
	var oe *ole.OleError
	if errors.As(err, &oe) {
		code := oe.Code()
		return code == rpcCallRejected || code == rpcServerRetryLater
	}
	return false
}

func get(d *ole.IDispatch, name string, args ...any) (*ole.VARIANT, error) {
	v, err := retry(func() (*ole.VARIANT, error) { return oleutil.GetProperty(d, name, args...) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

func call(d *ole.IDispatch, name string, args ...any) error {
	v, err := retry(func() (*ole.VARIANT, error) { return oleutil.CallMethod(d, name, args...) })
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	_ = v.Clear()
	return nil
}

func toObject(name string, v *ole.VARIANT) (*ole.IDispatch, error) {
	d := v.ToIDispatch()
	if d == nil {
		_ = v.Clear()
		return nil, fmt.Errorf("%s did not return an object", name)
	}
	return d, nil
}

func getObject(d *ole.IDispatch, name string, args ...any) (*ole.IDispatch, error) {
	v, err := get(d, name, args...)
	if err != nil {
		return nil, err
	}
	return toObject(name, v)
}

func callObject(d *ole.IDispatch, name string, args ...any) (*ole.IDispatch, error) {
	v, err := retry(func() (*ole.VARIANT, error) { return oleutil.CallMethod(d, name, args...) })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return toObject(name, v)
}

func getString(d *ole.IDispatch, name string) (string, error) {
	v, err := get(d, name)
	if err != nil {
		return "", err
	}
	defer v.Clear()
	if s, ok := v.Value().(string); ok {
		return s, nil
	}
	return fmt.Sprint(v.Value()), nil
}

func getInt(d *ole.IDispatch, name string) (int, error) {
	v, err := get(d, name)
	if err != nil {
		return 0, err
	}
	defer v.Clear()
	switch n := v.Value().(type) {
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case int16:
		return int(n), nil
	case uint32:
		return int(n), nil
	case int:
		return n, nil
	}
	return 0, fmt.Errorf("%s is not a number: %v", name, v.Value())
}

func release(objs ...*ole.IDispatch) {
	for _, o := range objs {
		if o != nil {
			o.Release()
		}
	}
}

// classicInstalled reports whether the Outlook.Application COM class exists.
// The new Outlook for Windows doesn't register it.
func classicInstalled() bool {
	_, err := ole.CLSIDFromProgID("Outlook.Application")
	return err == nil
}

// attach returns the running Outlook. GetActiveObject can fail on a healthy
// machine, so it falls back to creating the object, which attaches to the
// running instance because Outlook is single-instance. With mayStart false
// it never launches Outlook.
func attach(mayStart, running bool) (*ole.IDispatch, error) {
	if unk, err := oleutil.GetActiveObject("Outlook.Application"); err == nil {
		defer unk.Release()
		return unk.QueryInterface(ole.IID_IDispatch)
	}
	if !mayStart && !running {
		return nil, errors.New("Outlook is not running")
	}
	unk, err := oleutil.CreateObject("Outlook.Application")
	if err != nil {
		return nil, err
	}
	defer unk.Release()
	return unk.QueryInterface(ole.IID_IDispatch)
}

var iidOleWindow = ole.NewGUID("{00000114-0000-0000-C000-000000000046}")

// hwndOf asks a COM object (an Outlook Inspector) for its window through
// IOleWindow::GetWindow, the 4th entry in its vtable.
func hwndOf(d *ole.IDispatch) hwnd {
	ow, err := d.QueryInterface(iidOleWindow)
	if err != nil || ow == nil {
		return 0
	}
	defer ow.Release()
	vtbl := *(**[5]uintptr)(unsafe.Pointer(ow))
	var h hwnd
	hr, _, _ := syscall.SyscallN(vtbl[3], uintptr(unsafe.Pointer(ow)), uintptr(unsafe.Pointer(&h)))
	if hr != 0 {
		return 0
	}
	return h
}
