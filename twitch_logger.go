package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func createFileForChannel(channel string) *os.File {
	t := time.Now()
	formatted := fmt.Sprintf("%d-%02d-%02d",
		t.Year(), t.Month(), t.Day())

	dir := filepath.Join("logs", channel)
	filepath := filepath.Join(dir, formatted+"_log.txt")

	os.MkdirAll(dir, 0700)
	f, err := os.OpenFile(filepath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	log.Printf("Created log file for %s with path %s", channel, filepath)
	return f
}
