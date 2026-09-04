package voicemail

import (
	"encoding/binary"
	"fmt"
)

// Raw opus frames are stored/serialized as 4-byte little-endian length +
// payload. These helpers and the Ogg/Opus writer let us turn a message back
// into a playable container for the app.

const (
	frameSampleRate = 48000
	frameChannels   = 1
	frameSize       = 960 // 20ms at 48kHz
	frameMS         = 20
)

// SerializeFrames packs opus frames for storage.
func SerializeFrames(frames [][]byte) []byte {
	out := make([]byte, 0, len(frames)*4)
	buf := make([]byte, 4)
	for _, f := range frames {
		binary.LittleEndian.PutUint32(buf, uint32(len(f)))
		out = append(out, buf...)
		out = append(out, f...)
	}
	return out
}

// DeserializeFrames unpacks frames produced by SerializeFrames.
func DeserializeFrames(data []byte) [][]byte {
	var frames [][]byte
	offset := 0
	for offset+4 <= len(data) {
		n := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if n < 0 || offset+n > len(data) {
			break
		}
		frame := make([]byte, n)
		copy(frame, data[offset:offset+n])
		frames = append(frames, frame)
		offset += n
	}
	return frames
}

// BuildOgg muxes opus frames into an Ogg/Opus container for playback.
func BuildOgg(frames [][]byte) ([]byte, error) {
	w := &oggWriter{serial: 0x4f434e31} // "OCN1"
	if err := w.writePage(idHeader(), 0x02, 0, true); err != nil {
		return nil, err
	}
	if err := w.writePage(commentHeader(), 0x00, 0, false); err != nil {
		return nil, err
	}
	// One audio packet per frame, one page per frame (simple, valid Ogg).
	for i, f := range frames {
		flags := byte(0x00)
		last := i == len(frames)-1
		if last {
			flags = 0x04
		}
		granule := uint64((i + 1) * frameSize)
		if err := w.writePage(f, flags, granule, false); err != nil {
			return nil, err
		}
	}
	return w.buf, nil
}

func idHeader() []byte {
	// OpusHead: magic + version + channels + preskip + input rate + gain + mapping
	h := make([]byte, 19)
	copy(h[0:8], "OpusHead")
	h[8] = 1                                   // version
	h[9] = frameChannels                       // channels
	binary.LittleEndian.PutUint16(h[10:12], 0) // preskip
	binary.LittleEndian.PutUint32(h[12:16], frameSampleRate)
	binary.LittleEndian.PutUint16(h[16:18], 0) // output gain
	h[18] = 0                                  // mapping family
	return h
}

func commentHeader() []byte {
	vendor := "OCN"
	buf := make([]byte, 0, 8+len(vendor)+4)
	buf = append(buf, []byte("OpusTags")...)
	vl := make([]byte, 4)
	binary.LittleEndian.PutUint32(vl, uint32(len(vendor)))
	buf = append(buf, vl...)
	buf = append(buf, vendor...)
	buf = append(buf, 0, 0, 0, 0) // no user comments
	return buf
}

// oggWriter assembles pages and tracks the running CRC checksum.
type oggWriter struct {
	buf    []byte
	serial uint32
	seq    uint32
}

func (w *oggWriter) writePage(packet []byte, flags byte, granule uint64, isHeader bool) error {
	if len(packet) == 0 {
		return nil
	}
	// Build segment table.
	var segs []byte
	rem := len(packet)
	for rem > 0 {
		if rem >= 255 {
			segs = append(segs, 255)
			rem -= 255
		} else {
			segs = append(segs, byte(rem))
			rem = 0
		}
	}
	if len(packet)%255 == 0 {
		// Exact multiple: a terminating zero-length segment marks the packet end.
		segs = append(segs, 0)
	}
	if len(segs) > 255 {
		return fmt.Errorf("ogg: packet too large for one page (%d segments)", len(segs))
	}

	page := make([]byte, 0, 27+len(segs)+len(packet))
	page = append(page, 'O', 'g', 'g', 'S')
	page = append(page, 0) // version
	page = append(page, flags)
	var gb [8]byte
	binary.LittleEndian.PutUint64(gb[:], granule)
	page = append(page, gb[:]...)
	var sb [4]byte
	binary.LittleEndian.PutUint32(sb[:], w.serial)
	page = append(page, sb[:]...)
	binary.LittleEndian.PutUint32(sb[:], w.seq)
	page = append(page, sb[:]...)
	page = append(page, 0, 0, 0, 0) // crc placeholder
	page = append(page, byte(len(segs)))
	page = append(page, segs...)
	page = append(page, packet...)

	crc := crc32(page)
	binary.LittleEndian.PutUint32(page[22:26], crc)

	w.buf = append(w.buf, page...)
	w.seq++
	_ = isHeader
	return nil
}

// Ogg uses CRC-32 with polynomial 0x04c11db7 (reflected, init 0, no xorout).
var crcTable = func() [256]uint32 {
	var t [256]uint32
	for i := 0; i < 256; i++ {
		c := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if c&0x80000000 != 0 {
				c = (c << 1) ^ 0x04c11db7
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return t
}()

func crc32(data []byte) uint32 {
	var c uint32
	for _, b := range data {
		c = (c << 8) ^ crcTable[byte(c>>24)^b]
	}
	return c
}
