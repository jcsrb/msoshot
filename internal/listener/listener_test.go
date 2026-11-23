package listener

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, l *Listener, want string) {
	for i := 0; i < 100; i++ {
		if _, s := l.Status(); s == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, s := l.Status()
	t.Fatalf("state %q, want %q", s, want)
}

func TestStartMoveStop(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	l := New(h, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p1, p2 := freePort(t), freePort(t)

	l.Start("127.0.0.1", p1)
	waitFor(t, l, "listening")
	get := func(port int) error {
		resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
		if err == nil {
			resp.Body.Close()
		}
		return err
	}
	if err := get(p1); err != nil {
		t.Fatal(err)
	}

	l.Start("127.0.0.1", p2)
	waitFor(t, l, "listening")
	if err := get(p2); err != nil {
		t.Fatal(err)
	}
	if err := get(p1); err == nil {
		t.Fatal("old port still served")
	}
	l.Stop()
	if err := get(p2); err == nil {
		t.Fatal("still serving after Stop")
	}
}

func TestRetriesWhenAddressMissing(t *testing.T) {
	l := New(http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	l.Start("192.0.2.1", 8787) // TEST-NET-1: never assigned to this machine
	for i := 0; i < 100; i++ {
		if _, s := l.Status(); len(s) > 8 && s[:8] == "waiting:" {
			l.Stop()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected waiting state")
}
