package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// a.channels normal, a.connections -> # obviously

type TwitchConfig struct {
	Nickname         string `json:"nickname"`
	OauthToken       string `json:"oauthToken"`
	FilterList       []string
	RecordingEnabled bool
	ArchiveDir       string
	TTSPath          string
	TTSMessage       string
}

// ChannelConnection represents a connection to a single Twitch channel
type ChannelConnection struct {
	channel     string
	client      *Client
	cancel      context.CancelFunc
	messages    []map[string]interface{}
	viewerCount int
	isConnected bool
	mu          sync.RWMutex
}

// App represents the app state with all channels and connections
type App struct {
	ctx           context.Context
	channels      []string
	activeChannel string
	connections   map[string]*ChannelConnection // channel -> connection
	connectionsMu sync.RWMutex

	liveStatuses   map[string]bool
	statusTicker   *time.Ticker
	stopMonitoring chan bool
}

func NewApp() *App {
	channels := make([]string, 0)
	// TODO Add tts on/off
	for x, _ := range channels_map {
		channels = append(channels, x)
	}

	return &App{
		channels:       channels,
		connections:    make(map[string]*ChannelConnection),
		liveStatuses:   make(map[string]bool),
		stopMonitoring: make(chan bool),
	}
}

func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
	go func() {
		log.Printf("Waiting 2 more seconds for live status checks...")
		time.Sleep(2 * time.Second)

		log.Printf("Auto-connecting to all channels...")
		if err := a.ConnectToAllChannels(); err != nil {
			log.Printf("Auto-connection errors: %v", err)
		} else {
			log.Printf("Auto-connection completed successfully")
		}
		log.Printf("Waiting 5 seconds for frontend to initialize...")
		time.Sleep(2 * time.Second)

		log.Printf("Starting live status monitoring...")
		go a.startLiveStatusMonitoring()

	}()
}

