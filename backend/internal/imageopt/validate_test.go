package imageopt

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/gen2brain/webp"
)

func TestDamagedImagesRejectedWithOptimizationOnAndOff(t *testing.T) {
	var jpegBytes, gifBytes, webpBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, gradient(), nil); err != nil {
		t.Fatal(err)
	}
	frames := []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.Black, color.White})}
	if err := gif.EncodeAll(&gifBytes, &gif.GIF{Image: frames, Delay: []int{10}}); err != nil {
		t.Fatal(err)
	}
	if err := webp.Encode(&webpBytes, gradient()); err != nil {
		t.Fatal(err)
	}
	wp := append([]byte(nil), webpBytes.Bytes()...)
	wp = wp[:len(wp)-10]
	// A plausible RIFF length must not let a truncated VP8 payload through.
	binary.LittleEndian.PutUint32(wp[4:], uint32(len(wp)-8))
	images := map[string][]byte{
		"png":  pngData(t, gradient(), png.DefaultCompression)[:33],
		"jpeg": jpegBytes.Bytes()[:jpegBytes.Len()-30],
		"gif":  gifBytes.Bytes()[:gifBytes.Len()-3],
		"webp": wp,
	}
	// Break only the second APNG frame, leaving the default image valid and
	// recomputing its CRC so full pixel validation, rather than CRC alone, catches it.
	apng := animatedPNG(t)
	for offset := 8; offset+12 <= len(apng); {
		n := int(binary.BigEndian.Uint32(apng[offset:]))
		if string(apng[offset+4:offset+8]) == "fdAT" {
			payload := append([]byte(nil), apng[offset+8:offset+8+n]...)
			payload[4] ^= 0xff
			copy(apng[offset:], pngChunk("fdAT", payload))
			break
		}
		offset += n + 12
	}
	images["apng-second-frame"] = apng
	webpBytes.Reset()
	if err := webp.EncodeAll(&webpBytes, &webp.WEBP{Image: []image.Image{gradient(), image.NewNRGBA(image.Rect(0, 0, 128, 96))}, Delay: []int{100, 200}}); err != nil {
		t.Fatal(err)
	}
	animatedWebP := append([]byte(nil), webpBytes.Bytes()...)
	frame := 0
	for offset := 12; offset+8 <= len(animatedWebP); {
		n := int(binary.LittleEndian.Uint32(animatedWebP[offset+4:]))
		if string(animatedWebP[offset:offset+4]) == "ANMF" {
			frame++
			if frame == 2 {
				clear(animatedWebP[offset+8+16 : offset+8+n])
				break
			}
		}
		offset += 8 + n + n%2
	}
	images["webp-second-frame"] = animatedWebP
	for name, data := range images {
		for _, enabled := range []bool{true, false} {
			t.Run(name+"/"+map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
				if _, err := Optimize(context.Background(), data, Config{Enabled: enabled, Quality: 80}); err == nil {
					t.Fatal("damaged image was accepted")
				}
			})
		}
	}
}

func TestIndexedAPNGWithPaletteAfterFrameControl(t *testing.T) {
	palette := color.Palette{color.NRGBA{A: 0}, color.White}
	img := image.NewPaletted(image.Rect(0, 0, 8, 6), palette)
	img.SetColorIndex(3, 2, 1)
	static := pngData(t, img, png.DefaultCompression)
	// fcTL may precede PLTE/tRNS. Both the default-image decoder and the
	// reconstructed frame must see that palette and transparency information.
	data := append([]byte(nil), static[:33]...)
	data = append(data, pngChunk("acTL", []byte{0, 0, 0, 1, 0, 0, 0, 0})...)
	control := make([]byte, 26)
	binary.BigEndian.PutUint32(control[4:], 8)
	binary.BigEndian.PutUint32(control[8:], 6)
	control[21], control[23] = 1, 10
	data = append(data, pngChunk("fcTL", control)...)
	data = append(data, static[33:]...)
	result := optimizeImage(t, data, DefaultConfig())
	if !bytes.Equal(result.Data, data) {
		t.Fatal("indexed APNG was changed")
	}
}

func heicBox(kind string, payload []byte) []byte {
	result := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(result, uint32(len(result)))
	copy(result[4:], kind)
	copy(result[8:], payload)
	return result
}

