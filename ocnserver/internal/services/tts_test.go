package services

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestGenerateOpusFrames(t *testing.T) {
	tts := NewTTS("/tmp/tts_test")

	frames, err := tts.GenerateOpusFrames("Hello, this is a test")
	if err != nil {
		t.Fatalf("GenerateOpusFrames failed: %v", err)
	}

	if len(frames) == 0 {
		t.Fatal("No opus frames generated")
	}

	t.Logf("Generated %d opus frames", len(frames))
	for i, f := range frames {
		t.Logf("Frame %d: %d bytes", i, len(f))
		if i >= 3 {
			break
		}
	}
}

func TestParseOggOpus(t *testing.T) {
	// Generate a small Opus/Ogg file with ffmpeg
	wavCmd := exec.Command("espeak", "--stdout", "Test")
	wavOutput, err := wavCmd.Output()
	if err != nil {
		t.Fatalf("espeak failed: %v", err)
	}

	ffmpegCmd := exec.Command("ffmpeg",
		"-y",
		"-i", "pipe:0",
		"-ar", "48000",
		"-ac", "1",
		"-c:a", "libopus",
		"-b:a", "48k",
		"-frame_duration", "20",
		"-f", "ogg",
		"pipe:1",
	)
	ffmpegCmd.Stdin = bytes.NewReader(wavOutput)

	oggData, err := ffmpegCmd.Output()
	if err != nil {
		t.Fatalf("ffmpeg failed: %v", err)
	}

	t.Logf("Ogg data: %d bytes", len(oggData))

	frames, err := parseOggOpus(oggData)
	if err != nil {
		t.Fatalf("parseOggOpus failed: %v", err)
	}

	if len(frames) == 0 {
		t.Fatal("No frames extracted")
	}

	t.Logf("Extracted %d opus frames", len(frames))
	for i, f := range frames {
		t.Logf("Frame %d: %d bytes", i, len(f))
		if i >= 3 {
			break
		}
	}
}