func (a *App) ConnectToAllChannels() error {
	log.Printf("ConnectToAllChannels called - connecting to %d channels...", len(a.channels))

	if len(a.channels) == 0 {
		log.Printf("No channels configured, skipping auto-connect")
		return nil
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(a.channels))
	successes := make(chan string, len(a.channels))

	for i, channel := range a.channels {
		log.Printf("Starting connection to channel %d/%d: %s", i+1, len(a.channels), channel)

		wg.Add(1)
		go func(ch string, index int) {
			defer wg.Done()

			log.Printf("Connecting to %s (goroutine %d)...", ch, index+1)

			if err := a.ConnectToChannel(ch); err != nil {
				log.Printf("Failed to auto-connect to %s: %v", ch, err)
				errors <- fmt.Errorf("failed to connect to %s: %w", ch, err)
				return
			}

			log.Printf("Successfully auto-connected to channel: %s", ch)
			successes <- ch
		}(channel, i)

		if i < len(a.channels)-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	log.Printf("Waiting for all %d connection attempts to complete...", len(a.channels))

	// Wait for all connections to complete
	go func() {
		wg.Wait()
		close(errors)
		close(successes)
		log.Printf("All connection attempts finished")
	}()

	var connectionErrors []string
	var successfulConnections []string

	// Read from both channels until they're closed
	errChan := errors
	sucChan := successes

	for errChan != nil || sucChan != nil {
		select {
		case err, ok := <-errChan:
			if !ok {
				errChan = nil
			} else {
				connectionErrors = append(connectionErrors, err.Error())
			}
		case success, ok := <-sucChan:
			if !ok {
				sucChan = nil
			} else {
				successfulConnections = append(successfulConnections, success)
			}
		}
	}

	log.Printf("-> Auto-connection results:")
	log.Printf("   Successful: %d channels - %v", len(successfulConnections), successfulConnections)
	log.Printf("   Failed: %d channels - %v", len(connectionErrors), connectionErrors)

	if len(connectionErrors) > 0 && len(successfulConnections) == 0 {
		return fmt.Errorf("all connections failed: %v", connectionErrors)
	} else if len(connectionErrors) > 0 {
		// TODO redo
		log.Printf("Some connections failed, but %d succeeded", len(successfulConnections))
	} else {
		log.Printf("All channels connected successfully!")
	}

	return nil
}

func (a *App) ConnectToChannel(channel string) error {
	originalChannel := channel

	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	log.Printf("ConnectToChannel called: '%s' -> '%s'", originalChannel, channel)

	a.connectionsMu.Lock()

	if conn, exists := a.connections[channel]; exists && conn.isConnected {
		log.Printf("Channel %s already connected, switching to it", channel)
		// just switch to this channel
		a.activeChannel = channel
		a.connectionsMu.Unlock()

		runtime.EventsEmit(a.ctx, "channel-switched", channel)
		a.emitRecentMessages(channel)
		return nil
	}

	log.Printf("Creating new connection for %s", channel)
	conn := &ChannelConnection{
		channel:     channel,
		messages:    make([]map[string]interface{}, 0, bufferSize),
		isConnected: false,
	}

	log.Printf("Creating client for %s", channel)
	conn.client = NewClient(channel, bufferSize)

	log.Printf("Attempting IRC connection to %s", channel)
	if err := conn.client.Connect(); err != nil {
		a.connectionsMu.Unlock()
		log.Printf("IRC connection failed for %s: %v", channel, err)
		return fmt.Errorf("failed to connect to %s: %w", channel, err)
	}

	log.Printf("Starting client for %s", channel)
	conn.client.Start()
	conn.isConnected = true

	ctx, cancel := context.WithCancel(context.Background())
	conn.cancel = cancel

	a.connections[channel] = conn

	if a.activeChannel == "" {
		log.Printf("Setting %s as active channel", channel)
		a.activeChannel = channel
	}

	a.connectionsMu.Unlock()

	log.Printf("Starting message forwarding for %s", channel)
	go a.forwardMessages(ctx, conn)

	log.Printf("Starting viewer count monitoring for %s", channel)
	go a.monitorViewerCount(ctx, conn)

	log.Printf("Successfully connected to channel: %s", channel)
	runtime.EventsEmit(a.ctx, "channel-connected", channel)

	return nil
}

// forwardMessages handles messages for the active channel
func (a *App) forwardMessages(ctx context.Context, conn *ChannelConnection) {
	if conn == nil || conn.client == nil {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("forwardMessages recovered from panic for %s: %v", conn.channel, r)
		}
	}()

	var firstRun bool = true

	for {
		select {
		case <-ctx.Done():
			log.Printf("Message forwarding cancelled for %s", conn.channel)
			return

		case msg, ok := <-conn.client.MessageChannel():
			if !ok {
				log.Printf("Message channel closed for %s", conn.channel)
				return
			}

			if err := ProcessMessageEmotes(&msg); err != nil {
				log.Printf("Error processing emotes: %v\n", err)
			}

			// only fetch emotes when the first message is being received
			// i'm trying to avoid pointless grabs on inactive/less active channels
			if firstRun {
				channels[strings.TrimPrefix(conn.client.channel, "#")] = Channel{
					Name:   conn.client.channel,
					Emotes: make(map[string]EmoteInfo),
				}

				channelID := msg.GetRoomID()
				if channelID != "" {
					go Fetch7TVEmotes(channelID, conn.client.channel)
					go FetchBTTVChannelEmotes(channelID, conn.client.channel)
					go FetchFFZChannelEmotes(channelID, conn.client.channel)
					firstRun = false
				}
			}

			emotes := ParseEmotes(&msg)
			emoteInfo := make(map[string]string)
			for _, emote := range emotes {
				base64, err := a.GetEmoteBase64(emote.FilePath, emote, &msg)
				if err != nil {
					log.Printf("Error encoding emote: %v", err)
					continue
				}
				emoteInfo[emote.Name] = base64
			}

			msgData := map[string]interface{}{
				"username":      msg.Username,
				"content":       msg.Content,
				"channel":       msg.Channel,
				"timestamp":     msg.Timestamp.Format("15:04:05"),
				"userColor":     msg.UserColor,
				"emotes":        emoteInfo,
				"isHighlighted": false,
			}

			channelToLog := strings.TrimPrefix(conn.client.channel, "#")
			file, ok := loggerList[channelToLog]
			if !ok {
				// new
				file = createFileForChannel(channelToLog)
				loggerList[channelToLog] = file
			}
			fmt.Fprintf(file, "[%s] %s: %s\n", msg.Timestamp.Format("15:04:05"),
				msg.Username, msg.Content)
			file.Sync()

			conn.mu.Lock()
			conn.messages = append(conn.messages, msgData)
			if len(conn.messages) > bufferSize {
				conn.messages = conn.messages[1:] // Remove oldest
			}
			conn.mu.Unlock()

			a.connectionsMu.RLock()
			isActive := (a.activeChannel == conn.channel)
			a.connectionsMu.RUnlock()

			if containsAny(msg.Content, filterList) {
				msgData["isHighlighted"] = true
				go playMp3(otoCtx, getMp3ForChannel("ding"), 0.10)
			}

			if isActive {
				runtime.EventsEmit(a.ctx, "new-message", msgData)
			} else if !isActive && msgData["isHighlighted"] == true {
				runtime.EventsEmit(a.ctx, "highlight-channel", msgData)
			}

		case reward, ok := <-conn.client.RewardChannel():
			if !ok {
				log.Printf("Reward channel closed for %s", conn.channel)
				return
			}

			rewardData := map[string]interface{}{
				"username":   reward.Username,
				"rewardName": reward.RewardName,
				"userInput":  reward.UserInput,
				"timestamp":  reward.Timestamp.Format("15:04:05"),
				"rawData":    reward.RawData,
				"channel":    conn.channel,
			}

			// Only emit if this is the active channel
			a.connectionsMu.RLock()
			isActive := (a.activeChannel == conn.channel)
			a.connectionsMu.RUnlock()

			if isActive {
				runtime.EventsEmit(a.ctx, "reward-redemption", rewardData)
			}

		case err, ok := <-conn.client.ErrorChannel():
			if !ok {
				log.Printf("Error channel closed for %s", conn.channel)
				return
			}

			log.Printf("Twitch client error for %s: %v", conn.channel, err)
			runtime.EventsEmit(a.ctx, "connection-error", map[string]interface{}{
				"channel": conn.channel,
				"error":   err.Error(),
			})
			return
		}
	}
}

