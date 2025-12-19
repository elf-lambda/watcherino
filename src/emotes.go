package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/gif"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/draw"
)

// Global emote storage
var (
	channels          = make(map[string]Channel)
	channelsMutex     sync.RWMutex
	global7TVEmotes   = make(map[string]EmoteInfo)
	global7TVMutex    sync.RWMutex
	globalBTTVEmotes  = make(map[string]EmoteInfo)
	globalBTTVMutex   sync.RWMutex
	globalFFZEmotes   = make(map[string]EmoteInfo)
	globalFFZMutex    sync.RWMutex
	channelsBTTV      = make(map[string]map[string]EmoteInfo)
	channelsBTTVMutex sync.RWMutex
	channelsFFZ       = make(map[string]map[string]EmoteInfo)
	channelsFFZMutex  sync.RWMutex
)

// EmoteInfo represents information about an emote
type EmoteInfo struct {
	ID        string
	Name      string
	URL       string
	FilePath  string
	ImageURL  string
	Positions []EmotePosition
}

// EmotePosition represents where an emote appears in a message
type EmotePosition struct {
	Start int
	End   int
}

type Channel struct {
	Name   string
	Emotes map[string]EmoteInfo
}

func findEmote(channelName, word string) (EmoteInfo, bool) {
	channelName = strings.TrimPrefix(channelName, "#")

	// Check channel-specific 7TV emotes
	channelsMutex.RLock()
	if channel, ok := channels[channelName]; ok {
		if e, ok := channel.Emotes[word]; ok {
			channelsMutex.RUnlock()
			return e, true
		}
	}
	channelsMutex.RUnlock()

	// Check global 7TV emotes
	global7TVMutex.RLock()
	if e, ok := global7TVEmotes[word]; ok {
		global7TVMutex.RUnlock()
		return e, true
	}
	global7TVMutex.RUnlock()

	// Check channel-specific BTTV emotes
	channelsBTTVMutex.RLock()
	if channelEmotes, ok := channelsBTTV[channelName]; ok {
		if e, ok := channelEmotes[word]; ok {
			channelsBTTVMutex.RUnlock()
			return e, true
		}
	}
	channelsBTTVMutex.RUnlock()

	// Check global BTTV emotes
	globalBTTVMutex.RLock()
	if e, ok := globalBTTVEmotes[word]; ok {
		globalBTTVMutex.RUnlock()
		return e, true
	}
	globalBTTVMutex.RUnlock()

	// Check channel-specific FFZ emotes
	channelsFFZMutex.RLock()
	if channelEmotes, ok := channelsFFZ[channelName]; ok {
		if e, ok := channelEmotes[word]; ok {
			channelsFFZMutex.RUnlock()
			return e, true
		}
	}
	channelsFFZMutex.RUnlock()

	// Check global FFZ emotes
	globalFFZMutex.RLock()
	if e, ok := globalFFZEmotes[word]; ok {
		globalFFZMutex.RUnlock()
		return e, true
	}
	globalFFZMutex.RUnlock()

	return EmoteInfo{}, false
}

