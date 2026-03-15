package converter

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestConvertTextToPDF(t *testing.T) {
	service := NewService(5 << 20)

	result, err := service.ConvertBytes(context.Background(), "note.md", "pdf", []byte("# Hello\nThis is markdown text."))
	if err != nil {
		t.Fatalf("convert text to pdf: %v", err)
	}

	if result.OutputMime != "application/pdf" {
		t.Fatalf("unexpected output mime: %q", result.OutputMime)
	}
	if result.OutputBase64 == "" {
		t.Fatalf("expected output base64")
	}

	raw, err := base64.StdEncoding.DecodeString(result.OutputBase64)
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Fatalf("expected pdf header")
	}
}

func TestConvertImageToWebP(t *testing.T) {
	service := NewService(5 << 20)
	source := makeTestPNG(t)

	result, err := service.ConvertBytes(context.Background(), "img.png", "webp", source)
	if err != nil {
		t.Fatalf("convert image to webp: %v", err)
	}
	if result.OutputMime != "image/webp" {
		t.Fatalf("unexpected output mime: %q", result.OutputMime)
	}
	if result.OutputBase64 == "" {
		t.Fatalf("expected output content")
	}
}

func TestUnsupportedConversion(t *testing.T) {
	service := NewService(5 << 20)
	_, err := service.ConvertBytes(context.Background(), "file.bin", "pdf", []byte("x"))
	if err == nil {
		t.Fatalf("expected unsupported conversion error")
	}
	if !strings.Contains(err.Error(), "unsupported conversion") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func makeTestPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(40 + x*10), B: 60, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
