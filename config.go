package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

// Read config file and parse channel=true/false format
func getChannelsFromConfig(filePath string) map[string]bool {
	channels := make(map[string]bool)
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			fmt.Printf("Skipping invalid line: %s\n", line)
			continue
		}

		channel := strings.TrimSpace(parts[0])
		ttsEnabled := strings.TrimSpace(strings.ToLower(parts[1])) == "true"

		channels[channel] = ttsEnabled
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return channels
}
