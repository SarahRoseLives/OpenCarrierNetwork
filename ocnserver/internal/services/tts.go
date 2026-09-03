package services

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
)

const (
	sampleRate = 48000
	channels   = 1
)

type TTS struct {
	cacheDir string
	mu       sync.Mutex
}

func NewTTS(cacheDir string) *TTS {
	os.MkdirAll(cacheDir, 0755)
	return &TTS{cacheDir: cacheDir}
}

// GeneratePCM generates TTS audio and returns raw PCM (16-bit, 48kHz, mono)
func (t *TTS) GeneratePCM(text string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cacheFile := fmt.Sprintf("%s/tts_%x.pcm", t.cacheDir, hashText(text))
	if data, err := os.ReadFile(cacheFile); err == nil {
		log.Printf("TTS cache hit")
		return data, nil
	}

	log.Printf("Generating TTS PCM for: %s...", text[:min(30, len(text))])

	wavCmd := exec.Command("espeak", "--stdout", text)
	wavOutput, err := wavCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("espeak failed: %w", err)
	}

	ffmpegCmd := exec.Command("ffmpeg",
		"-i", "pipe:0",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", fmt.Sprintf("%d", channels),
		"-f", "s16le",
		"pipe:1",
	)
	ffmpegCmd.Stdin = bytes.NewReader(wavOutput)

	pcmData, err := ffmpegCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w", err)
	}

	if err := os.WriteFile(cacheFile, pcmData, 0644); err != nil {
		log.Printf("Warning: failed to cache TTS: %v", err)
	}

	log.Printf("TTS generated: %d bytes PCM", len(pcmData))
	return pcmData, nil
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
