package imageopt

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
)

// Reject movie tracks before even DecodeConfig: heic.Decode uses DecodeAll for
// sequences, allocating all frames although the caller only asks for one photo.
// Check boxes, not raw byte matches, so compressed image payloads remain opaque.
func validateHEIC(data []byte) error {
	for offset := 0; offset < len(data); {
		if len(data)-offset < 8 {
			return fmt.Errorf("HEIC/HEIF 文件结构损坏")
		}
		size := uint64(binary.BigEndian.Uint32(data[offset:]))
		header := 8
		if size == 1 {
			if len(data)-offset < 16 {
				return fmt.Errorf("HEIC/HEIF 文件结构损坏")
			}
			size, header = binary.BigEndian.Uint64(data[offset+8:]), 16
		} else if size == 0 {
			size = uint64(len(data) - offset)
		}
		if size < uint64(header) || size > uint64(len(data)-offset) {
			return fmt.Errorf("HEIC/HEIF 文件结构损坏")
		}
		switch string(data[offset+4 : offset+8]) {
		case "moov":
			return fmt.Errorf("暂不支持 HEIC/HEIF 图片序列，请上传单张照片")
		case "ftyp":
			payload := data[offset+header : offset+int(size)]
			if len(payload) < 8 || len(payload)%4 != 0 {
				return fmt.Errorf("HEIC/HEIF 文件结构损坏")
			}
			for i := 0; i < len(payload); i += 4 {
				if i == 4 { // Minor version is not a compatible brand.
					continue
				}
				switch string(payload[i : i+4]) {
				case "msf1", "hevc", "hevx":
					return fmt.Errorf("暂不支持 HEIC/HEIF 图片序列，请上传单张照片")
				}
			}
		}
		offset += int(size)
	}
	return nil
}

func validateAnimation(data []byte, format string, cfg image.Config) (int, error) {
	switch format {
	case "gif":
		return 0, validateGIF(data, cfg)
	case "webp":
		return validateWebP(data, cfg)
	case "png":
		return 0, validatePNG(data, cfg)
	}
	return 0, nil
}

func animationBudget(frames int, cfg image.Config) error {
	// Decoders may retain full canvases for every frame. Bound that total before
	// decoding, even when frames themselves only update a small rectangle.
	if frames > MaxFrames || int64(frames)*int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return fmt.Errorf("动画图片过大（最多 %d 帧，累计画布像素不超过 %d）", MaxFrames, MaxPixels)
	}
	return nil
}

func validateGIF(data []byte, cfg image.Config) error {
	invalid := fmt.Errorf("GIF 数据损坏")
	if len(data) < 13 {
		return invalid
	}
	offset := 13
	if data[10]&0x80 != 0 {
		offset += 3 * (1 << (int(data[10]&7) + 1))
	}
	skipBlocks := func() bool {
		for offset < len(data) {
			size := int(data[offset])
			offset++
			if size > len(data)-offset {
				return false
			}
			offset += size
			if size == 0 {
				return true
			}
		}
		return false
	}
	frames := 0
	for offset < len(data) {
		tag := data[offset]
		offset++
		switch tag {
		case 0x21:
			if offset >= len(data) {
				return invalid
			}
			offset++ // Extension label.
			if !skipBlocks() {
				return invalid
			}
		case 0x2c:
			if len(data)-offset < 9 {
				return invalid
			}
			x := int(binary.LittleEndian.Uint16(data[offset:]))
			y := int(binary.LittleEndian.Uint16(data[offset+2:]))
			w := int(binary.LittleEndian.Uint16(data[offset+4:]))
			h := int(binary.LittleEndian.Uint16(data[offset+6:]))
			if w == 0 || h == 0 || x+w > cfg.Width || y+h > cfg.Height {
				return invalid
			}
			flags := data[offset+8]
			offset += 9
			if flags&0x80 != 0 {
				offset += 3 * (1 << (int(flags&7) + 1))
			}
			if offset >= len(data) {
				return invalid
			}
			offset++ // LZW minimum code size; DecodeAll validates the bitstream.
			if !skipBlocks() {
				return invalid
			}
			frames++
			if err := animationBudget(frames, cfg); err != nil {
				return err
			}
		case 0x3b:
			if frames > 0 && offset == len(data) {
				return nil
			}
			return invalid
		default:
			return invalid
		}
	}
	return invalid
}

func uint24(data []byte) int {
	return int(data[0]) | int(data[1])<<8 | int(data[2])<<16
}

func validateWebP(data []byte, cfg image.Config) (int, error) {
	invalid := fmt.Errorf("WebP 数据损坏")
	if len(data) < 12 || uint64(binary.LittleEndian.Uint32(data[4:]))+8 != uint64(len(data)) {
		return 0, invalid
	}
	frames := 0
	for offset := 12; offset < len(data); {
		if len(data)-offset < 8 {
			return 0, invalid
		}
		size := uint64(binary.LittleEndian.Uint32(data[offset+4:]))
		padded := size + size%2
		if padded > uint64(len(data)-offset-8) {
			return 0, invalid
		}
		if string(data[offset:offset+4]) == "ANMF" {
			if size < 16 {
				return 0, invalid
			}
			frame := data[offset+8 : offset+8+int(size)]
			x, y := 2*uint24(frame), 2*uint24(frame[3:])
			w, h := 1+uint24(frame[6:]), 1+uint24(frame[9:])
			if x+w > cfg.Width || y+h > cfg.Height {
				return 0, invalid
			}
			frames++
			if err := animationBudget(frames, cfg); err != nil {
				return 0, err
			}
		}
		offset += 8 + int(padded)
	}
	if animated(data, "webp") != (frames > 0) {
		return 0, invalid
	}
	return frames, nil
}