// ParseEmotes extracts emote information from a Twitch message
func ParseEmotes(msg *Message) []EmoteInfo {

	// Parse Twitch emotes first
	var emotes []EmoteInfo
	if emotesTag, exists := msg.Tags["emotes"]; exists && emotesTag != "" {
		contentRunes := []rune(msg.Content)
		emoteGroups := strings.Split(emotesTag, "/")

		for _, group := range emoteGroups {
			parts := strings.Split(group, ":")
			if len(parts) != 2 {
				continue
			}

			emoteID := parts[0]
			for _, posStr := range strings.Split(parts[1], ",") {
				rangeParts := strings.Split(posStr, "-")
				if len(rangeParts) != 2 {
					continue
				}

				start, err1 := strconv.Atoi(rangeParts[0])
				end, err2 := strconv.Atoi(rangeParts[1])
				if err1 != nil || err2 != nil {
					continue
				}

				// Handle UTF-8 boundaries
				if end >= len(contentRunes) {
					continue
				}

				emoteName := string(contentRunes[start : end+1])
				emotes = append(emotes, EmoteInfo{
					ID:   emoteID,
					Name: emoteName,
					URL:  fmt.Sprintf("https://static-cdn.jtvnw.net/emoticons/v2/%s/default/dark/1.0", emoteID),
					Positions: []EmotePosition{{
						Start: start,
						End:   end,
					}},
				})
			}
		}
	}

	// Parse third-party emotes
	runes := []rune(msg.Content)
	current := 0
	covered := make([]bool, len(runes))

	// Mark positions covered by Twitch emotes
	for _, emote := range emotes {
		for _, pos := range emote.Positions {
			for i := pos.Start; i <= pos.End; i++ {
				if i < len(covered) {
					covered[i] = true
				}
			}
		}
	}

	// Find words not covered by Twitch emotes
	for current < len(runes) {
		// Skip spaces
		for current < len(runes) && (runes[current] == ' ' || covered[current]) {
			current++
		}
		if current >= len(runes) {
			break
		}

		// Find word boundaries
		start := current
		for current < len(runes) && runes[current] != ' ' && !covered[current] {
			current++
		}
		end := current - 1

		if start < len(runes) && end >= start {
			word := string(runes[start : end+1])
			if emote, found := findEmote(msg.Channel, word); found {
				emotes = append(emotes, EmoteInfo{
					ID:       emote.ID,
					Name:     word,
					URL:      emote.URL,
					FilePath: emote.FilePath,
					Positions: []EmotePosition{{
						Start: start,
						End:   end,
					}},
				})
			}
		}
	}

	sort.Slice(emotes, func(i, j int) bool {
		return emotes[i].Positions[0].Start < emotes[j].Positions[0].Start
	})

	return emotes
}

// ProcessMessageEmotes processes all emotes in a message
func ProcessMessageEmotes(msg *Message) error {
	emotes := ParseEmotes(msg)
	if len(emotes) == 0 {
		return nil
	}

	for _, emote := range emotes {
		if emote.FilePath == "" {
			go downloadEmote(emote, msg.Channel)
		}
	}

	return nil
}

// Emote downloader
func downloadEmote(emote EmoteInfo, channelName string) {
	channelDir := filepath.Join("channels", strings.TrimPrefix(channelName, "#"))
	emotesDir := filepath.Join(channelDir, "emotes")

	if err := os.MkdirAll(emotesDir, 0755); err != nil {
		log.Printf("Failed to create directories: %v\n", err)
		return
	}

	filename := fmt.Sprintf("%s_%s.png", emote.Name, emote.ID)
	if emote.Name == "" {
		filename = fmt.Sprintf("emote_%s.png", emote.ID)
	}

	filePath := filepath.Join(emotesDir, filename)

	// Skip if already exists
	if _, err := os.Stat(filePath); err == nil {
		emote.FilePath = filePath
		cacheEmote(emote)
		return
	}

	// Download the emote
	resp, err := http.Get(emote.URL)
	if err != nil {
		log.Printf("Failed to download emote %s: %v\n", emote.ID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download emote %s: status %d\n", emote.ID, resp.StatusCode)
		return
	}

	file, err := os.Create(filePath)
	if err != nil {
		log.Printf("Failed to create file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	if _, err = io.Copy(file, resp.Body); err != nil {
		log.Printf("Failed to write emote file %s: %v\n", filePath, err)
		return
	}

	log.Printf("Downloaded emote: %s (%s) -> %s\n", emote.Name, emote.ID, filePath)
	emote.FilePath = filePath
	cacheEmote(emote)
}

// Simple emote cache
var emoteCache = struct {
	sync.RWMutex
	emotes map[string]EmoteInfo
}{emotes: make(map[string]EmoteInfo)}

func cacheEmote(emote EmoteInfo) {
	emoteCache.Lock()
	defer emoteCache.Unlock()
	emoteCache.emotes[emote.ID] = emote
}

func getCachedEmote(emoteID string) (EmoteInfo, bool) {
	emoteCache.RLock()
	defer emoteCache.RUnlock()
	emote, exists := emoteCache.emotes[emoteID]
	return emote, exists
}

// ListEmotesInMessage returns emote information for a specific message
func ListEmotesInMessage(msg *Message) []EmoteInfo {
	return ParseEmotes(msg)
}

// GetCachedEmotes returns all cached emotes
func GetCachedEmotes() map[string]EmoteInfo {
	emoteCache.RLock()
	defer emoteCache.RUnlock()
	result := make(map[string]EmoteInfo)
	for k, v := range emoteCache.emotes {
		result[k] = v
	}
	return result
}

// GetEmoteFilePath returns the local file path for an emote ID
func GetEmoteFilePath(emoteID string) (string, bool) {
	emote, exists := getCachedEmote(emoteID)
	if !exists {
		return "", false
	}
	return emote.FilePath, true
}

// Existing helper functions remain mostly the same
func downloadFirstFrameFromGIF(url, outPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("error downloading gif: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status downloading gif: %d", resp.StatusCode)
	}

	g, err := gif.Decode(resp.Body)
	if err != nil {
		return fmt.Errorf("error decoding gif: %w", err)
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("error creating png file: %w", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, g); err != nil {
		return fmt.Errorf("error encoding png: %w", err)
	}

	return nil
}

func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}
	return resizeImageToMax32(filepath)
}