// monitorViewerCount monitors viewer count for a specific channel
func (a *App) monitorViewerCount(ctx context.Context, conn *ChannelConnection) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := a.GetViewerCount(conn.channel)
			if err == nil {
				conn.mu.Lock()
				conn.viewerCount = count
				conn.mu.Unlock()

				// Only emit if this is the active channel
				a.connectionsMu.RLock()
				isActive := (a.activeChannel == conn.channel)
				a.connectionsMu.RUnlock()

				if isActive {
					runtime.EventsEmit(a.ctx, "viewer-count", count)
				}
			}
		}
	}
}

func (a *App) SwitchToChannel(channel string) error {
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	a.connectionsMu.Lock()

	// Connect if it doesnt exist/disconnected
	conn, exists := a.connections[channel]
	if !exists || !conn.isConnected {
		a.connectionsMu.Unlock()
		return a.ConnectToChannel(channel)
	}

	a.activeChannel = channel
	a.connectionsMu.Unlock()

	a.emitRecentMessages(channel)

	conn.mu.RLock()
	viewerCount := conn.viewerCount
	conn.mu.RUnlock()

	if !audioLocked {
		if audioMuted {
			audioRecorder.StopAudio()
		}
		audioRecorder.channel = strings.TrimPrefix(channel, "#")
		isLive := a.checkStreamStatus(strings.TrimPrefix(channel, "#"))
		if !audioMuted && isLive {
			go func() {
				audioRecorder.StopAudio()
				audioRecorder.StartAudioOnly(10)
			}()
		}
	}

	runtime.EventsEmit(a.ctx, "viewer-count", viewerCount)
	runtime.EventsEmit(a.ctx, "channel-switched", channel)

	return nil
}

func (a *App) ToggleAudioMute() bool {
	audioMuted = !audioMuted
	if audioMuted {
		audioRecorder.StopAudio()
	} else {
		// Restart audio for current audio channel (respects lock)
		if audioRecorder.channel != "" && audioRecorder.channel != "none" {
			channel := audioRecorder.channel
			if a.checkStreamStatus(channel) {
				go audioRecorder.StartAudioOnly(10)
			}
		}
	}
	return audioMuted
}

func (a *App) SetAudioLock(locked bool) {
	audioLocked = locked
}

