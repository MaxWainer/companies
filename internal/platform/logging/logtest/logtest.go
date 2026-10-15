package logtest

import (
	"context"
	"log/slog"
	"sync"
)

type Entry struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type Recorder struct {
	mu      sync.Mutex
	entries []Entry
}

func New() (*slog.Logger, *Recorder) {
	rec := &Recorder{}
	return slog.New(&handler{rec: rec}), rec
}

func (r *Recorder) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Entry(nil), r.entries...)
}

func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func (r *Recorder) Filter(message string) []Entry {
	var out []Entry
	for _, e := range r.Entries() {
		if e.Message == message {
			out = append(out, e)
		}
	}
	return out
}

func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}

func (r *Recorder) Messages() []string {
	entries := r.Entries()
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Message
	}
	return out
}

type handler struct {
	rec   *Recorder
	attrs []slog.Attr
}

func (*handler) Enabled(context.Context, slog.Level) bool { return true }

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any, len(h.attrs)+r.NumAttrs())
	for _, a := range h.attrs {
		attrs[a.Key] = a.Value.Resolve().Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Resolve().Any()
		return true
	})
	h.rec.mu.Lock()
	defer h.rec.mu.Unlock()
	h.rec.entries = append(h.rec.entries, Entry{Level: r.Level, Message: r.Message, Attrs: attrs})
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{rec: h.rec, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *handler) WithGroup(string) slog.Handler { return h }
