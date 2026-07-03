// Package config loads and persists dropzone settings and resolves per-OS defaults.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	// AppName is the config-dir / keyring namespace.
	AppName = "dropzone"

	keyringService = "dropzone"
	keyringUser    = "r2-secret-access-key"
)

// R2Config holds the Cloudflare R2 connection details. The secret is normally
// stored in the OS keychain rather than this struct (SecretAccessKey stays
// empty on disk); it is only persisted here as a fallback when the keychain
// is unavailable.
type R2Config struct {
	AccountID       string `json:"accountId"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	Bucket          string `json:"bucket"`
	PublicBaseURL   string `json:"publicBaseUrl"`
}

// ImageConfig controls screenshot compression.
type ImageConfig struct {
	Format              string `json:"format"`              // "webp-lossless" (default) | "jpeg"
	JPEGQualityFallback int    `json:"jpegQualityFallback"` // used on webp failure or format=jpeg
}

// VideoConfig controls recording compression.
type VideoConfig struct {
	Codec      string `json:"codec"`      // "h265" (default) | "h264" | "av1"
	CRF        int    `json:"crf"`        // 0 = codec default (h265=28, h264=23, av1=30)
	Preset     string `json:"preset"`     // "medium" (default); ignored for av1
	FFmpegPath string `json:"ffmpegPath"` // optional override; else looked up on PATH
}

// Config is the full on-disk configuration.
type Config struct {
	R2                  R2Config    `json:"r2"`
	WatchFolders        []string    `json:"watchFolders"` // explicit; empty => auto-detect
	WatchClipboard      bool        `json:"watchClipboard"`
	CopyLinkToClipboard bool        `json:"copyLinkToClipboard"`
	KeyPrefix           string      `json:"keyPrefix"` // optional root prefix for object keys
	Image               ImageConfig `json:"image"`
	Video               VideoConfig `json:"video"`

	path string // where this config was loaded from (not serialized)
}

// Dir returns the dropzone config directory (created lazily on Save).
func Dir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, AppName)
}

// Path returns the config.json path.
func Path() string { return filepath.Join(Dir(), "config.json") }

// Load reads config.json, applying defaults for any unset fields. A missing
// file yields a default (unconfigured) Config with no error.
func Load() (*Config, error) {
	c := &Config{path: Path()}
	data, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.ApplyDefaults()
			return c, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	c.path = Path()
	c.ApplyDefaults()
	return c, nil
}

// ApplyDefaults fills zero-valued fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.Image.Format == "" {
		c.Image.Format = "webp-lossless"
	}
	if c.Image.JPEGQualityFallback == 0 {
		c.Image.JPEGQualityFallback = 80
	}
	if c.Video.Codec == "" {
		c.Video.Codec = "h265"
	}
	if c.Video.Preset == "" {
		c.Video.Preset = "medium"
	}
	// Booleans default to true for a fresh (all-zero) config. We treat a
	// brand-new config (no folders, unconfigured) as "opt in to everything".
	if !c.Configured() {
		c.WatchClipboard = true
		c.CopyLinkToClipboard = true
	}
}

// Save writes the config as 0600 JSON, creating the directory if needed.
func (c *Config) Save() error {
	if c.path == "" {
		c.path = Path()
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0o600)
}

// Configured reports whether the essential R2 fields are present.
func (c *Config) Configured() bool {
	return c.R2.AccountID != "" && c.R2.AccessKeyID != "" && c.R2.Bucket != "" && c.HasSecret()
}

// PathUsed returns the file this config was loaded from / will save to.
func (c *Config) PathUsed() string { return c.path }

// HasSecret reports whether a secret is available (config or keychain).
func (c *Config) HasSecret() bool {
	if c.R2.SecretAccessKey != "" {
		return true
	}
	s, err := keyring.Get(keyringService, keyringUser)
	return err == nil && s != ""
}

// Secret returns the R2 secret access key from config or the OS keychain.
func (c *Config) Secret() (string, error) {
	if c.R2.SecretAccessKey != "" {
		return c.R2.SecretAccessKey, nil
	}
	return keyring.Get(keyringService, keyringUser)
}

// SetSecret stores the secret in the OS keychain. If the keychain is
// unavailable it falls back to the (0600) config file on the next Save.
func (c *Config) SetSecret(secret string) error {
	if err := keyring.Set(keyringService, keyringUser, secret); err != nil {
		c.R2.SecretAccessKey = secret
		return err
	}
	c.R2.SecretAccessKey = "" // prefer keychain
	return nil
}

// ResolvedWatchFolders returns the explicit WatchFolders, or the per-OS
// defaults, keeping only directories that currently exist.
func (c *Config) ResolvedWatchFolders() []string {
	candidates := c.WatchFolders
	if len(candidates) == 0 {
		candidates = DefaultWatchFolders()
	}
	var out []string
	seen := map[string]bool{}
	for _, d := range candidates {
		d = expandHome(d)
		if seen[d] {
			continue
		}
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			out = append(out, d)
			seen[d] = true
		}
	}
	return out
}

// DefaultWatchFolders returns the OS-standard capture folders.
func DefaultWatchFolders() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return []string{
			filepath.Join(home, "Pictures", "Screenshots"),
			filepath.Join(home, "Videos", "Captures"),
		}
	case "darwin":
		loc := macScreenshotLocation()
		if loc == "" {
			loc = filepath.Join(home, "Desktop")
		}
		return []string{loc}
	default: // linux and others
		pics := os.Getenv("XDG_PICTURES_DIR")
		if pics == "" {
			pics = filepath.Join(home, "Pictures")
		}
		return []string{filepath.Join(pics, "Screenshots"), pics}
	}
}

// macScreenshotLocation reads the user's configured screenshot folder.
func macScreenshotLocation() string {
	out, err := exec.Command("defaults", "read", "com.apple.screencapture", "location").Output()
	if err != nil {
		return ""
	}
	return expandHome(strings.TrimSpace(string(out)))
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}

// OpenPath opens a file or folder in the OS file manager / default handler.
func OpenPath(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
