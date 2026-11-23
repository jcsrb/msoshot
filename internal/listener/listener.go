// Package listener runs the API's HTTP server on a chosen address and can
// move it to another address without restarting the app. If the address
// isn't available yet (for example a VPN still connecting at login), it keeps
// retrying.
package listener

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

type Listener struct {
	handler http.Handler
	log     *slog.Logger

	mu     sync.Mutex
	addr   string
	state  string
	cancel context.CancelFunc
	done   chan struct{}
}

func New(handler http.Handler, log *slog.Logger) *Listener {
	return &Listener{handler: handler, log: log, state: "stopped"}
}

// Status returns the current address and a human-readable state.
func (l *Listener) Status() (addr, state string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.addr, l.state
}

func (l *Listener) setState(addr, s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.addr == addr {
		l.state = s
	}
}

// Start (re)starts serving on host:port. It returns immediately; binding
// happens in the background and is retried every 5 seconds until it works.
func (l *Listener) Start(host string, port int) {
	l.Stop()
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	l.mu.Lock()
	l.addr, l.state, l.cancel, l.done = addr, "starting", cancel, done
	l.mu.Unlock()

	go func() {
		defer close(done)
		var ln net.Listener
		for {
			var err error
			ln, err = net.Listen("tcp4", addr)
			if err == nil {
				break
			}
			l.setState(addr, "waiting: "+err.Error())
			l.log.Warn("cannot listen yet, retrying", "addr", addr, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
		srv := &http.Server{
			Handler:           l.handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		go func() {
			<-ctx.Done()
			shutdownCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
			defer c()
			_ = srv.Shutdown(shutdownCtx)
		}()
		l.setState(addr, "listening")
		l.log.Info("listening", "addr", addr)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.setState(addr, "error: "+err.Error())
			l.log.Error("server stopped", "err", err)
		}
	}()
}

// Stop shuts the server down and waits for it.
func (l *Listener) Stop() {
	l.mu.Lock()
	cancel, done := l.cancel, l.done
	l.cancel, l.done = nil, nil
	l.state = "stopped"
	l.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