// PNG's standard decoder validates the default image but ignores APNG frames.
// Validate all CRCs, sequence numbers and frame rectangles, then decode each
// additional frame as a standalone PNG without retaining all decoded pixels.
func validatePNG(data []byte, cfg image.Config) error {
	invalid := fmt.Errorf("PNG/APNG 数据损坏")
	if len(data) < 33 {
		return invalid
	}
	var ihdr, palette, transparency []byte
	var frame bytes.Buffer
	var sequence uint32
	var declared, frames int
	var defaultData, frameData bool
	decodeFrame := func() error {
		if frame.Len() == 0 {
			return nil
		}
		if !frameData {
			return invalid
		}
		writePNGChunk(&frame, "IEND", nil)
		_, err := png.Decode(bytes.NewReader(frame.Bytes()))
		frame.Reset()
		return err
	}
	for offset := 8; offset < len(data); {
		if len(data)-offset < 12 {
			return invalid
		}
		size := uint64(binary.BigEndian.Uint32(data[offset:]))
		if size > uint64(len(data)-offset-12) {
			return invalid
		}
		end := offset + 8 + int(size)
		kind, payload := string(data[offset+4:offset+8]), data[offset+8:end]
		if crc32.ChecksumIEEE(data[offset+4:end]) != binary.BigEndian.Uint32(data[end:]) {
			return invalid
		}
		switch kind {
		case "IHDR":
			if offset != 8 || len(payload) != 13 {
				return invalid
			}
			ihdr = append([]byte(nil), payload...)
		case "PLTE":
			palette = payload
			if frame.Len() > 0 && !frameData {
				writePNGChunk(&frame, kind, payload)
			}
		case "tRNS":
			transparency = payload
			if frame.Len() > 0 && !frameData {
				writePNGChunk(&frame, kind, payload)
			}
		case "acTL":
			if len(payload) != 8 || declared != 0 || defaultData {
				return invalid
			}
			declared = int(binary.BigEndian.Uint32(payload))
			if declared == 0 {
				return invalid
			}
			if err := animationBudget(declared, cfg); err != nil {
				return err
			}
		case "fcTL":
			if declared == 0 || len(payload) != 26 || binary.BigEndian.Uint32(payload) != sequence {
				return invalid
			}
			sequence++
			if err := decodeFrame(); err != nil {
				return err
			}
			frames++
			if frames > declared {
				return invalid
			}
			w, h := binary.BigEndian.Uint32(payload[4:]), binary.BigEndian.Uint32(payload[8:])
			x, y := binary.BigEndian.Uint32(payload[12:]), binary.BigEndian.Uint32(payload[16:])
			if w == 0 || h == 0 || uint64(x)+uint64(w) > uint64(cfg.Width) || uint64(y)+uint64(h) > uint64(cfg.Height) || payload[24] > 2 || payload[25] > 1 {
				return invalid
			}
			if !defaultData && (int(w) != cfg.Width || int(h) != cfg.Height || x != 0 || y != 0) {
				return invalid
			}
			binary.BigEndian.PutUint32(ihdr, w)
			binary.BigEndian.PutUint32(ihdr[4:], h)
			frame.WriteString("\x89PNG\r\n\x1a\n")
			writePNGChunk(&frame, "IHDR", ihdr)
			if palette != nil {
				writePNGChunk(&frame, "PLTE", palette)
			}
			if transparency != nil {
				writePNGChunk(&frame, "tRNS", transparency)
			}
			frameData = false
		case "IDAT":
			if frames > 1 {
				return invalid
			}
			defaultData = true
			if frames == 1 {
				writePNGChunk(&frame, "IDAT", payload)
				frameData = true
			}
		case "fdAT":
			if !defaultData || frames == 0 || len(payload) < 4 || binary.BigEndian.Uint32(payload) != sequence {
				return invalid
			}
			sequence++
			writePNGChunk(&frame, "IDAT", payload[4:])
			frameData = true
		case "IEND":
			if len(payload) != 0 || end+4 != len(data) || !defaultData || frames != declared {
				return invalid
			}
			return decodeFrame()
		}
		offset = end + 4
	}
	return invalid
}

func writePNGChunk(output *bytes.Buffer, kind string, payload []byte) {
	var value [4]byte
	binary.BigEndian.PutUint32(value[:], uint32(len(payload)))
	output.Write(value[:])
	output.WriteString(kind)
	output.Write(payload)
	crc := crc32.Update(0, crc32.IEEETable, []byte(kind))
	crc = crc32.Update(crc, crc32.IEEETable, payload)
	binary.BigEndian.PutUint32(value[:], crc)
	output.Write(value[:])
}
