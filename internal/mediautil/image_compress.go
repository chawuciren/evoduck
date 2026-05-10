package mediautil

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

type CompressionResult struct {
	Data       []byte
	Name       string
	MimeType   string
	Compressed bool
}

func CompressImageData(name, mimeType string, data []byte, maxBytes int) (CompressionResult, error) {
	result := CompressionResult{
		Data:     data,
		Name:     name,
		MimeType: strings.TrimSpace(mimeType),
	}
	if len(data) == 0 || maxBytes <= 0 || len(data) <= maxBytes {
		return result, nil
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return result, nil
	}
	if result.MimeType == "" {
		switch strings.ToLower(format) {
		case "jpeg", "jpg":
			result.MimeType = "image/jpeg"
		case "png":
			result.MimeType = "image/png"
		}
	}
	candidates := compressedImageCandidates(decoded, format)
	best := data
	bestMime := result.MimeType
	for _, candidate := range candidates {
		if len(candidate.data) == 0 {
			continue
		}
		if len(candidate.data) < len(best) {
			best = candidate.data
			bestMime = candidate.mimeType
		}
		if len(candidate.data) <= maxBytes {
			best = candidate.data
			bestMime = candidate.mimeType
			break
		}
	}
	if len(best) >= len(data) {
		return result, nil
	}
	result.Data = best
	result.MimeType = bestMime
	result.Compressed = true
	return result, nil
}

type compressedCandidate struct {
	data     []byte
	mimeType string
}

func compressedImageCandidates(img image.Image, format string) []compressedCandidate {
	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		qualities := []int{85, 70, 55, 40, 30}
		candidates := make([]compressedCandidate, 0, len(qualities))
		for _, quality := range qualities {
			buf := new(bytes.Buffer)
			if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err == nil {
				candidates = append(candidates, compressedCandidate{data: buf.Bytes(), mimeType: "image/jpeg"})
			}
		}
		return candidates
	case "png":
		buf := new(bytes.Buffer)
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		if err := encoder.Encode(buf, img); err == nil {
			return []compressedCandidate{{data: buf.Bytes(), mimeType: "image/png"}}
		}
	}
	return nil
}

func IsCompressibleImage(mimeType, name string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if mimeType == "image/jpeg" || mimeType == "image/jpg" || mimeType == "image/png" {
		return true
	}
	ext := strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(ext, ".jpg") || strings.HasSuffix(ext, ".jpeg") || strings.HasSuffix(ext, ".png")
}

func CompressImageFile(inputPath, outputPath, name, mimeType string, maxBytes int, overwrite bool) (*StoreResult, error) {
	if strings.TrimSpace(inputPath) == "" {
		return nil, fmt.Errorf("input_path is required")
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read input image %q: %w", inputPath, err)
	}
	if strings.TrimSpace(name) == "" {
		name = SanitizeName(filepath.Base(inputPath))
	}
	compressed, err := CompressImageData(name, mimeType, data, maxBytes)
	if err != nil {
		return nil, err
	}
	finalPath := strings.TrimSpace(outputPath)
	if finalPath == "" {
		if overwrite {
			finalPath = inputPath
		} else {
			finalPath = defaultCompressedOutputPath(inputPath)
		}
	}
	if err := os.WriteFile(finalPath, compressed.Data, 0o644); err != nil {
		return nil, fmt.Errorf("write compressed image %q: %w", finalPath, err)
	}
	return &StoreResult{
		Path:         finalPath,
		Name:         name,
		MimeType:     compressed.MimeType,
		OriginalSize: int64(len(data)),
		FinalSize:    int64(len(compressed.Data)),
		Compressed:   compressed.Compressed,
	}, nil
}

func defaultCompressedOutputPath(inputPath string) string {
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)
	return base + "-compressed" + ext
}
