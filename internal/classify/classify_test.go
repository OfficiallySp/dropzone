package classify

import "testing"

func TestByExt(t *testing.T) {
	cases := map[string]Kind{
		"C:/Users/x/Pictures/Screenshots/Screenshot 2026-07-03.png": Image,
		"shot.JPG":           Image,
		"clip.mp4":           Video,
		"record.MOV":         Video,
		"notes.txt":          Unknown,
		"archive.zip":        Unknown,
		"/home/x/cap.webp":   Image,
		"/home/x/screen.mkv": Video,
	}
	for path, want := range cases {
		if got := ByExt(path); got != want {
			t.Errorf("ByExt(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestIsTempName(t *testing.T) {
	temp := []string{"foo.tmp", "shot.png.part", "video.crdownload", "~snip.png", ".hidden.png"}
	for _, p := range temp {
		if !IsTempName(p) {
			t.Errorf("IsTempName(%q) = false, want true", p)
		}
	}
	final := []string{"shot.png", "clip.mp4", "Screenshot 2026-07-03 at 1.png"}
	for _, p := range final {
		if IsTempName(p) {
			t.Errorf("IsTempName(%q) = true, want false", p)
		}
	}
}
