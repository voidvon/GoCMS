package imageopt

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/gen2brain/webp"
)

func TestMain(m *testing.M) {
	if RunWorker() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Synthetic 64x48 gradient, generated with macOS sips; no site content.
//
//go:embed testdata/gradient.heic
var gradientHEIC []byte

func gradient() *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, 128, 96))
	seed := uint32(1)
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			seed = seed*1664525 + 1013904223
			noise := int(seed >> 28)
			m.SetNRGBA(x, y, color.NRGBA{uint8(x*2 + noise), uint8(y*2 + noise), uint8(x + y + noise), 255})
		}
	}
	return m
}

func pngData(t *testing.T, img image.Image, level png.CompressionLevel) []byte {
	t.Helper()
	var b bytes.Buffer
	encoder := png.Encoder{CompressionLevel: level}
	if err := encoder.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func optimizeImage(t *testing.T, data []byte, options Config) Result {
	t.Helper()
	result, err := Optimize(context.Background(), data, options)
	if err != nil {
		t.Fatal(err)
	}
	actual, actualFormat, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil || actualFormat != result.Format || actual.Width != result.Width || actual.Height != result.Height {
		t.Fatalf("invalid result metadata: %+v %s %v", actual, actualFormat, err)
	}
	return result
}

func TestSmallestRepresentationAndThreshold(t *testing.T) {
	data := pngData(t, gradient(), png.NoCompression)
	result := optimizeImage(t, data, DefaultConfig())
	if result.Format != "webp" || len(data)-len(result.Data) < MinBytesSaved {
		t.Fatalf("expected smaller WebP, got %s (%d -> %d)", result.Format, len(data), len(result.Data))
	}
	t.Logf("PNG %d bytes -> WebP %d bytes", len(data), len(result.Data))
	// A small source cannot save 512 bytes, even if another format is smaller.
	small := pngData(t, image.NewNRGBA(image.Rect(0, 0, 3, 2)), png.DefaultCompression)
	if got := optimizeImage(t, small, DefaultConfig()); !bytes.Equal(got.Data, small) {
		t.Fatal("small image was needlessly re-encoded")
	}
	if got := optimizeImage(t, result.Data, DefaultConfig()); !bytes.Equal(got.Data, result.Data) {
		t.Fatal("small WebP was repeatedly re-encoded")
	}
	if got := optimizeImage(t, data, Config{Enabled: false}); !bytes.Equal(got.Data, data) {
		t.Fatal("disabled optimization changed upload")
	}
}

func TestTransparencySurvivesOptimization(t *testing.T) {
	img := gradient()
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			v := img.NRGBAAt(x, y)
			v.A = uint8(x * 2)
			img.SetNRGBA(x, y, v)
		}
	}
	data := pngData(t, img, png.NoCompression)
	result := optimizeImage(t, data, DefaultConfig())
	if result.Format == "jpeg" || len(result.Data) >= len(data) {
		t.Fatal("transparent input lost alpha or was not optimized")
	}
	decoded, _, err := image.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []int{0, 32, 64, 127} {
		_, _, _, want := img.At(x, 40).RGBA()
		_, _, _, got := decoded.At(x, 40).RGBA()
		if got != want {
			t.Fatalf("alpha at x=%d: %d != %d", x, got, want)
		}
	}
}

func TestAnimatedImagesKeepOriginalBytes(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	frames := []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 16, 16), palette), image.NewPaletted(image.Rect(0, 0, 16, 16), palette)}
	frames[1].SetColorIndex(0, 0, 1)
	var gifBytes, webpBytes bytes.Buffer
	if err := gif.EncodeAll(&gifBytes, &gif.GIF{Image: frames, Delay: []int{10, 20}}); err != nil {
		t.Fatal(err)
	}
	if err := webp.EncodeAll(&webpBytes, &webp.WEBP{Image: []image.Image{frames[0], frames[1]}, Delay: []int{100, 200}}, webp.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"gif": gifBytes.Bytes(), "webp": webpBytes.Bytes(), "apng": animatedPNG(t)} {
		t.Run(name, func(t *testing.T) {
			if got := optimizeImage(t, data, DefaultConfig()); !bytes.Equal(got.Data, data) {
				t.Fatal("animation was modified")
			}
		})
	}
}

func pngChunk(kind string, payload []byte) []byte {
	result := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(result, uint32(len(payload)))
	copy(result[4:], kind)
	copy(result[8:], payload)
	binary.BigEndian.PutUint32(result[len(result)-4:], crc32.ChecksumIEEE(result[4:len(result)-4]))
	return result
}