func (a *App) emitRecentMessages(channel string) {
	conn, exists := a.connections[channel]
	if !exists {
		return
	}

	conn.mu.RLock()
	messages := make([]map[string]interface{}, len(conn.messages))
	copy(messages, conn.messages)
	conn.mu.RUnlock()

	runtime.EventsEmit(a.ctx, "channel-messages", map[string]interface{}{
		"channel":  channel,
		"messages": messages,
	})
}

func (a *App) DisconnectFromChannel(channel string) error {
	log.Printf("DisconnectFromChannel called for: %s", channel)

	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	a.connectionsMu.Lock()
	defer a.connectionsMu.Unlock()

	conn, exists := a.connections[channel]
	if !exists {
		log.Printf("Channel %s not found in connections", channel)
		return fmt.Errorf("not connected to channel: %s", channel)
	}

	log.Printf("Stopping connection for %s...", channel)

	if conn.cancel != nil {
		log.Printf("Cancelling context for %s", channel)
		conn.cancel()
	}

	if conn.client != nil {
		log.Printf("Stopping client for %s", channel)
		conn.client.Stop()
	}

	conn.isConnected = false
	delete(a.connections, channel)
	log.Printf("Removed %s from connections map", channel)

	if a.activeChannel == channel {
		log.Printf("%s was active channel, clearing active channel", channel)
		a.activeChannel = ""
		runtime.EventsEmit(a.ctx, "active-channel-disconnected", channel)
	}

	log.Printf("Successfully disconnected from %s", channel)
	runtime.EventsEmit(a.ctx, "channel-disconnected", channel)
	return nil
}

// Currently pointless
func (a *App) DisconnectFromAllChannels() {
	a.connectionsMu.Lock()
	defer a.connectionsMu.Unlock()

	for channel, conn := range a.connections {
		if conn.cancel != nil {
			conn.cancel()
		}
		if conn.client != nil {
			conn.client.Stop()
		}
		log.Printf("Disconnected from %s", channel)
	}

	a.connections = make(map[string]*ChannelConnection)
	a.activeChannel = ""
	runtime.EventsEmit(a.ctx, "all-channels-disconnected", nil)
}

// Unused atm
func (a *App) GetConnectedChannels() []string {
	a.connectionsMu.RLock()
	defer a.connectionsMu.RUnlock()

	connected := make([]string, 0, len(a.connections))
	for channel, conn := range a.connections {
		if conn.isConnected {
			connected = append(connected, channel)
		}
	}
	return connected
}

// Unused atm
func (a *App) GetRecentMessages(channel string, count int) []map[string]interface{} {
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}

	a.connectionsMu.RLock()
	conn, exists := a.connections[channel]
	a.connectionsMu.RUnlock()

	if !exists {
		return []map[string]interface{}{}
	}

	conn.mu.RLock()
	defer conn.mu.RUnlock()

	start := len(conn.messages) - count
	if start < 0 {
		start = 0
	}

	return conn.messages[start:]
}

func (a *App) GetChannels() []string {
	a.connectionsMu.RLock()
	defer a.connectionsMu.RUnlock()
	return a.channels
}

func (a *App) AddChannel(channel string) {
	a.connectionsMu.RLock()
	// defer a.connectionsMu.RUnlock()
	// Just in case
	channel = strings.TrimPrefix(channel, "#")
	for _, ch := range a.channels {
		if ch == channel {
			return
		}
	}
	// TTS
	isLive := a.checkStreamStatus(channel)
	if isLive {
		mp3File := getMp3ForChannel(channel)
		go playMp3(otoCtx, mp3File, 0.10)
		log.Println("Starting archiving for ", channel)
		go func(ch string) {
			if toRecord {
				recorder := NewTwitchRecorder(ch, archiveDir)
				recorder.Start()
			}
		}(channel)
	}
	a.channels = append(a.channels, channel)
	a.liveStatuses[channel] = isLive

	a.connectionsMu.RUnlock()

	a.ConnectToChannel(channel)

	runtime.EventsEmit(a.ctx, "channel-live-status", map[string]interface{}{
		"channel": channel,
		"isLive":  isLive,
	})
}

