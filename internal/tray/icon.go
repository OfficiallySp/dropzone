package tray

import (
	_ "embed"
	"encoding/binary"
	"runtime"
)

//go:embed icon.png
var iconPNG []byte

// Icon returns tray icon bytes in the format the current OS expects: a
// PNG-payload ICO on Windows, raw PNG elsewhere.
func Icon() []byte {
	if runtime.GOOS == "windows" {
		return icoFromPNG(iconPNG)
	}
	return iconPNG
}

// icoFromPNG wraps PNG bytes in a single-image .ico container. Windows (Vista+)
// renders PNG-compressed icon entries directly, so no re-encode is needed.
func icoFromPNG(png []byte) []byte {
	const headerLen = 6
	const entryLen = 16

	w, h := pngSize(png)
	buf := make([]byte, 0, headerLen+entryLen+len(png))

	// ICONDIR
	hdr := make([]byte, headerLen)
	binary.LittleEndian.PutUint16(hdr[0:], 0) // reserved
	binary.LittleEndian.PutUint16(hdr[2:], 1) // type: icon
	binary.LittleEndian.PutUint16(hdr[4:], 1) // count
	buf = append(buf, hdr...)

	// ICONDIRENTRY
	entry := make([]byte, entryLen)
	entry[0] = byteDim(w) // width  (0 => 256)
	entry[1] = byteDim(h) // height (0 => 256)
	entry[2] = 0          // palette
	entry[3] = 0          // reserved
	binary.LittleEndian.PutUint16(entry[4:], 1)                     // color planes
	binary.LittleEndian.PutUint16(entry[6:], 32)                    // bits per pixel
	binary.LittleEndian.PutUint32(entry[8:], uint32(len(png)))      // size of image data
	binary.LittleEndian.PutUint32(entry[12:], headerLen+entryLen)   // offset to image data
	buf = append(buf, entry...)

	return append(buf, png...)
}

func byteDim(v int) byte {
	if v >= 256 {
		return 0
	}
	return byte(v)
}

// pngSize reads width/height from the PNG IHDR chunk; returns 0,0 on failure.
func pngSize(png []byte) (int, int) {
	// 8-byte signature + 4-byte length + "IHDR" + width(4) + height(4)
	if len(png) < 24 {
		return 0, 0
	}
	w := binary.BigEndian.Uint32(png[16:20])
	h := binary.BigEndian.Uint32(png[20:24])
	return int(w), int(h)
}
