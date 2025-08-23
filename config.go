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
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "$") {
			continue
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

// Read Twitch config from file and return TwitchConfig struct
// Errors out if values arent filled
func getTwitchConfigFromFile(filePath string) TwitchConfig {
	config := TwitchConfig{}
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if !strings.HasPrefix(line, "$") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		tmp := make([]string, 0)
		switch key {
		case "$nick":
			config.Nickname = value
		case "$oauth":
			if !strings.HasPrefix(value, "oauth:") {
				config.OauthToken = "oauth:" + value
			} else {
				config.OauthToken = value
			}
		case "$filter":
			tmp = append(tmp, strings.Split(value, ",")...)
			config.FilterList = tmp
		}

	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	if config.Nickname == "" {
		log.Fatal("Missing $nick in config file")
	}
	if config.OauthToken == "" {
		log.Fatal("Missing $oauth in config file")
	}

	return config
}