const MaxEmoteSize = 32

func resizeImageToMax32(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Only resize if height exceeds MaxEmoteSize
	if height <= MaxEmoteSize {
		return nil
	}

	// Calculate scale based only on height
	scale := float64(MaxEmoteSize) / float64(height)
	newWidth := int(float64(width) * scale)
	newHeight := MaxEmoteSize

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	outFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer outFile.Close()

	return png.Encode(outFile, dst)
}

func Fetch7TVEmotes(twitchUserID, channelName string) error {
	url := fmt.Sprintf("https://7tv.io/v3/users/twitch/%s", twitchUserID)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch 7TV emotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("7TV API error: %d", resp.StatusCode)
	}

	var apiResp struct {
		EmoteSet struct {
			Emotes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Data struct {
					Host struct {
						URL   string `json:"url"`
						Files []struct {
							Name   string `json:"name"`
							Format string `json:"format"`
						} `json:"files"`
					} `json:"host"`
				} `json:"data"`
			} `json:"emotes"`
		} `json:"emote_set"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("failed to decode 7TV JSON: %w", err)
	}

	// log.Printf("channel 7tv emotes: %+v\n", apiResp)

	channelDir := filepath.Join("channels", strings.TrimPrefix(channelName, "#"))
	emoteDir := filepath.Join(channelDir, "emotes_7tv")

	if err := os.MkdirAll(emoteDir, 0755); err != nil {
		return fmt.Errorf("failed to create emotes_7tv directory: %w", err)
	}

	normalizedChannelName := strings.TrimPrefix(channelName, "#")
	channelsMutex.Lock()

	// Initialize channel if it doesn't exist
	if _, ok := channels[normalizedChannelName]; !ok {
		channels[normalizedChannelName] = Channel{
			Name:   normalizedChannelName,
			Emotes: make(map[string]EmoteInfo),
		}
	}
	channelsMutex.Unlock()

	for _, emote := range apiResp.EmoteSet.Emotes {
		var imageURL, sourceFormat string

		for _, file := range emote.Data.Host.Files {
			if strings.HasSuffix(file.Name, ".png") {
				imageURL = "https:" + emote.Data.Host.URL + "/" + file.Name
				sourceFormat = "png"

			} else if strings.HasSuffix(file.Name, ".gif") && imageURL == "" {
				imageURL = "https:" + emote.Data.Host.URL + "/" + file.Name
				sourceFormat = "gif"
			}
		}

		if imageURL == "" {
			log.Printf("No PNG or GIF found for emote %s, skipping\n", emote.Name)
			continue
		}

		outputPath := filepath.Join(emoteDir, fmt.Sprintf("%s_%s.png", emote.Name, emote.ID))

		global7TVMutex.RLock()
		defer global7TVMutex.RUnlock()
		// Skip if already exists
		if _, err := os.Stat(outputPath); err == nil {
			channelsMutex.RLock()
			channels[strings.TrimPrefix(channelName, "#")].Emotes[emote.Name] = EmoteInfo{
				ID:       emote.ID,
				Name:     emote.Name,
				ImageURL: imageURL,
				FilePath: outputPath,
			}
			channelsMutex.RUnlock()
			continue
		}

		if sourceFormat == "png" {
			err := downloadFile(imageURL, outputPath)
			if err != nil {
				log.Printf("Failed to download 7TV emote (png) %s: %v\n", emote.Name, err)
				continue
			}
		} else if sourceFormat == "gif" {
			err := downloadFirstFrameFromGIF(imageURL, outputPath)
			if err != nil {
				log.Printf("Failed to convert GIF emote %s: %v\n", emote.Name, err)
				continue
			}
		}

		log.Printf("Downloaded 7TV emote: %s -> %s\n", emote.Name, outputPath)

		channelsMutex.Lock()
		channels[normalizedChannelName].Emotes[emote.Name] = EmoteInfo{
			ID:       emote.ID,
			Name:     emote.Name,
			ImageURL: imageURL,
			FilePath: outputPath,
			URL:      imageURL,
		}
		channelsMutex.Unlock()
	}

	return nil
}

func Fetch7TVGlobalEmotes() error {
	log.Println("inside fetch global")
	log.Println(global7TVEmotes)
	url := "https://7tv.io/v3/emote-sets/global"
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch 7TV global emotes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("7TV global API error: %d", resp.StatusCode)
	}

	var data struct {
		Emotes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Data struct {
				Host struct {
					URL   string `json:"url"`
					Files []struct {
						Name   string `json:"name"`
						Format string `json:"format"`
					} `json:"files"`
				} `json:"host"`
			} `json:"data"`
		} `json:"emotes"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode global emotes JSON: %w", err)
	}

	emoteDir := filepath.Join("channels", "global", "emotes_7tv")
	if err := os.MkdirAll(emoteDir, 0755); err != nil {
		return fmt.Errorf("failed to create global emote directory: %w", err)
	}

	for _, emote := range data.Emotes {
		// Select .png or .gif
		var imageURL, sourceFormat string
		for _, file := range emote.Data.Host.Files {
			if strings.HasSuffix(file.Name, ".png") {
				imageURL = "https:" + emote.Data.Host.URL + "/" + file.Name
				sourceFormat = "png"
				break
			} else if strings.HasSuffix(file.Name, ".gif") && imageURL == "" {
				imageURL = "https:" + emote.Data.Host.URL + "/" + file.Name
				sourceFormat = "gif"
			}
		}

		if imageURL == "" {
			continue
		}

		outputPath := filepath.Join(emoteDir, fmt.Sprintf("%s_%s.png", emote.Name, emote.ID))

		if _, err := os.Stat(outputPath); err == nil {
			global7TVEmotes[emote.Name] = EmoteInfo{
				ID:       emote.ID,
				Name:     emote.Name,
				ImageURL: imageURL,
				FilePath: outputPath,
			}
			continue
		}

		if sourceFormat == "png" {
			_ = downloadFile(imageURL, outputPath)
		} else if sourceFormat == "gif" {
			_ = downloadFirstFrameFromGIF(imageURL, outputPath)
		}

		global7TVEmotes[emote.Name] = EmoteInfo{
			ID:       emote.ID,
			Name:     emote.Name,
			ImageURL: imageURL,
			FilePath: outputPath,
		}
	}

	return nil
}

