// Package imageopt selects the smallest useful representation of an upload.
// Codecs are embedded and work without CGo or external conversion programs.
package imageopt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/gif"
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
	MaxBytes      = 10 << 20
	MaxDimension  = 12000
	MaxPixels     = 48_000_000
	MaxFrames     = 200
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

// optimize runs only in the disposable worker. Header inspection, validation and
// pixel decoding all share its deadline and cannot block the HTTP server.
func optimize(data []byte, options Config) (result Result, err error) {
	defer func() {
		if recover() != nil {
			result, err = Result{}, fmt.Errorf("图片数据损坏或格式不受支持")
		}
	}()
	if len(data) == 0 {
		return Result{}, fmt.Errorf("图片文件为空")
	}
	if len(data) > MaxBytes {
		return Result{}, ErrTooLarge
	}
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		if err := validateHEIC(data); err != nil {
			return Result{}, err
		}
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("文件不是受支持的图片格式（JPEG、PNG、GIF、WebP、HEIC/HEIF）")
	}
	if !validDimensions(cfg.Width, cfg.Height) {
		return Result{}, fmt.Errorf("图片尺寸过大")
	}
	original := Result{Data: data, Format: format, Width: cfg.Width, Height: cfg.Height}
	mustConvert := format == "heic"
	animationFrames, err := validateAnimation(data, format, cfg)
	if err != nil {
		return Result{}, fmt.Errorf("图片校验失败: %w", err)
	}
	var img image.Image
	switch format {
	case "webp":
		if animated(data, format) {
			var decoded *webp.WEBP
			decoded, err = webp.DecodeAll(bytes.NewReader(data))
			if err == nil && len(decoded.Image) != animationFrames {
				err = fmt.Errorf("WebP 动画帧不完整")
			}
			break
		}
		img, err = webp.Decode(bytes.NewReader(data), webp.Options{AutoRotate: true})
	case "heic":
		img, err = heic.Decode(bytes.NewReader(data))
	case "gif":
		_, err = gif.DecodeAll(bytes.NewReader(data))
	case "jpeg", "png":
		img, err = imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	default:
		return Result{}, fmt.Errorf("只支持 JPEG、PNG、GIF、WebP 和 HEIC/HEIF 图片")
	}
	if err != nil {
		if mustConvert {
			return Result{}, fmt.Errorf("HEIC/HEIF 图片转换失败: %w", err)
		}
		return Result{}, fmt.Errorf("图片数据损坏，无法解码: %w", err)
	}
	if !mustConvert && (!options.Enabled || format == "gif" || animated(data, format)) {
		return original, nil
	}
	if !validDimensions(img.Bounds().Dx(), img.Bounds().Dy()) {
		return Result{}, fmt.Errorf("图片尺寸过大")
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
		output := limitedBuffer{limit: MaxBytes}
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
	if len(best.Data) == 0 {
		return Result{}, fmt.Errorf("图片转换失败")
	}
	if !mustConvert && len(original.Data)-len(best.Data) < MinBytesSaved {
		return original, nil
	}
	return best, nil
}

func validDimensions(width, height int) bool {
	return width > 0 && height > 0 && width <= MaxDimension && height <= MaxDimension && int64(width)*int64(height) <= MaxPixels
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
