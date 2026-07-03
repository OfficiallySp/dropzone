// Package classify routes captured files into the image or video pipeline and
// filters out in-progress temp files.
package classify

import (
	"mime"
	"path/filepath"
	"strings"
)

// Kind is the category of a captured file.
type Kind int

const (
	Unknown Kind = iota
	Image
	Video
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".jfif": true, ".gif": true,
	".bmp": true, ".webp": true, ".heic": true, ".heif": true, ".tif": true,
	".tiff": true, ".avif": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".mov": true, ".mkv": true, ".webm": true, ".avi": true,
	".m4v": true, ".wmv": true, ".flv": true, ".mpg": true, ".mpeg": true,
}

// ByExt classifies a path by its (lowercased) extension.
func ByExt(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case imageExts[ext]:
		return Image
	case videoExts[ext]:
		return Video
	default:
		return Unknown
	}
}

// IsTempName reports whether the path looks like an in-progress / atomic-write
// temp file that should be ignored until it is renamed to its final name.
func IsTempName(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, "~") || strings.HasPrefix(base, ".") {
		return true
	}
	for _, suf := range []string{".tmp", ".part", ".partial", ".crdownload", ".download", ".~tmp"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return false
}

// ContentType returns a best-effort MIME type for the given path's extension,
// falling back to a generic type per kind.
func ContentType(path string, kind Kind) string {
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); ct != "" {
		return ct
	}
	switch kind {
	case Image:
		return "image/png"
	case Video:
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}