func FetchBTTVGlobalEmotes() error {
	url := "https://api.betterttv.net/3/cached/emotes/global"
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch BTTV global emotes: %w", err)
	}
	defer resp.Body.Close()

	var emotes []struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&emotes); err != nil {
		return fmt.Errorf("failed to decode BTTV global emotes JSON: %w", err)
	}

	emoteDir := filepath.Join("channels", "global", "emotes_bttv")
	if err := os.MkdirAll(emoteDir, 0755); err != nil {
		return fmt.Errorf("failed to create BTTV global emote directory: %w", err)
	}

	for _, emote := range emotes {
		imageURL := fmt.Sprintf("https://cdn.betterttv.net/emote/%s/3x", emote.ID)
		outputPath := filepath.Join(emoteDir, fmt.Sprintf("%s_%s.png", emote.Code, emote.ID))

		if _, err := os.Stat(outputPath); err != nil {
			if err := downloadFile(imageURL, outputPath); err != nil {
				log.Printf("Failed to download BTTV emote %s: %v\n", emote.Code, err)
				continue
			}
			if err := resizeImageToMax32(outputPath); err != nil {
				log.Printf("Failed to resize BTTV emote %s: %v\n", emote.Code, err)
			}
		}

		globalBTTVEmotes[emote.Code] = EmoteInfo{
			ID:       emote.ID,
			Name:     emote.Code,
			ImageURL: imageURL,
			FilePath: outputPath,
		}
	}
	return nil
}