func TestHEICSequencesRejectedBeforeDecoding(t *testing.T) {
	for _, brand := range []string{"msf1", "hevc", "hevx"} {
		data := append([]byte(nil), gradientHEIC...)
		copy(data[8:12], brand)
		for _, enabled := range []bool{true, false} {
			_, err := Optimize(context.Background(), data, Config{Enabled: enabled, Quality: 80})
			if err == nil || !strings.Contains(err.Error(), "图片序列") {
				t.Fatalf("sequence brand %s accepted: %v", brand, err)
			}
		}
	}
	// Even a still-image brand must not conceal a movie track. Check both box
	// size encodings and a sequence brand present only in compatible_brands.
	ftyp := heicBox("ftyp", []byte("mif1\x00\x00\x00\x00heic"))
	large := make([]byte, 16)
	binary.BigEndian.PutUint32(large, 1)
	copy(large[4:], "moov")
	binary.BigEndian.PutUint64(large[8:], 16)
	for _, data := range [][]byte{
		append(append([]byte(nil), ftyp...), heicBox("moov", nil)...),
		append(append([]byte(nil), ftyp...), large...),
		heicBox("ftyp", []byte("mif1\x00\x00\x00\x00heicmsf1")),
	} {
		if err := validateHEIC(data); err == nil || !strings.Contains(err.Error(), "图片序列") {
			t.Fatalf("concealed sequence was not rejected: %v", err)
		}
	}
	// A byte match in opaque image data is not a movie box.
	if err := validateHEIC(append(ftyp, heicBox("mdat", []byte("moov"))...)); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{ftyp[:9], append(append([]byte(nil), ftyp...), large[:15]...)} {
		if err := validateHEIC(data); err == nil {
			t.Fatal("truncated container was accepted")
		}
	}
}

func TestAnimationAllocationBudget(t *testing.T) {
	// Identical tiny frame payloads with large canvas dimensions would otherwise
	// cause DecodeAll to allocate frames * canvas pixels from a tiny upload.
	var data bytes.Buffer
	frames := []*image.Paletted{
		image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White}),
		image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White}),
	}
	if err := gif.EncodeAll(&data, &gif.GIF{Image: frames, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	g := append([]byte(nil), data.Bytes()...)
	binary.LittleEndian.PutUint16(g[6:], 5000)
	binary.LittleEndian.PutUint16(g[8:], 5000)
	_, err := Optimize(context.Background(), g, DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "累计画布像素") {
		t.Fatalf("GIF allocation budget: %v", err)
	}
	apng := animatedPNG(t)
	for offset := 8; offset+12 <= len(apng); {
		n := int(binary.BigEndian.Uint32(apng[offset:]))
		if string(apng[offset+4:offset+8]) == "acTL" {
			payload := append([]byte(nil), apng[offset+8:offset+8+n]...)
			binary.BigEndian.PutUint32(payload, MaxFrames+1)
			copy(apng[offset:], pngChunk("acTL", payload))
			break
		}
		offset += n + 12
	}
	_, err = Optimize(context.Background(), apng, DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "动画图片过大") {
		t.Fatalf("APNG frame budget: %v", err)
	}
	data.Reset()
	frames[1].SetColorIndex(0, 0, 1)
	if err := webp.EncodeAll(&data, &webp.WEBP{Image: []image.Image{frames[0], frames[1]}, Delay: []int{100, 200}}); err != nil {
		t.Fatal(err)
	}
	w := data.Bytes()
	// VP8X stores canvas dimensions minus one as little-endian uint24s.
	w[24], w[25], w[26] = 0x87, 0x13, 0 // 5000 - 1
	w[27], w[28], w[29] = 0x87, 0x13, 0
	_, err = Optimize(context.Background(), w, DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "累计画布像素") {
		t.Fatalf("WebP allocation budget: %v", err)
	}
}

func TestCandidateOutputLimit(t *testing.T) {
	output := limitedBuffer{limit: 10}
	// In particular, io.Copy must not bypass the cap through a promoted
	// bytes.Buffer.ReadFrom method when collecting child stdout.
	_, err := io.Copy(&output, io.LimitReader(strings.NewReader(strings.Repeat("x", 100)), 100))
	if err == nil || output.Len() > 10 {
		t.Fatalf("unbounded encoder/worker output: %d %v", output.Len(), err)
	}
}
