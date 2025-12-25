package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/go-audio/wav"
	sherpa "github.com/k2-fsa/sherpa-onnx-go-windows"
)

var ttsEngine *sherpa.OfflineTts

func initTTS() error {
	config := sherpa.OfflineTtsConfig{}

	config.Model.Vits.Model = "tts\\en_US-joe-medium.onnx"
	config.Model.Vits.DataDir = "tts\\espeak-ng-data"
	config.Model.Vits.Tokens = "tts\\tokens.txt"
	config.Model.NumThreads = 1
	config.Model.Debug = 0

	tts := sherpa.NewOfflineTts(&config)
	if tts == nil {
		return fmt.Errorf("failed to create TTS engine")
	}

	ttsEngine = tts
	return nil
}

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

	err = player.Close()
	if err != nil {
		log.Printf("Warning: player.Close failed: %s\n", err.Error())
	}
}

func getMp3ForChannel(channel string) []byte {
	return getWavForChannel(channel)
}

func getWavForChannel(channel string) []byte {
	os.MkdirAll("audio", 0700)
	fileName := filepath.Join("audio", channel+".wav")

	if _, err := os.Stat(fileName); err == nil {
		body, err := os.ReadFile(fileName)
		if err != nil {
			log.Printf("Error reading file: %v\n", err)
			return nil
		}
		return body
	}

	log.Printf("Generating local TTS for %s...", channel)

	text := fmt.Sprintf("%s %s", channel, GetTwitchConfigFromFile("config.txt").TTSMessage)
	err := generateLocalTTS(text, fileName)
	if err != nil {
		log.Printf("Error generating TTS: %v\n", err)
		return nil
	}

	log.Printf("Wrote %s\n", fileName)

	body, err := os.ReadFile(fileName)
	if err != nil {
		log.Printf("Error reading file: %v\n", err)
		return nil
	}

	return body
}

func generateLocalTTS(text string, outputPath string) error {
	if ttsEngine == nil {
		return fmt.Errorf("TTS engine not initialized")
	}

	audio := ttsEngine.Generate(text, 0, 1.0)
	if audio == nil {
		return fmt.Errorf("failed to generate audio")
	}

	err := audio.Save(outputPath)
	if !err {
		return fmt.Errorf("failed to save audio: %v", err)
	}

	return nil
}

func cleanupTTS() {
	if ttsEngine != nil {
		sherpa.DeleteOfflineTts(ttsEngine)
		ttsEngine = nil
	}
}