func FetchBTTVChannelEmotes(channelID, channelName string) error {
	url := fmt.Sprintf("https://api.betterttv.net/3/cached/users/twitch/%s", channelID)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch BTTV emotes for channel %s: %w", channelName, err)
	}
	defer resp.Body.Close()

	var data struct {
		ChannelEmotes []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"channelEmotes"`
		SharedEmotes []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"sharedEmotes"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode BTTV channel emotes JSON: %w", err)
	}

	emoteDir := filepath.Join("channels", strings.TrimPrefix(channelName, "#"), "emotes_bttv")
	if err := os.MkdirAll(emoteDir, 0755); err != nil {
		return fmt.Errorf("failed to create BTTV emote directory: %w", err)
	}

	channelName = strings.TrimPrefix(channelName, "#")
	channelsBTTVMutex.Lock()
	defer channelsBTTVMutex.Unlock()

	// Ensure the channel's emote map exists before we try to add to it
	if _, ok := channelsBTTV[channelName]; !ok {
		channelsBTTV[channelName] = make(map[string]EmoteInfo)
	}

	for _, emote := range append(data.ChannelEmotes, data.SharedEmotes...) {
		imageURL := fmt.Sprintf("https://cdn.betterttv.net/emote/%s/3x", emote.ID)
		outputPath := filepath.Join(emoteDir, fmt.Sprintf("%s_%s.png", emote.Code, emote.ID))

		if _, err := os.Stat(outputPath); err != nil {
			headResp, err := http.Head(imageURL)
			if err != nil {
				log.Printf("Failed HEAD request for %s: %v\n", emote.Code, err)
				continue
			}
			contentType := headResp.Header.Get("Content-Type")
			if strings.Contains(contentType, "gif") {
				err = downloadFirstFrameFromGIF(imageURL, outputPath)
			} else {
				err = downloadFile(imageURL, outputPath)
			}
			if err != nil {
				log.Printf("Failed to download BTTV emote %s: %v\n", emote.Code, err)
				continue
			}

			if err := resizeImageToMax32(outputPath); err != nil {
				log.Printf("Failed to resize BTTV emote %s: %v\n", emote.Code, err)
			}
		}

		// Directly update the global map, which is now locked
		channelsBTTV[channelName][emote.Code] = EmoteInfo{
			ID:       emote.ID,
			Name:     emote.Code,
			ImageURL: imageURL,
			FilePath: outputPath,
		}
	}
	return nil
}

func FetchFFZGlobalEmotes() error {
	url := "https://api.frankerfacez.com/v1/set/global"
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch FFZ global emotes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("FFZ global API error: %d", resp.StatusCode)
	}

	var data struct {
		Sets map[string]struct {
			Emoticons []struct {
				ID     int               `json:"id"`
				Name   string            `json:"name"`
				URLs   map[string]string `json:"urls"`
				Width  int               `json:"width"`
				Height int               `json:"height"`
			} `json:"emoticons"`
		} `json:"sets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode FFZ global emotes JSON: %w", err)
	}

	emoteDir := filepath.Join("channels", "global", "emotes_ffz")
	if err := os.MkdirAll(emoteDir, 0755); err != nil {
		return fmt.Errorf("failed to create FFZ global emote directory: %w", err)
	}

	for _, set := range data.Sets {
		for _, emote := range set.Emoticons {
			// Prefer larger sizes: 4, 2, then 1
			var imageURL string
			if url, ok := emote.URLs["4"]; ok {
				if strings.HasPrefix(url, "//") {
					imageURL = "https:" + url
				} else {
					imageURL = url
				}
			} else if url, ok := emote.URLs["2"]; ok {
				if strings.HasPrefix(url, "//") {
					imageURL = "https:" + url
				} else {
					imageURL = url
				}
			} else if url, ok := emote.URLs["1"]; ok {
				if strings.HasPrefix(url, "//") {
					imageURL = "https:" + url
				} else {
					imageURL = url
				}
			} else {
				log.Printf("No valid URL found for FFZ global emote %s, skipping\n", emote.Name)
				continue
			}

			outputPath := filepath.Join(emoteDir, fmt.Sprintf("%s_%d.png", emote.Name, emote.ID))

			// Skip if already exists
			if _, err := os.Stat(outputPath); err == nil {
				globalFFZEmotes[emote.Name] = EmoteInfo{
					ID:       fmt.Sprintf("%d", emote.ID),
					Name:     emote.Name,
					ImageURL: imageURL,
					FilePath: outputPath,
				}
				continue
			}

			// Download the emote - check if it's a GIF first
			headResp, err := http.Head(imageURL)
			if err != nil {
				log.Printf("Failed HEAD request for FFZ global emote %s: %v\n", emote.Name, err)
				continue
			}
			contentType := headResp.Header.Get("Content-Type")

			if strings.Contains(contentType, "gif") {
				err = downloadFirstFrameFromGIF(imageURL, outputPath)
			} else {
				err = downloadFile(imageURL, outputPath)
			}

			if err != nil {
				log.Printf("Failed to download FFZ global emote %s: %v\n", emote.Name, err)
				continue
			}

			// Resize if needed
			if err := resizeImageToMax32(outputPath); err != nil {
				log.Printf("Failed to resize FFZ global emote %s: %v\n", emote.Name, err)
			}

			log.Printf("Downloaded FFZ global emote: %s -> %s\n", emote.Name, outputPath)

			globalFFZEmotes[emote.Name] = EmoteInfo{
				ID:       fmt.Sprintf("%d", emote.ID),
				Name:     emote.Name,
				ImageURL: imageURL,
				FilePath: outputPath,
			}
		}
	}

	return nil
}

