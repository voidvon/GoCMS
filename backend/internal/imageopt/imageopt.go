// Package imageopt selects the smallest useful representation of an upload.
// Codecs are embedded and work without CGo or external conversion programs.
package imageopt

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/heic"
	"github.com/gen2brain/webp"
)

const (
	EnabledKey    = "image_optimization_enabled"
	QualityKey    = "image_optimization_quality"
	MinBytesSaved = 512
)

type Config struct {
	Enabled bool
	Quality int
}

func DefaultConfig() Config { return Config{Enabled: true, Quality: 80} }

func ParseSettings(settings map[string]string) (Config, error) {
	cfg := DefaultConfig()
	if raw, ok := settings[EnabledKey]; ok {
		if raw != "true" && raw != "false" {
			return cfg, fmt.Errorf("图片自动优化开关必须为 true 或 false")
		}
		cfg.Enabled = raw == "true"
	}
	if raw, ok := settings[QualityKey]; ok {
		quality, err := strconv.Atoi(raw)
		if err != nil || quality < 1 || quality > 100 {
			return cfg, fmt.Errorf("图片优化质量必须是 1 到 100 的整数")
		}
		cfg.Quality = quality
	}
	return cfg, nil
}

type Result struct {
	Data          []byte
	Format        string
	Width, Height int
}

// Serialize pixel decoding and encoding to bound concurrent image memory use.
var processing = make(chan struct{}, 1)

// Optimize expects the caller to validate file size and DecodeConfig dimensions
// first. GIF, APNG and animated WebP keep their frames and timing unchanged.
// HEIC is always converted to a browser-compatible format, even when disabled.
func Optimize(ctx context.Context, data []byte, format string, cfg image.Config, options Config) (Result, error) {
	original := Result{Data: data, Format: format, Width: cfg.Width, Height: cfg.Height}
	mustConvert := format == "heic"
	if !mustConvert && (!options.Enabled || format == "gif" || animated(data, format)) {
		return original, nil
	}
	select {
	case processing <- struct{}{}:
		defer func() { <-processing }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	var img image.Image
	var err error
	switch format {
	case "webp":
		img, err = webp.Decode(bytes.NewReader(data), webp.Options{AutoRotate: true})
	case "heic":
		img, err = heic.Decode(bytes.NewReader(data))
	default:
		img, err = imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	}
	if err != nil {
		if mustConvert {
			return Result{}, fmt.Errorf("HEIC/HEIF 图片转换失败: %w", err)
		}
		// A failed optimization must not discard an otherwise accepted upload.
		return original, nil
	}
	quality := options.Quality
	if quality < 1 || quality > 100 {
		quality = 80
	}
	best := original
	if mustConvert {
		best = Result{}
	}
	consider := func(format string, encode func(io.Writer) error) {
		if ctx.Err() != nil {
			return
		}
		var output bytes.Buffer
		if err := encode(&output); err != nil || output.Len() == 0 {
			return
		}
		if len(best.Data) == 0 || output.Len() < len(best.Data) {
			best = Result{Data: output.Bytes(), Format: format, Width: img.Bounds().Dx(), Height: img.Bounds().Dy()}
		}
	}
	consider("webp", func(w io.Writer) error {
		return webp.Encode(w, img, webp.Options{Quality: quality, Method: 6})
	})
	if opaque(img) {
		consider("jpeg", func(w io.Writer) error {
			return jpeg.Encode(w, img, &jpeg.Options{Quality: min(100, quality+2)})
		})
	}
	// Keep PNG lossless, so diagrams and text are not palette-quantized.
	consider("png", func(w io.Writer) error {
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		return encoder.Encode(w, img)
	})
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(best.Data) == 0 {
		return Result{}, fmt.Errorf("图片转换失败")
	}
	if !mustConvert && len(original.Data)-len(best.Data) < MinBytesSaved {
		return original, nil
	}
	return best, nil
}

func opaque(img image.Image) bool {
	if v, ok := img.(interface{ Opaque() bool }); ok {
		return v.Opaque()
	}
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
				return false
			}
		}
	}
	return true
}

func animated(data []byte, format string) bool {
	if format == "webp" && len(data) >= 21 && string(data[12:16]) == "VP8X" {
		return data[20]&2 != 0
	}
	if format == "png" {
		for offset := 8; offset+12 <= len(data); {
			size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
			if string(data[offset+4:offset+8]) == "acTL" {
				return true
			}
			if size+12 > uint64(len(data)-offset) {
				break
			}
			offset += int(size) + 12
		}
	}
	return false
}

func init() {
	// HEIF's generic brand is also used by HEIC files; the decoder validates the
	// actual codec. Other HEIF codecs (e.g. AVIF) are not implicitly accepted.
	image.RegisterFormat("heic", "????ftypmif1", heic.Decode, heic.DecodeConfig)
}
