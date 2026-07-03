//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

func desktopPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "dropzone.desktop")
}

// Enable writes a freedesktop autostart .desktop entry.
func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p := desktopPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=dropzone
Exec=%s
X-GNOME-Autostart-enabled=true
`, exe)
	return os.WriteFile(p, []byte(content), 0o644)
}

// Disable removes the autostart entry (no-op if absent).
func Disable() error {
	err := os.Remove(desktopPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// IsEnabled reports whether the autostart entry exists.
func IsEnabled() bool {
	_, err := os.Stat(desktopPath())
	return err == nil
}
