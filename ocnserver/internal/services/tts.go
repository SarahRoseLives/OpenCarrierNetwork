package services

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
)

const (
	sampleRate    = 48000
	channels      = 1
	opusFrameSize = 960 // 20ms at 48kHz
)

type TTS struct {
	cacheDir string
	mu       sync.Mutex
}

func NewTTS(cacheDir string) *TTS {
	os.MkdirAll(cacheDir, 0755)
	return &TTS{cacheDir: cacheDir}
}

// GenerateOpusFrames generates TTS audio and returns raw Opus frames
func (t *TTS) GenerateOpusFrames(text string) ([][]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cacheFile := fmt.Sprintf("%s/tts_%x.opusframes", t.cacheDir, hashText(text))
	if cached, err := os.ReadFile(cacheFile); err == nil {
		frames := deserializeFrames(cached)
		if len(frames) > 0 {
			log.Printf("TTS cache hit: %d opus frames", len(frames))
			return frames, nil
		}
	}

	log.Printf("Generating TTS for: %s...", text[:min(30, len(text))])

	// Generate WAV with espeak
	wavCmd := exec.Command("espeak", "--stdout", text)
	wavOutput, err := wavCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("espeak failed: %w", err)
	}

	// Convert to Opus/Ogg with ffmpeg
	ffmpegCmd := exec.Command("ffmpeg",
		"-y",
		"-i", "pipe:0",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-c:a", "libopus",
		"-b:a", "48k",
		"-frame_duration", "20",
		"-f", "ogg",
		"pipe:1",
	)
	ffmpegCmd.Stdin = bytes.NewReader(wavOutput)

	oggData, err := ffmpegCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg opus encoding failed: %w", err)
	}

	// Parse Ogg container to extract raw Opus frames
	frames, err := parseOggOpus(oggData)
	if err != nil {
		return nil, fmt.Errorf("ogg parse failed: %w", err)
	}

	// Cache serialized frames
	if err := os.WriteFile(cacheFile, serializeFrames(frames), 0644); err != nil {
		log.Printf("Warning: failed to cache TTS: %v", err)
	}

	log.Printf("TTS generated: %d opus frames", len(frames))
	return frames, nil
}

// parseOggOpus extracts raw Opus frames from an Ogg/Opus container
func parseOggOpus(data []byte) ([][]byte, error) {
	var frames [][]byte
	offset := 0

	for offset < len(data) {
		// Check for OggS magic
		if offset+27 > len(data) {
			break
		}
		if string(data[offset:offset+4]) != "OggS" {
			return nil, fmt.Errorf("invalid Ogg page at offset %d", offset)
		}

		// Parse Ogg page header
		version := data[offset+4]
		_ = version
		typeFlags := data[offset+5]
		_ = typeFlags
		// granule := binary.LittleEndian.Uint64(data[offset+6 : offset+14])
		// _ = granule
		// serial := binary.LittleEndian.Uint32(data[offset+14 : offset+18])
		// _ = serial
		// pageSeq := binary.LittleEndian.Uint32(data[offset+18 : offset+22])
		// _ = pageSeq
		// checksum := binary.LittleEndian.Uint32(data[offset+22 : offset+26])
		// _ = checksum
		numSegments := int(data[offset+26])

		headerEnd := offset + 27
		segTableEnd := headerEnd + numSegments

		if segTableEnd > len(data) {
			break
		}

		// Read segment table
		segTable := data[headerEnd:segTableEnd]

		// Calculate total page data size
		pageDataSize := 0
		for _, s := range segTable {
			pageDataSize += int(s)
		}

		pageDataStart := segTableEnd
		pageDataEnd := pageDataStart + pageDataSize

		if pageDataEnd > len(data) {
			break
		}

		// Extract packets from segments
		// A packet spans multiple segments until a segment < 255
		packetStart := pageDataStart
		packetLen := 0
		for i := 0; i < numSegments; i++ {
			segLen := int(segTable[i])
			packetLen += segLen
			if segLen < 255 {
				// End of a packet
				packetEnd := packetStart + packetLen
				if packetEnd <= pageDataEnd {
					packet := data[packetStart:packetEnd]
					// Skip OpusHead and OpusTags packets
					if len(packet) >= 8 && string(packet[:8]) == "OpusHead" {
						// Skip header
					} else if len(packet) >= 8 && string(packet[:8]) == "OpusTag" {
						// Skip tags
					} else if len(packet) > 0 {
						// This is an Opus data frame
						frame := make([]byte, len(packet))
						copy(frame, packet)
						frames = append(frames, frame)
					}
				}
				packetStart = packetEnd
				packetLen = 0
			}
		}

		offset = pageDataEnd
	}

	return frames, nil
}

func serializeFrames(frames [][]byte) []byte {
	var buf bytes.Buffer
	for _, f := range frames {
		lenBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBytes, uint32(len(f)))
		buf.Write(lenBytes)
		buf.Write(f)
	}
	return buf.Bytes()
}

func deserializeFrames(data []byte) [][]byte {
	var frames [][]byte
	offset := 0
	for offset+4 <= len(data) {
		fLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if offset+fLen > len(data) {
			break
		}
		frame := make([]byte, fLen)
		copy(frame, data[offset:offset+fLen])
		frames = append(frames, frame)
		offset += fLen
	}
	return frames
}

func hashText(text string) uint32 {
	var h uint32
	for _, c := range text {
		h = h*31 + uint32(c)
	}
	return h
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
