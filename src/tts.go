package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/go-audio/wav"
)

func initOto() (*oto.Context, error) {
	op := &oto.NewContextOptions{
		SampleRate:   22050,
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	}
	otoCtx, _, err := oto.NewContext(op)
	if err != nil {
		return nil, fmt.Errorf("oto.NewContext failed: %w", err)
	}
	return otoCtx, nil
}

func generateTTSFiles() error {
	cmd := exec.Command("cmd", "/C", "generate_tts.bat")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run generate_tts.bat: %w", err)
	}
	return nil
}

func getWavForChannel(channel string) []byte {
	cfg := GetTwitchConfigFromFile("config.txt")
	ttspath := cfg.TTSPath
	if ttspath == "" {
		ttspath = "tts"
	}

	fileName := filepath.Join(ttspath, channel+".wav")
	body, err := os.ReadFile(fileName)
	if err != nil {
		log.Printf("Error reading TTS file %s: %v\n", fileName, err)
		return nil
	}
	return body
}

// getMp3ForChannel kept for compatibility
func getMp3ForChannel(channel string) []byte {
	return getWavForChannel(channel)
}

func playWav(otoCtx *oto.Context, file []byte, volume float64) {
	if len(file) == 0 {
		log.Println("Warning: Empty WAV data, skipping playback")
		return
	}
	fileBytesReader := bytes.NewReader(file)
	decoder := wav.NewDecoder(fileBytesReader)
	if !decoder.IsValidFile() {
		log.Println("Warning: Invalid WAV file, skipping playback")
		return
	}
	buf, err := decoder.FullPCMBuffer()
	if err != nil {
		log.Printf("Warning: failed to decode WAV: %s\n", err.Error())
		return
	}
	pcmData := make([]byte, len(buf.Data)*2)
	for i, sample := range buf.Data {
		s := int16(sample)
		pcmData[i*2] = byte(s)
		pcmData[i*2+1] = byte(s >> 8)
	}
	player := otoCtx.NewPlayer(bytes.NewReader(pcmData))
	player.SetVolume(volume)
	player.Play()
	for player.IsPlaying() {
		time.Sleep(time.Millisecond)
	}
	if err := player.Close(); err != nil {
		log.Printf("Warning: player.Close failed: %s\n", err.Error())
	}
}
