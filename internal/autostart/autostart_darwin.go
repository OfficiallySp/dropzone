//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

const label = "com.dropzone.agent"

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// Enable writes a per-user LaunchAgent that runs at login.
func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, label, exe)
	return os.WriteFile(p, []byte(content), 0o644)
}

// Disable removes the LaunchAgent (no-op if absent).
func Disable() error {
	err := os.Remove(plistPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// IsEnabled reports whether the LaunchAgent exists.
func IsEnabled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}
