// Package compress handles screenshot (image) and recording (video) compression.
package compress

import (
	"bytes"
	"image"
	"image/jpeg" // also registers the JPEG decoder via its init()

	// Decoders registered for image.Decode. Screenshots are almost always PNG;
	// the rest cover common capture formats.
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"github.com/HugoSmits86/nativewebp"
)

// ImageResult is a compressed image ready to upload.
type ImageResult struct {
	Data        []byte
	Ext         string // ".webp" or ".jpg"
	ContentType string
}

// CompressImageBytes decodes raw image bytes and re-encodes them. By default it
// produces lossless WebP (ideal for text/UI screenshots); it falls back to JPEG
// if WebP encoding fails or forceJPEG is set.
func CompressImageBytes(raw []byte, jpegQuality int, forceJPEG bool) (*ImageResult, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return encodeImage(img, jpegQuality, forceJPEG)
}

func encodeImage(img image.Image, jpegQuality int, forceJPEG bool) (*ImageResult, error) {
	if !forceJPEG {
		var buf bytes.Buffer
		if err := nativewebp.Encode(&buf, img, nil); err == nil {
			return &ImageResult{Data: buf.Bytes(), Ext: ".webp", ContentType: "image/webp"}, nil
		}
		// fall through to JPEG on encode error
	}
	if jpegQuality <= 0 || jpegQuality > 100 {
		jpegQuality = 80
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return &ImageResult{Data: buf.Bytes(), Ext: ".jpg", ContentType: "image/jpeg"}, nil
}
