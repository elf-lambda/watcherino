package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

type TwitchRecorder struct {
	channel       string
	outputDir     string
	streamlinkCmd *exec.Cmd
	ffplayCmd     *exec.Cmd
}

func NewTwitchRecorder(channel, outputDir string) *TwitchRecorder {
	return &TwitchRecorder{
		channel:   channel,
		outputDir: outputDir,
	}
}

func (tr *TwitchRecorder) recordStream() error {
	timestamp := time.Now().Format("2006-01-02_15-04-05")

	channelDir := filepath.Join(tr.outputDir, tr.channel)
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		return err
	}

	filename := filepath.Join(channelDir, tr.channel+"_"+timestamp+".mp4")
	streamURL := "https://twitch.tv/" + tr.channel

	log.Printf("Starting recording: %s", filename)

	cmd := exec.Command("streamlink",
		streamURL,
		"480p,720p,360p,best",
		"-o", filename,
		"--twitch-disable-ads",
	)

	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}
	streamlinkPids = append(streamlinkPids, cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		return err
	}

	log.Printf("Recording saved: %s", filename)
	return nil
}

func (tr *TwitchRecorder) Start() {
	log.Printf("Starting recording for %s...", tr.channel)

	if err := tr.recordStream(); err != nil {
		log.Printf("Recording error: %v", err)
	}

	log.Printf("Recording finished for %s", tr.channel)
}

func (tr *TwitchRecorder) StartAudioOnly(volume int) error {
	streamURL := "https://twitch.tv/" + tr.channel

	tr.streamlinkCmd = exec.Command("streamlink",
		streamURL,
		"audio_only,160p,worst",
		"-o", "-",
		"--twitch-disable-ads",
	)

	tr.ffplayCmd = exec.Command("ffplay",
		"-nodisp",
		"-autoexit",
		"-volume", fmt.Sprintf("%d", volume),
		"-",
	)

	if runtime.GOOS == "windows" {
		tr.streamlinkCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		tr.ffplayCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}

	tr.ffplayCmd.Stdin, _ = tr.streamlinkCmd.StdoutPipe()

	if err := tr.ffplayCmd.Start(); err != nil {
		return err
	}

	if err := tr.streamlinkCmd.Start(); err != nil {
		tr.ffplayCmd.Process.Kill()
		return err
	}

	go func() {
		tr.streamlinkCmd.Wait()
		tr.ffplayCmd.Wait()
	}()

	return nil
}

func (tr *TwitchRecorder) StopAudio() {
	if tr.streamlinkCmd != nil && tr.streamlinkCmd.Process != nil {
		tr.streamlinkCmd.Process.Kill()
	}
	if tr.ffplayCmd != nil && tr.ffplayCmd.Process != nil {
		tr.ffplayCmd.Process.Kill()
	}
}
