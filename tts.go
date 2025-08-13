package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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
func getMp3ForChannel(channel string) []byte {
	os.MkdirAll("mp3", 0700)
	var body []byte
	haveFile := checkMp3(channel)
	if haveFile {
		body, err := os.ReadFile("mp3/" + channel + ".mp3")
		if err != nil {
			log.Printf("Error reading file: %v\n", err)
			return nil
		}
		return body
	} else {
		textParam := url.QueryEscape(channel + " is now live.")
		streamElementsUrl := fmt.Sprintf("https://api.streamelements.com/kappa/v2/speech?voice=Brian&text=%s", textParam)

		resp, err := http.Get(streamElementsUrl)
		if err != nil {
			log.Printf("Error fetching the URL: %v\n", err)
			return nil
		}
		defer resp.Body.Close()

		time.Sleep(500 * time.Millisecond)

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
			log.Printf("Failed to fetch the URL. HTTP Status: %s\n%s\n", resp.Status, streamElementsUrl)
			return nil
		}

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response body: %v\n", err)
			return nil
		}
		err = os.WriteFile("mp3/"+channel+".mp3", body, 0644)
		if err != nil {
			log.Printf("error: %s\n", err)
		}
		log.Printf("Wrote %s.mp3\n", channel)
		return body
	}
}