func (a *App) RemoveChannel(channel string) {
	log.Printf("RemoveChannel called for: %s", channel)

	normalizedChannel := channel
	if !strings.HasPrefix(channel, "#") {
		normalizedChannel = "#" + channel
	}

	log.Printf("Disconnecting from channel if connected...")
	if err := a.DisconnectFromChannel(normalizedChannel); err != nil {
		log.Printf("Error disconnecting from %s: %v", normalizedChannel, err)
	}

	log.Printf("Removing from channels list...")

	originalChannelCount := len(a.channels)

	for i, ch := range a.channels {
		if ch == channel {
			log.Printf("Found channel %s at index %d, removing...", channel, i)
			a.channels = append(a.channels[:i], a.channels[i+1:]...)
			break
		}
	}

	newChannelCount := len(a.channels)
	log.Printf("Channel count: %d -> %d", originalChannelCount, newChannelCount)

	a.connectionsMu.Lock()
	if _, exists := a.liveStatuses[channel]; exists {
		delete(a.liveStatuses, channel)
		log.Printf("Cleaned up live status for %s", channel)
	}
	a.connectionsMu.Unlock()

	log.Printf("Successfully removed channel: %s", channel)

	runtime.EventsEmit(a.ctx, "channel-removed", channel)
}

func (a *App) GetActiveChannel() string {
	a.connectionsMu.RLock()
	defer a.connectionsMu.RUnlock()
	return a.activeChannel
}

func (a *App) GetCurrentViewerCount() int {
	a.connectionsMu.RLock()
	defer a.connectionsMu.RUnlock()

	if a.activeChannel == "" {
		return 0
	}

	if conn, exists := a.connections[a.activeChannel]; exists {
		conn.mu.RLock()
		defer conn.mu.RUnlock()
		return conn.viewerCount
	}
	return 0
}

func (a *App) GetEmoteBase64(filePath string, emote EmoteInfo, msg *Message) (string, error) {
	// log.Println("get emote for", filePath, "\nemote: ", emote)

	if strings.HasPrefix(emote.URL, "https://static-cdn.jtvnw.net") {
		// return filepath.ToSlash(emote.FilePath), nil
		tmp := fmt.Sprintf("%s_%s.png", emote.Name, emote.ID)
		filePath = filepath.Join("channels", strings.TrimPrefix(msg.Channel, "#"), "emotes", tmp)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("error reading emote file: %v", err)
	}

	contentType := "image/png"
	// if strings.HasSuffix(filePath, ".gif") {
	// 	contentType = "image/gif"
	// }

	// Lol
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", contentType, encoded), nil
}

func (a *App) GetViewerCount(channel string) (int, error) {
	channel = strings.TrimPrefix(channel, "#")

	url := "https://gql.twitch.tv/gql"
	query := fmt.Sprintf(`{"query":"query { user(login:\"%s\") { stream { viewersCount } } }"}`, channel)

	req, err := http.NewRequest("POST", url, strings.NewReader(query))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Client-ID", "kimne78kx3ncx6brgo4mv6wki5h1ko")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			User struct {
				Stream struct {
					ViewersCount int `json:"viewersCount"`
				} `json:"stream"`
			} `json:"user"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.Data.User.Stream.ViewersCount, nil
}

func (a *App) checkStreamStatus(channel string) bool {
	channel = strings.TrimPrefix(channel, "#")
	url := "https://gql.twitch.tv/gql"
	query := fmt.Sprintf(`{"query":"query { user(login:\"%s\") { stream { id } } }"}`, channel)

	req, err := http.NewRequest("POST", url, strings.NewReader(query))
	if err != nil {
		log.Printf("Error creating request for %s: %v", channel, err)
		return false
	}

	req.Header.Set("Client-ID", "kimne78kx3ncx6brgo4mv6wki5h1ko")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error checking stream status for %s: %v", channel, err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			User struct {
				Stream *struct {
					ID string `json:"id"`
				} `json:"stream"`
			} `json:"user"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decoding response for %s: %v", channel, err)
		return false
	}

	isLive := result.Data.User.Stream != nil
	log.Printf("Checking %s via GraphQL -> Live: %t", channel, isLive)
	return isLive
}

// func (a *App) checkStreamStatus(channel string) bool {
// 	channel = strings.TrimPrefix(channel, "#")

// 	timestamp := time.Now().Unix()
// 	url := fmt.Sprintf("https://static-cdn.jtvnw.net/previews-ttv/live_user_%s-320x180.jpg?timestamp=%d", channel, timestamp)

