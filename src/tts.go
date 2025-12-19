package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/go-mp3"
)

func initOto() (*oto.Context, error) {
	// Prepare an Oto context (this will use your default audio device) that will
	// play all our sounds. Its configuration can't be changed later.
	op := &oto.NewContextOptions{
		SampleRate:   44100,
		ChannelCount: 1, // Mono output
		Format:       oto.FormatSignedInt16LE,
	}

	// Create Oto context
	otoCtx, _, err := oto.NewContext(op)
	if err != nil {
		return nil, fmt.Errorf("oto.NewContext failed: %w", err)
	}

	return otoCtx, nil
}

// Check if mp3 file for channel exists
func checkMp3(channel string) bool {
	if _, err := os.Stat("mp3/" + channel + ".mp3"); err == nil {
		return true
	}
	return false
}

func playMp3(otoCtx *oto.Context, file []byte, volume float64) {
	// Check if file data is valid
	if len(file) == 0 {
		log.Println("Warning: Empty MP3 data, skipping playback")
		return
	}

	fileBytesReader := bytes.NewReader(file)
	decodedMp3, err := mp3.NewDecoder(fileBytesReader)
	if err != nil {
		log.Printf("Warning: mp3.NewDecoder failed: %s, skipping playback\n", err.Error())
		return
	}
	player := otoCtx.NewPlayer(decodedMp3)

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

// Check if mp3 file exists, otherwise generate one
// TODO: add local TTS
// func getMp3ForChannel(channel string) []byte {
// 	os.MkdirAll("mp3", 0700)
// 	var body []byte
// 	haveFile := checkMp3(channel)
// 	if haveFile {
// 		body, err := os.ReadFile("mp3/" + channel + ".mp3")
// 		if err != nil {
// 			log.Printf("Error reading file: %v\n", err)
// 			return nil
// 		}
// 		return body
// 	} else {
// 		textParam := url.QueryEscape(channel + " is now live.")
// 		streamElementsUrl := fmt.Sprintf("https://api.streamelements.com/kappa/v2/speech?voice=Brian&text=%s", textParam)

// 		resp, err := http.Get(streamElementsUrl)
// 		if err != nil {
// 			log.Printf("Error fetching the URL: %v\n", err)
// 			return nil
// 		}
// 		defer resp.Body.Close()

// 		time.Sleep(500 * time.Millisecond)

// 		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
// 			log.Printf("Failed to fetch the URL. HTTP Status: %s\n%s\n", resp.Status, streamElementsUrl)
// 			return nil
// 		}

// 		body, err = io.ReadAll(resp.Body)
// 		if err != nil {
// 			log.Printf("Error reading response body: %v\n", err)
// 			return nil
// 		}
// 		err = os.WriteFile("mp3/"+channel+".mp3", body, 0644)
// 		if err != nil {
// 			log.Printf("error: %s\n", err)
// 		}
// 		log.Printf("Wrote %s.mp3\n", channel)
// 		return body
// 	}
// }

// This is coded badly but kinda saves the old format in case for future api tts.
func getMp3ForChannel(channel string) []byte {

	os.MkdirAll("mp3", 0700)

	fileName := filepath.Join("mp3", channel+".wav")

	haveOldFile := checkMp3(channel)
	if haveOldFile {

		var body []byte
		body, err := os.ReadFile("mp3/" + channel + ".mp3")
		if err != nil {
			log.Printf("Error reading file: %v\n", err)
			return nil
		}
		return body
	}

	// Check if we already have the file
	if _, err := os.Stat(fileName); err == nil {
		body, err := os.ReadFile(fileName)
		if err != nil {
			log.Printf("Error reading file: %v\n", err)
			return nil
		}
		return body
	}

	// If not Generate it locally
	log.Printf("Generating local TTS for %s...", channel)

	text := fmt.Sprintf("%s %s", channel, GetTwitchConfigFromFile("config.txt").TTSMessage)
	err := generateLocalTTS(text, fileName)
	if err != nil {
		log.Printf("Error generating TTS: %v\n", err)
		return nil
	}

	log.Printf("Wrote %s\n", fileName)

	// Read and return the new file
	haveOldFile = checkMp3(channel)
	if haveOldFile {

		var body []byte
		body, err := os.ReadFile("mp3/" + channel + ".mp3")
		if err != nil {
			log.Printf("Error reading file: %v\n", err)
			return nil
		}
		return body
	}

	log.Println("ERROR IN getMp3ForChannel end!")
	return []byte{}
}

func generateLocalTTS(text string, outputPath string) error {
	piperPath := "piper"
	modelPath := GetTwitchConfigFromFile("config.txt").TTSPath

	// Run Piper to generate WAV file
	cmd := exec.Command(piperPath, "--model", modelPath, "--output_file", outputPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stdin bytes.Buffer
	stdin.Write([]byte(text))
	cmd.Stdin = &stdin

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("piper execution failed: %v | stderr: %s", err, stderr.String())
	}

	// Convert WAV to MP3 using ffmpeg
	mp3Path := strings.TrimSuffix(outputPath, ".wav") + ".mp3"
	ffmpegCmd := exec.Command("ffmpeg", "-i", outputPath, "-codec:a", "libmp3lame", "-qscale:a", "2", mp3Path)
	ffmpegCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var ffmpegStderr bytes.Buffer
	ffmpegCmd.Stderr = &ffmpegStderr

	err = ffmpegCmd.Run()
	if err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %v | stderr: %s", err, ffmpegStderr.String())
	}

	// os.Remove(outputPath)

	return nil
}