func FetchFFZChannelEmotes(channelID, channelName string) error {
	// FFZ API uses channel name (username) instead of numeric ID
	username := strings.TrimPrefix(channelName, "#")
	log.Printf("Fetching FFZ emotes for channel %s (username: %s)\n", channelName, username)

	url := fmt.Sprintf("https://api.frankerfacez.com/v1/room/%s", username)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch FFZ emotes for channel %s: %w", channelName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		log.Printf("FFZ: Channel %s not found or has no FFZ emotes\n", username)
		return nil // Not an error, just no emotes
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("FFZ channel API returned status %d for channel %s\n", resp.StatusCode, channelName)
		return fmt.Errorf("FFZ channel API error for %s: %d", channelName, resp.StatusCode)
	}

	var data struct {
		Sets map[string]struct {
			Emoticons []struct {
				ID     int               `json:"id"`
				Name   string            `json:"name"`
				URLs   map[string]string `json:"urls"`
				Width  int               `json:"width"`
				Height int               `json:"height"`
			} `json:"emoticons"`
		} `json:"sets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode FFZ channel emotes JSON: %w", err)
	}

	log.Printf("FFZ API returned %d sets for channel %s\n", len(data.Sets), channelName)

	emoteDir := filepath.Join("channels", strings.TrimPrefix(channelName, "#"), "emotes_ffz")
	if err := os.MkdirAll(emoteDir, 0755); err != nil {
		return fmt.Errorf("failed to create FFZ emote directory: %w", err)
	}

	channelName = strings.TrimPrefix(channelName, "#")
	channelsFFZMutex.Lock()
	defer channelsFFZMutex.Unlock()

	// Ensure the channel's emote map exists before we try to add to it
	if _, ok := channelsFFZ[channelName]; !ok {
		channelsFFZ[channelName] = make(map[string]EmoteInfo)
	}

	emoteCount := 0
	for _, set := range data.Sets {
		log.Printf("Processing FFZ set with %d emoticons\n", len(set.Emoticons))
		for _, emote := range set.Emoticons {
			emoteCount++
			// Prefer larger sizes: 4, 2, then 1
			var imageURL string
			if url, ok := emote.URLs["4"]; ok {
				if strings.HasPrefix(url, "//") {
					imageURL = "https:" + url
				} else {
					imageURL = url
				}
			} else if url, ok := emote.URLs["2"]; ok {
				if strings.HasPrefix(url, "//") {
					imageURL = "https:" + url
				} else {
					imageURL = url
				}
			} else if url, ok := emote.URLs["1"]; ok {
				if strings.HasPrefix(url, "//") {
					imageURL = "https:" + url
				} else {
					imageURL = url
				}
			} else {
				log.Printf("No valid URL found for FFZ emote %s, skipping\n", emote.Name)
				continue
			}

			outputPath := filepath.Join(emoteDir, fmt.Sprintf("%s_%d.png", emote.Name, emote.ID))

			// Skip if already exists
			if _, err := os.Stat(outputPath); err == nil {
				channelsFFZ[channelName][emote.Name] = EmoteInfo{
					ID:       fmt.Sprintf("%d", emote.ID),
					Name:     emote.Name,
					ImageURL: imageURL,
					FilePath: outputPath,
				}
				continue
			}

			// Download the emote - check if it's a GIF first
			headResp, err := http.Head(imageURL)
			if err != nil {
				log.Printf("Failed HEAD request for FFZ emote %s: %v\n", emote.Name, err)
				continue
			}
			contentType := headResp.Header.Get("Content-Type")

			if strings.Contains(contentType, "gif") {
				err = downloadFirstFrameFromGIF(imageURL, outputPath)
			} else {
				err = downloadFile(imageURL, outputPath)
			}

			if err != nil {
				log.Printf("Failed to download FFZ emote %s: %v\n", emote.Name, err)
				continue
			}

			// Resize if needed
			if err := resizeImageToMax32(outputPath); err != nil {
				log.Printf("Failed to resize FFZ emote %s: %v\n", emote.Name, err)
			}

			log.Printf("Downloaded FFZ emote: %s -> %s\n", emote.Name, outputPath)

			channelsFFZ[channelName][emote.Name] = EmoteInfo{
				ID:       fmt.Sprintf("%d", emote.ID),
				Name:     emote.Name,
				ImageURL: imageURL,
				FilePath: outputPath,
			}
		}
	}

	log.Printf("Processed %d FFZ emotes for channel %s\n", emoteCount, channelName)
	return nil
}
