package compress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ErrFFmpegNotFound is returned when no ffmpeg binary can be located.
var ErrFFmpegNotFound = errors.New("ffmpeg not found on PATH")

// VideoOptions configures the ffmpeg re-encode.
type VideoOptions struct {
	FFmpegPath string // explicit override; else PATH lookup
	Codec      string // "h265" (default) | "h264" | "av1"
	CRF        int    // 0 => codec default
	Preset     string // x264/x265 preset; ignored for av1
}

// FFmpegPath resolves the ffmpeg binary, honoring an explicit override.
func FFmpegPath(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
	}
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", ErrFFmpegNotFound
	}
	return p, nil
}

// HasFFmpeg reports whether ffmpeg is available.
func HasFFmpeg(override string) bool {
	_, err := FFmpegPath(override)
	return err == nil
}

// CompressVideo re-encodes in to a temporary .mp4 and returns its path. The
// caller is responsible for removing the returned file.
func CompressVideo(ctx context.Context, in string, opts VideoOptions) (string, error) {
	bin, err := FFmpegPath(opts.FFmpegPath)
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("dropzone-%d.mp4", time.Now().UnixNano()))

	cmd := exec.CommandContext(ctx, bin, ffmpegArgs(in, out, opts)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("ffmpeg: %w: %s", err, tail(stderr.String(), 500))
	}
	return out, nil
}

func ffmpegArgs(in, out string, opts VideoOptions) []string {
	preset := opts.Preset
	if preset == "" {
		preset = "medium"
	}
	crf := opts.CRF

	var vargs []string
	switch strings.ToLower(opts.Codec) {
	case "h264", "libx264", "avc":
		if crf == 0 {
			crf = 23
		}
		vargs = []string{"-c:v", "libx264", "-crf", strconv.Itoa(crf), "-preset", preset, "-pix_fmt", "yuv420p"}
	case "av1", "libsvtav1", "svtav1":
		if crf == 0 {
			crf = 30
		}
		avPreset := preset
		if _, err := strconv.Atoi(avPreset); err != nil {
			avPreset = "6" // libsvtav1 uses numeric presets 0..13
		}
		vargs = []string{"-c:v", "libsvtav1", "-crf", strconv.Itoa(crf), "-preset", avPreset}
	default: // h265 / hevc
		if crf == 0 {
			crf = 28
		}
		vargs = []string{"-c:v", "libx265", "-crf", strconv.Itoa(crf), "-preset", preset, "-tag:v", "hvc1"}
	}

	args := []string{"-y", "-i", in}
	args = append(args, vargs...)
	args = append(args, "-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", out)
	return args
}

// tail returns the last n bytes of s (for compact error messages).
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
