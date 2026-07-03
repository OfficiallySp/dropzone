// Package clipboardwatch monitors the system clipboard for new images (e.g.
// Win+Shift+S / snipping-tool captures that only land on the clipboard).
package clipboardwatch

import (
	"context"
	"sync"

	"golang.design/x/clipboard"
)

var (
	initOnce sync.Once
	initErr  error
)

// Init prepares the clipboard backend (idempotent — safe to call on every
// service restart). It returns an error on headless systems (no GUI session),
// in which case clipboard watching should be skipped.
func Init() error {
	initOnce.Do(func() { initErr = clipboard.Init() })
	return initErr
}

// Watch returns a channel of raw PNG bytes, one per new clipboard image.
// (clipboard v0.8.0 delivers a Data{Format, Bytes} value; we forward the image
// bytes.)
func Watch(ctx context.Context) <-chan []byte {
	in := clipboard.Watch(ctx, clipboard.FmtImage)
	out := make(chan []byte)
	go func() {
		defer close(out)
		for d := range in {
			if d.Format != clipboard.FmtImage || len(d.Bytes) == 0 {
				continue
			}
			select {
			case out <- d.Bytes:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// WriteText replaces the clipboard contents with text (used to place the
// shareable link on the clipboard after upload).
func WriteText(s string) {
	clipboard.Write(clipboard.FmtText, []byte(s))
}
