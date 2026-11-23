// Package activity keeps the last few renders in memory for the dashboard.
// Nothing here is written to disk.
package activity

import (
	"sync"
	"time"
)

// Entry is one render request.
type Entry struct {
	ID         int               `json:"id"`
	Time       time.Time         `json:"time"`
	Client     string            `json:"client"`
	Subject    string            `json:"subject"`
	Status     int               `json:"status"`
	Error      string            `json:"error,omitempty"`
	DurationMs int64             `json:"duration_ms"`
	Info       map[string]string `json:"info,omitempty"`
	HasImage   bool              `json:"has_image"`
	png        []byte
}

// Log is a fixed-size, concurrency-safe ring of recent entries.
type Log struct {
	mu      sync.Mutex
	size    int
	next    int
	entries []Entry
}

func New(size int) *Log { return &Log{size: size, next: 1} }

// Add records an entry and returns its ID. png may be nil.
func (l *Log) Add(e Entry, png []byte) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.ID = l.next
	l.next++
	e.png = png
	e.HasImage = len(png) > 0
	l.entries = append(l.entries, e)
	if len(l.entries) > l.size {
		l.entries = l.entries[len(l.entries)-l.size:]
	}
	return e.ID
}

// Recent returns entries newest first, without image data.
func (l *Log) Recent() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, 0, len(l.entries))
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := l.entries[i]
		e.png = nil
		out = append(out, e)
	}
	return out
}

// PNG returns the image of an entry, if it is still kept.
func (l *Log) PNG(id int) ([]byte, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.ID == id && len(e.png) > 0 {
			return e.png, true
		}
	}
	return nil, false
}
