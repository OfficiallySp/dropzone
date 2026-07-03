// Package watcher watches capture folders for new image/video files and emits
// them once they are fully written ("settled").
package watcher

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"dropzone/internal/classify"
)

// Event is a settled capture file ready for processing.
type Event struct {
	Path string
	Kind classify.Kind
}

// Watcher wraps fsnotify with debouncing, a "file settled" gate and
// user-space recursion (fsnotify is not recursive).
type Watcher struct {
	fsw *fsnotify.Watcher
	out chan Event

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// New creates a Watcher over the given directories (non-existent ones are skipped).
func New(dirs []string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:    fsw,
		out:    make(chan Event, 64),
		timers: make(map[string]*time.Timer),
	}
	for _, d := range dirs {
		if err := w.addTree(d); err != nil {
			log.Printf("watch %s: %v", d, err)
		}
	}
	return w, nil
}

// addTree adds a watch on dir and its existing subdirectories.
func (w *Watcher) addTree(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if aerr := w.fsw.Add(path); aerr != nil {
				log.Printf("watch %s: %v", path, aerr)
			}
		}
		return nil
	})
}

// Events returns the channel of settled files.
func (w *Watcher) Events() <-chan Event { return w.out }

// Run processes fsnotify events until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	defer w.fsw.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handle(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("watch error: %v", err)
		}
	}
}

func (w *Watcher) handle(ev fsnotify.Event) {
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
		return
	}
	path := ev.Name

	// A newly created directory: start watching it (recursion), then re-walk to
	// catch files that appeared before the watch was registered.
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		_ = w.addTree(path)
		return
	}

	if classify.IsTempName(path) {
		return
	}
	kind := classify.ByExt(path)
	if kind == classify.Unknown {
		return
	}
	w.debounce(path, kind)
}

// debounce coalesces bursts of events for a path and defers the settle check.
func (w *Watcher) debounce(path string, kind classify.Kind) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[path]; ok {
		t.Stop()
	}
	w.timers[path] = time.AfterFunc(600*time.Millisecond, func() {
		w.mu.Lock()
		delete(w.timers, path)
		w.mu.Unlock()
		if settled(path, kind) {
			w.out <- Event{Path: path, Kind: kind}
		}
	})
}

// settled blocks until the file's size and mtime are stable across two checks
// and it can be opened for reading (handles Windows sharing locks and long
// recordings that grow throughout capture).
func settled(path string, kind classify.Kind) bool {
	const interval = 400 * time.Millisecond
	maxWait := 30 * time.Second
	if kind == classify.Video {
		maxWait = 30 * time.Minute // recordings can be long
	}
	deadline := time.Now().Add(maxWait)

	var lastSize int64 = -1
	var lastMod time.Time
	stable := 0

	for time.Now().Before(deadline) {
		fi, err := os.Stat(path)
		if err != nil {
			return false // deleted/moved away
		}
		if fi.Size() == lastSize && fi.ModTime().Equal(lastMod) {
			stable++
			if stable >= 2 && canOpen(path) {
				return true
			}
		} else {
			stable = 0
			lastSize = fi.Size()
			lastMod = fi.ModTime()
		}
		time.Sleep(interval)
	}
	return canOpen(path)
}

func canOpen(path string) bool {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
