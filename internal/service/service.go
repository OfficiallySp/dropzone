// Package service owns the running capture→upload pipeline and its watchers,
// and can be (re)started when configuration changes from the GUI.
package service

import (
	"context"
	"errors"
	"sync"

	"dropzone/internal/clipboardwatch"
	"dropzone/internal/config"
	"dropzone/internal/pipeline"
	"dropzone/internal/r2"
	"dropzone/internal/watcher"
)

// Service manages the lifecycle of the upload pipeline.
type Service struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	pipe    *pipeline.Pipeline
	running bool
	folders []string
	clip    bool

	// Callbacks (set before Start). Invoked from worker goroutines.
	OnStatus func(string)
	OnUpload func(name, url string)
}

// New returns an idle Service.
func New() *Service { return &Service{} }

// Start builds the R2 client and launches the pipeline, folder watcher and
// (optionally) clipboard watcher. It is an error to Start a running Service.
func (s *Service) Start(cfg *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return errors.New("service already running")
	}
	secret, err := cfg.Secret()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	client, err := r2.New(ctx, cfg.R2.AccountID, cfg.R2.AccessKeyID, secret, cfg.R2.Bucket, cfg.R2.PublicBaseURL)
	if err != nil {
		cancel()
		return err
	}

	pipe := pipeline.New(cfg, client)
	pipe.OnStatus = s.OnStatus
	pipe.OnUpload = s.OnUpload
	pipe.Start(ctx, 3)

	folders := cfg.ResolvedWatchFolders()
	if w, werr := watcher.New(folders); werr == nil {
		go w.Run(ctx)
		go func() {
			for ev := range w.Events() {
				pipe.SubmitFile(ev.Path, ev.Kind)
			}
		}()
	}

	clip := false
	if cfg.WatchClipboard {
		if clipboardwatch.Init() == nil {
			clip = true
			ch := clipboardwatch.Watch(ctx)
			go func() {
				for d := range ch {
					pipe.SubmitImageBytes(d, "clipboard.png")
				}
			}()
		}
	}

	s.cancel, s.pipe, s.running, s.folders, s.clip = cancel, pipe, true, folders, clip
	return nil
}

// Stop cancels all watchers/workers (no-op if not running).
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.cancel()
	s.running = false
	s.pipe = nil
}

// Restart applies a new configuration.
func (s *Service) Restart(cfg *config.Config) error {
	s.Stop()
	return s.Start(cfg)
}

// SetPaused pauses/resumes processing.
func (s *Service) SetPaused(p bool) {
	s.mu.Lock()
	pipe := s.pipe
	s.mu.Unlock()
	if pipe != nil {
		pipe.SetPaused(p)
	}
}

// Running reports whether the pipeline is active.
func (s *Service) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Summary returns the currently watched folders and whether the clipboard is watched.
func (s *Service) Summary() (folders []string, clip bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.folders...), s.clip
}
