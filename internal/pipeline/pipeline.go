// Package pipeline turns captured files/clipboard images into compressed R2
// uploads, deduping across sources and copying a share link on success.
package pipeline

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"dropzone/internal/classify"
	"dropzone/internal/clipboardwatch"
	"dropzone/internal/compress"
	"dropzone/internal/config"
	"dropzone/internal/dedupe"
	"dropzone/internal/r2"
)

// Pipeline processes capture jobs with a small worker pool.
type Pipeline struct {
	cfg    *config.Config
	client *r2.Client
	lru    *dedupe.LRU
	jobs   chan job
	paused atomic.Bool

	// OnStatus, if set, receives short human-readable status lines for the tray.
	OnStatus func(string)
	// OnUpload, if set, is called after each successful upload with the source
	// name and the public URL.
	OnUpload func(name, url string)
}

type job struct {
	kind classify.Kind
	// image jobs carry decoded bytes; video jobs carry a file path.
	data     []byte
	path     string
	origName string
}

// New creates a pipeline. Call Start to launch workers.
func New(cfg *config.Config, client *r2.Client) *Pipeline {
	return &Pipeline{
		cfg:    cfg,
		client: client,
		lru:    dedupe.New(512),
		jobs:   make(chan job, 128),
	}
}

// Start launches worker goroutines that run until ctx is cancelled.
func (p *Pipeline) Start(ctx context.Context, workers int) {
	if workers <= 0 {
		workers = 3
	}
	for i := 0; i < workers; i++ {
		go p.worker(ctx)
	}
}

// SetPaused stops (true) or resumes (false) accepting new work.
func (p *Pipeline) SetPaused(v bool) { p.paused.Store(v) }

// SubmitFile enqueues a settled capture file, deduping images by content.
func (p *Pipeline) SubmitFile(path string, kind classify.Kind) {
	if p.paused.Load() {
		return
	}
	switch kind {
	case classify.Image:
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("read %s: %v", path, err)
			return
		}
		if p.lru.SeenOrAdd(dedupe.HashBytes(data)) {
			return // already handled (e.g. also arrived via clipboard)
		}
		p.enqueue(job{kind: classify.Image, data: data, origName: filepath.Base(path)})
	case classify.Video:
		p.enqueue(job{kind: classify.Video, path: path, origName: filepath.Base(path)})
	}
}

// SubmitImageBytes enqueues a clipboard image, deduping by content.
func (p *Pipeline) SubmitImageBytes(data []byte, name string) {
	if p.paused.Load() || len(data) == 0 {
		return
	}
	if p.lru.SeenOrAdd(dedupe.HashBytes(data)) {
		return
	}
	p.enqueue(job{kind: classify.Image, data: data, origName: name})
}

func (p *Pipeline) enqueue(j job) {
	// Blocks if the buffer is full, applying backpressure rather than dropping.
	p.jobs <- j
}

func (p *Pipeline) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-p.jobs:
			switch j.kind {
			case classify.Image:
				p.processImage(ctx, j)
			case classify.Video:
				p.processVideo(ctx, j)
			}
		}
	}
}

func (p *Pipeline) processImage(ctx context.Context, j job) {
	p.status("compressing " + j.origName)
	forceJPEG := strings.EqualFold(p.cfg.Image.Format, "jpeg")
	res, err := compress.CompressImageBytes(j.data, p.cfg.Image.JPEGQualityFallback, forceJPEG)
	if err != nil {
		log.Printf("compress %s: %v", j.origName, err)
		p.status("error: " + j.origName)
		return
	}
	key := p.datedKey("images", res.Ext)
	p.status("uploading " + j.origName)
	if err := p.client.PutBytes(ctx, key, res.Data, res.ContentType); err != nil {
		log.Printf("upload %s: %v", key, err)
		p.status("upload failed: " + j.origName)
		return
	}
	log.Printf("uploaded image %s (%d -> %d bytes)", key, len(j.data), len(res.Data))
	p.afterUpload(j.origName, key)
}

// processVideo uploads the original first (fast availability), then compresses
// with ffmpeg and replaces the object. If the compressed container differs from
// the original, the original object is deleted so a single final .mp4 remains.
func (p *Pipeline) processVideo(ctx context.Context, j job) {
	base := p.datedKey("video", "") // no extension yet
	origExt := strings.ToLower(filepath.Ext(j.path))
	origKey := base + origExt

	p.status("uploading original " + j.origName)
	if err := p.client.PutFile(ctx, origKey, j.path, classify.ContentType(j.path, classify.Video)); err != nil {
		log.Printf("upload original %s: %v", origKey, err)
		p.status("upload failed: " + j.origName)
		return
	}
	log.Printf("uploaded original video %s", origKey)
	p.afterUpload(j.origName, origKey)

	// Compress + replace.
	p.status("compressing " + j.origName)
	out, err := compress.CompressVideo(ctx, j.path, compress.VideoOptions{
		FFmpegPath: p.cfg.Video.FFmpegPath,
		Codec:      p.cfg.Video.Codec,
		CRF:        p.cfg.Video.CRF,
		Preset:     p.cfg.Video.Preset,
	})
	if err != nil {
		if err == compress.ErrFFmpegNotFound {
			log.Printf("ffmpeg not found; keeping original %s", origKey)
			p.status("uploaded (no ffmpeg; original kept)")
		} else {
			log.Printf("compress %s: %v", j.origName, err)
			p.status("compress failed (original kept)")
		}
		return
	}
	defer os.Remove(out)

	finalKey := base + ".mp4"
	p.status("uploading compressed " + j.origName)
	if err := p.client.PutFile(ctx, finalKey, out, "video/mp4"); err != nil {
		log.Printf("upload compressed %s: %v", finalKey, err)
		p.status("compressed upload failed (original kept)")
		return
	}
	if finalKey != origKey {
		if err := p.client.Delete(ctx, origKey); err != nil {
			log.Printf("delete original %s: %v", origKey, err)
		}
	}
	if fi, serr := os.Stat(out); serr == nil {
		log.Printf("replaced with compressed video %s (%d bytes)", finalKey, fi.Size())
	}
	p.afterUpload(j.origName, finalKey)
}

// afterUpload copies the public link to the clipboard (if enabled), reports
// status, and notifies any OnUpload listener.
func (p *Pipeline) afterUpload(name, key string) {
	url := p.client.PublicURL(key)
	if p.cfg.CopyLinkToClipboard && p.cfg.R2.PublicBaseURL != "" {
		clipboardwatch.WriteText(url)
	}
	p.status("uploaded → " + url)
	if p.OnUpload != nil {
		p.OnUpload(name, url)
	}
}

// datedKey builds an object key like [prefix/]root/YYYY/MM/DD/<ulid><ext>.
func (p *Pipeline) datedKey(root, ext string) string {
	now := time.Now()
	var parts []string
	if pre := strings.Trim(p.cfg.KeyPrefix, "/"); pre != "" {
		parts = append(parts, pre)
	}
	parts = append(parts, root, now.Format("2006"), now.Format("01"), now.Format("02"), ulid.Make().String()+ext)
	return strings.Join(parts, "/")
}

func (p *Pipeline) status(s string) {
	if p.OnStatus != nil {
		if len(s) > 70 {
			s = s[:67] + "..."
		}
		p.OnStatus(s)
	}
}