// 	client := &http.Client{
// 		Timeout: 10 * time.Second,
// 	}

// 	resp, err := client.Get(url)
// 	if err != nil {
// 		log.Printf("Error checking stream status for %s: %v", channel, err)
// 		return false
// 	}
// 	defer resp.Body.Close()

// 	finalURL := resp.Request.URL.String()
// 	isLive := !strings.Contains(finalURL, "404_preview")

// 	log.Printf("Checking %s: %s -> Live: %t", channel, finalURL, isLive)
// 	return isLive
// }

func (a *App) startLiveStatusMonitoring() {
	log.Printf("Starting live status monitoring for %d channels", len(a.channels))

	// Initial check for all channels
	for _, channel := range a.channels {
		// go func(ch string) {
		isLive := a.checkStreamStatus(channel)
		if isLive {
			log.Printf("Initial check for channel: %s", channel)
		}

		mp3File := getMp3ForChannel(channel)

		func() {
			a.connectionsMu.Lock()
			defer a.connectionsMu.Unlock()
			a.liveStatuses[channel] = isLive
		}()

		if isLive {
			playMp3(otoCtx, mp3File, 0.10)
			log.Println("Starting archiving for ", channel)

			go func(ch string) {
				if toRecord && channels_map[channel] {
					recorder := NewTwitchRecorder(ch, archiveDir)
					recorder.Start()
				}
			}(channel)
		}
		runtime.EventsEmit(a.ctx, "channel-live-status", map[string]interface{}{
			"channel": channel,
			"isLive":  isLive,
		})

		log.Printf("Channel %s initial status: %t", channel, isLive)

		time.Sleep(50 * time.Millisecond)
		// }(channel)
	}

	// Ticker for periodic checks
	a.statusTicker = time.NewTicker(2 * time.Minute)

	log.Printf("Live status monitoring started, checking every 2 minutes")

	for {
		select {
		case <-a.statusTicker.C:
			log.Printf("Periodic live status check...")
			a.checkAllChannelsStatus()
		case <-a.stopMonitoring:
			log.Printf("Stopping live status monitoring")
			if a.statusTicker != nil {
				a.statusTicker.Stop()
			}
			return
		case <-a.ctx.Done():
			log.Printf("Context done, stopping live status monitoring")
			if a.statusTicker != nil {
				a.statusTicker.Stop()
			}
			return
		}
	}
}

// Check all channels and emit updates when status changes
func (a *App) checkAllChannelsStatus() {
	for _, channel := range a.channels {
		currentStatus := a.checkStreamStatus(channel)

		a.connectionsMu.Lock()
		previousStatus, exists := a.liveStatuses[channel]

		// If status changed or first check for this channel
		if !exists || previousStatus != currentStatus {
			log.Println(a.liveStatuses)
			a.liveStatuses[channel] = currentStatus
			a.connectionsMu.Unlock()

			if currentStatus {
				// play mp3
				mp3File := getMp3ForChannel(channel)
				playMp3(otoCtx, mp3File, 0.10)
				log.Println("Starting archiving for ", channel)

				go func(ch string) {
					if toRecord && channels_map[channel] {
						recorder := NewTwitchRecorder(ch, archiveDir)
						recorder.Start()
					}
				}(channel)
			}

			runtime.EventsEmit(a.ctx, "channel-live-status", map[string]interface{}{
				"channel": channel,
				"isLive":  currentStatus,
			})

			log.Printf("Channel %s status changed: %t -> %t", channel, previousStatus, currentStatus)
		} else {
			a.connectionsMu.Unlock()
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func (a *App) GetChannelLiveStatus(channel string) bool {
	a.connectionsMu.RLock()
	defer a.connectionsMu.RUnlock()
	// log.Printf("%s is %t\n", channel, a.liveStatuses[channel])
	// log.Println(a.liveStatuses)
	return a.liveStatuses[strings.TrimPrefix(channel, "#")]
}

// For future use maybe
func (a *App) OnBeforeClose(ctx context.Context) bool {
	a.DisconnectFromAllChannels()
	if a.stopMonitoring != nil {
		close(a.stopMonitoring)
	}
	return false
}

func (a *App) GetBufferSize() int {
	return bufferSize
}

func (a *App) GetTwitchConfig() TwitchConfig {
	return GetTwitchConfigFromFile("config.txt")
}