func animatedPNG(t *testing.T) []byte {
	first := pngData(t, gradient(), png.DefaultCompression)
	secondImage := gradient()
	secondImage.Set(0, 0, color.White)
	second := pngData(t, secondImage, png.DefaultCompression)
	result := append([]byte{}, first[:33]...)
	result = append(result, pngChunk("acTL", []byte{0, 0, 0, 2, 0, 0, 0, 0})...)
	control := make([]byte, 26)
	binary.BigEndian.PutUint32(control[4:], 128)
	binary.BigEndian.PutUint32(control[8:], 96)
	control[21], control[23] = 1, 10
	result = append(result, pngChunk("fcTL", control)...)
	result = append(result, first[33:len(first)-12]...)
	binary.BigEndian.PutUint32(control, 1)
	result = append(result, pngChunk("fcTL", control)...)
	sequence := uint32(2)
	for offset := 33; offset+12 <= len(second); {
		n := int(binary.BigEndian.Uint32(second[offset:]))
		if string(second[offset+4:offset+8]) == "IDAT" {
			payload := make([]byte, n+4)
			binary.BigEndian.PutUint32(payload, sequence)
			copy(payload[4:], second[offset+8:offset+8+n])
			result = append(result, pngChunk("fdAT", payload)...)
			sequence++
		}
		offset += n + 12
	}
	return append(result, pngChunk("IEND", nil)...)
}

func TestJPEGOrientationAndHEICConversion(t *testing.T) {
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, gradient(), &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	// APP1 with a little-endian TIFF orientation tag (6 = rotate clockwise).
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	segment := []byte{0xff, 0xe1, 0, byte(len(exif) + 2)}
	data := append([]byte{}, jpegBytes.Bytes()[:2]...)
	data = append(data, segment...)
	data = append(data, exif...)
	data = append(data, jpegBytes.Bytes()[2:]...)
	got := optimizeImage(t, data, DefaultConfig())
	if got.Width != 96 || got.Height != 128 {
		t.Fatalf("orientation not applied: %dx%d", got.Width, got.Height)
	}
	for _, enabled := range []bool{true, false} {
		result := optimizeImage(t, gradientHEIC, Config{Enabled: enabled, Quality: 80})
		if result.Format == "heic" || result.Width != 64 || result.Height != 48 {
			t.Fatalf("HEIC conversion failed: %s %dx%d", result.Format, result.Width, result.Height)
		}
		if _, _, err := image.Decode(bytes.NewReader(result.Data)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidImagesAndCancellation(t *testing.T) {
	data := pngData(t, gradient(), png.DefaultCompression)
	for _, enabled := range []bool{true, false} {
		if _, err := Optimize(context.Background(), data[:33], Config{Enabled: enabled, Quality: 80}); err == nil {
			t.Fatal("truncated PNG was accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Optimize(ctx, data, DefaultConfig()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled optimization: %v", err)
	}
}

func TestWorkerDeadlineAndSlotRecovery(t *testing.T) {
	// This source takes long enough to encode that cancellation happens inside
	// the codec, rather than just being checked between candidate encodings.
	img := image.NewNRGBA(image.Rect(0, 0, 1600, 1200))
	seed := uint32(1)
	for y := 0; y < 1200; y++ {
		for x := 0; x < 1600; x++ {
			seed = seed*1664525 + 1013904223
			n := int(seed >> 28)
			img.SetNRGBA(x, y, color.NRGBA{uint8(x + n), uint8(y + n), uint8(x + y + n), 255})
		}
	}
	data := pngData(t, img, png.NoCompression)
	started := time.Now()
	_, err := run(context.Background(), data, DefaultConfig(), 200*time.Millisecond, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("worker timeout: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("worker was not stopped: %s", elapsed)
	} else {
		t.Logf("200ms deadline stopped and reaped worker in %s", elapsed)
	}
	// A timed-out worker must be reaped and release its slot for the next upload.
	small := pngData(t, gradient(), png.DefaultCompression)
	optimizeImage(t, small, DefaultConfig())
}

func BenchmarkOptimizeCameraPhoto(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 4000, 3000))
	seed := uint32(1)
	for y := 0; y < 3000; y++ {
		for x := 0; x < 4000; x++ {
			seed = seed*1664525 + 1013904223
			n := int(seed >> 28)
			img.SetNRGBA(x, y, color.NRGBA{uint8(x + n), uint8(y + n), uint8(x + y + n), 255})
		}
	}
	var source bytes.Buffer
	if err := jpeg.Encode(&source, img, &jpeg.Options{Quality: 90}); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(source.Len()))
	b.ResetTimer()
	for b.Loop() {
		result, err := Optimize(context.Background(), source.Bytes(), DefaultConfig())
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(result.Data)), "output-bytes")
	}
}

func TestBoundedQueueAndCanceledWait(t *testing.T) {
	data := pngData(t, gradient(), png.DefaultCompression)
	for i := 0; i < cap(admission); i++ {
		admission <- struct{}{}
	}
	_, err := Optimize(context.Background(), data, DefaultConfig())
	for i := 0; i < cap(admission); i++ {
		<-admission
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue: %v", err)
	}
	processing <- struct{}{}
	_, err = run(context.Background(), data, DefaultConfig(), time.Second, 10*time.Millisecond)
	<-processing
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("queue deadline: %v", err)
	}
	processing <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = Optimize(ctx, data, DefaultConfig())
	<-processing
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled queue wait: %v", err)
	}
	optimizeImage(t, data, DefaultConfig())
}
