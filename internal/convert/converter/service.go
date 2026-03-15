package converter

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chai2010/webp"
	"github.com/jung-kurt/gofpdf"
	_ "image/jpeg"
	_ "image/png"
)

type Service struct {
	maxFileSize int64
}

func NewService(maxFileSize int64) *Service {
	if maxFileSize <= 0 {
		maxFileSize = 25 << 20
	}
	return &Service{maxFileSize: maxFileSize}
}

func (s *Service) MaxFileSize() int64 {
	return s.maxFileSize
}

func (s *Service) ConvertBytes(ctx context.Context, filename, targetFormat string, data []byte) (ConversionResult, error) {
	if len(data) == 0 {
		return ConversionResult{}, errors.New("file is empty")
	}
	if int64(len(data)) > s.maxFileSize {
		return ConversionResult{}, fmt.Errorf("file exceeds max size of %d bytes", s.maxFileSize)
	}

	sourceFormat := normalizeFormat(filepath.Ext(filename))
	targetFormat = normalizeFormat(targetFormat)
	if targetFormat == "" {
		return ConversionResult{}, errors.New("target format is required")
	}

	result, err := s.convert(ctx, sourceFormat, targetFormat, filename, data)
	if err != nil {
		return ConversionResult{}, err
	}
	result.Filename = filename
	result.SourceFormat = sourceFormat
	result.TargetFormat = targetFormat
	result.SizeBytes = len(result.OutputBase64) * 3 / 4
	return result, nil
}

func (s *Service) ConvertBase64Job(ctx context.Context, job JobInput) ConversionResult {
	result := ConversionResult{
		Filename: job.Filename,
	}

	data, err := base64.StdEncoding.DecodeString(job.FileBase64)
	if err != nil {
		result.Error = "invalid file_base64"
		return result
	}

	converted, err := s.ConvertBytes(ctx, job.Filename, job.TargetFormat, data)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	return converted
}

func (s *Service) convert(ctx context.Context, sourceFormat, targetFormat, filename string, data []byte) (ConversionResult, error) {
	switch {
	case targetFormat == "webp" && isImageFormat(sourceFormat):
		return convertImageToWebP(filename, data)
	case targetFormat == "pdf" && (sourceFormat == "md" || sourceFormat == "markdown" || sourceFormat == "txt"):
		return convertTextLikeToPDF(filename, data)
	case targetFormat == "jpg" && (sourceFormat == "heic" || sourceFormat == "heif"):
		return runCommandConversion(ctx, filename, data, "jpg")
	case targetFormat == "pdf" && sourceFormat == "docx":
		return runCommandConversion(ctx, filename, data, "pdf")
	case targetFormat == "docx" && sourceFormat == "pdf":
		return runCommandConversion(ctx, filename, data, "docx")
	default:
		return ConversionResult{}, fmt.Errorf("unsupported conversion: %s -> %s", sourceFormat, targetFormat)
	}
}

func convertImageToWebP(filename string, data []byte) (ConversionResult, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ConversionResult{}, errors.New("failed to decode image")
	}

	var out bytes.Buffer
	if err := webp.Encode(&out, img, &webp.Options{Lossless: true, Quality: 85}); err != nil {
		return ConversionResult{}, err
	}

	output := out.Bytes()
	return ConversionResult{
		OutputFilename: changeExt(filename, ".webp"),
		OutputMime:     "image/webp",
		OutputBase64:   base64.StdEncoding.EncodeToString(output),
		SizeBytes:      len(output),
	}, nil
}

func convertTextLikeToPDF(filename string, data []byte) (ConversionResult, error) {
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return ConversionResult{}, errors.New("text content is empty")
	}

	pdfDoc := gofpdf.New("P", "mm", "A4", "")
	pdfDoc.SetMargins(12, 12, 12)
	pdfDoc.SetAutoPageBreak(true, 12)
	pdfDoc.AddPage()
	pdfDoc.SetFont("Arial", "", 11)

	for _, line := range strings.Split(text, "\n") {
		pdfDoc.MultiCell(0, 5.2, line, "", "L", false)
	}

	var out bytes.Buffer
	if err := pdfDoc.Output(&out); err != nil {
		return ConversionResult{}, err
	}

	output := out.Bytes()
	return ConversionResult{
		OutputFilename: changeExt(filename, ".pdf"),
		OutputMime:     "application/pdf",
		OutputBase64:   base64.StdEncoding.EncodeToString(output),
		SizeBytes:      len(output),
	}, nil
}

func runCommandConversion(ctx context.Context, filename string, data []byte, targetFormat string) (ConversionResult, error) {
	tool, argsBuilder, err := findCommandForFormat(targetFormat)
	if err != nil {
		return ConversionResult{}, err
	}

	tmpDir, err := os.MkdirTemp("", "convert-*")
	if err != nil {
		return ConversionResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, sanitizeFilename(filename))
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return ConversionResult{}, err
	}

	convCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	args, outputPath := argsBuilder(inputPath, tmpDir, targetFormat)
	cmd := exec.CommandContext(convCtx, tool, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return ConversionResult{}, fmt.Errorf("conversion command failed: %s", msg)
	}

	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return ConversionResult{}, errors.New("conversion output file not found")
	}

	return ConversionResult{
		OutputFilename: filepath.Base(outputPath),
		OutputMime:     mimeFor(targetFormat),
		OutputBase64:   base64.StdEncoding.EncodeToString(outputBytes),
		SizeBytes:      len(outputBytes),
	}, nil
}

type commandArgsBuilder func(inputPath, outputDir, targetFormat string) (args []string, outputPath string)

func findCommandForFormat(targetFormat string) (string, commandArgsBuilder, error) {
	if targetFormat == "jpg" {
		if tool, err := exec.LookPath("magick"); err == nil {
			return tool, func(inputPath, outputDir, targetFormat string) ([]string, string) {
				outputPath := filepath.Join(outputDir, changeExt(filepath.Base(inputPath), ".jpg"))
				return []string{inputPath, outputPath}, outputPath
			}, nil
		}
		if tool, err := exec.LookPath("convert"); err == nil {
			return tool, func(inputPath, outputDir, targetFormat string) ([]string, string) {
				outputPath := filepath.Join(outputDir, changeExt(filepath.Base(inputPath), ".jpg"))
				return []string{inputPath, outputPath}, outputPath
			}, nil
		}
		return "", nil, errors.New("heic conversion requires imagemagick (magick/convert) installed")
	}

	if tool, err := exec.LookPath("libreoffice"); err == nil {
		return tool, func(inputPath, outputDir, targetFormat string) ([]string, string) {
			outputName := changeExt(filepath.Base(inputPath), "."+targetFormat)
			outputPath := filepath.Join(outputDir, outputName)
			args := []string{"--headless", "--convert-to", targetFormat, "--outdir", outputDir, inputPath}
			return args, outputPath
		}, nil
	}

	return "", nil, errors.New("docx/pdf conversion requires libreoffice installed")
}

func normalizeFormat(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, ".")
	switch value {
	case "jpeg":
		return "jpg"
	case "markdown":
		return "md"
	default:
		return value
	}
}

func isImageFormat(format string) bool {
	switch format {
	case "png", "jpg", "gif":
		return true
	default:
		return false
	}
}

func mimeFor(format string) string {
	switch format {
	case "pdf":
		return "application/pdf"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "jpg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		return "input.bin"
	}
	return name
}

func changeExt(filename, ext string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		base = "output"
	}
	return base + ext
}
