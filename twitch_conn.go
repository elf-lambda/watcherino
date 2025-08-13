package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TODO: Add automatic / disconnect / reconnect

// Message represents a parsed Twitch chat message
type Message struct {
	Username  string
	Content   string
	Channel   string
	Tags      map[string]string
	RawData   string
	Timestamp time.Time
	Height    int
	UserColor string
}

func (msg *Message) GetRoomID() string {
	if id, ok := msg.Tags["room-id"]; ok {
		return id
	}
	return ""
}

// RewardRedemption represents a channel point redemption
type RewardRedemption struct {
	RewardID   string
	Username   string
	RewardName string
	UserInput  string
	RawData    string
	Timestamp  time.Time
}

// RingBuffer holds the last N messages
type RingBuffer struct {
	messages []Message
	size     int
	index    int
	mu       sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with specified size
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		messages: make([]Message, size),
		size:     size,
		index:    0,
	}
}

// Add adds a message to the ring buffer
func (rb *RingBuffer) Add(msg Message) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.messages[rb.index] = msg
	rb.index = (rb.index + 1) % rb.size
}

// GetAll returns all messages in chronological order (oldest first)
func (rb *RingBuffer) GetAll() []Message {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]Message, 0, rb.size)

	// Start from the oldest message
	for i := 0; i < rb.size; i++ {
		idx := (rb.index + i) % rb.size
		if !rb.messages[idx].Timestamp.IsZero() {
			result = append(result, rb.messages[idx])
		}
	}

	return result
}

// GetLast returns the last N messages
func (rb *RingBuffer) GetLast(n int) []Message {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.size {
		n = rb.size
	}

	result := make([]Message, 0, n)

	// Get the last n messages
	for i := n - 1; i >= 0; i-- {
		idx := (rb.index - 1 - i + rb.size) % rb.size
		if !rb.messages[idx].Timestamp.IsZero() {
			result = append([]Message{rb.messages[idx]}, result...)
		}
	}

	return result
}

// Client represents a Twitch IRC client
type Client struct {
	conn          net.Conn
	username      string
	channel       string
	messageBuffer *RingBuffer
	rewardChan    chan RewardRedemption
	messageChan   chan Message
	errorChan     chan error
	stopChan      chan struct{}
	mu            sync.RWMutex
	connected     bool
	stopped       bool
}

// NewClient creates a new Twitch IRC client
func NewClient(channel string, bufferSize int) *Client {
	return &Client{
		channel:       channel,
		messageBuffer: NewRingBuffer(bufferSize),
		rewardChan:    make(chan RewardRedemption, 10),
		messageChan:   make(chan Message, 10),
		errorChan:     make(chan error, 10),
		stopChan:      make(chan struct{}),
		stopped:       false,
	}
}

// Establishe connection to Twitch IRC
func (c *Client) Connect() error {
	server := "irc.chat.twitch.tv"
	port := 6667

	c.username = fmt.Sprintf("justinfan%d", rand.Intn(9999-1000)+1000)

	conn, err := net.Dial("tcp", net.JoinHostPort(server, fmt.Sprintf("%d", port)))
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn

	fmt.Fprintf(conn, "NICK %s\r\n", c.username)
	fmt.Fprintf(conn, "JOIN %s\r\n", c.channel)
	fmt.Fprintf(conn, "CAP REQ :twitch.tv/tags twitch.tv/commands\r\n")

	c.mu.Lock()
	c.connected = true
	c.stopped = false
	c.mu.Unlock()

	return nil
}

// Begin listening for messages in a goroutine
func (c *Client) Start() {
	go c.listen()
}

// Safe send functions to prevent panic on closed channels
func (c *Client) safeSendMessage(msg Message) bool {
	c.mu.RLock()
	stopped := c.stopped
	c.mu.RUnlock()

	if stopped {
		return false
	}

	select {
	case c.messageChan <- msg:
		return true
	case <-c.stopChan:
		return false
	default:
		return false // Channel full
	}
}

func (c *Client) safeSendReward(reward RewardRedemption) bool {
	c.mu.RLock()
	stopped := c.stopped
	c.mu.RUnlock()

	if stopped {
		return false
	}

	select {
	case c.rewardChan <- reward:
		return true
	case <-c.stopChan:
		return false
	default:
		return false // Channel full
	}
}

func (c *Client) safeSendError(err error) bool {
	c.mu.RLock()
	stopped := c.stopped
	c.mu.RUnlock()

	if stopped {
		return false
	}

	select {
	case c.errorChan <- err:
		return true
	case <-c.stopChan:
		return false
	default:
		return false // Channel full
	}
}

// Listen for messages from the socket
func (c *Client) listen() {
	defer func() {
		c.mu.Lock()
		c.connected = false
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()
	}()

	scanner := bufio.NewScanner(c.conn)

	for {
		select {
		case <-c.stopChan:
			return
		default:
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					c.safeSendError(fmt.Errorf("scanner error: %w", err))
				}
				return
			}

			data := scanner.Text()
			// fmt.Println(data)
			if data == "PING :tmi.twitch.tv" {
				log.Printf("GOT A PING -> SENT A PONG for channel: %s\n", c.channel)
				fmt.Fprintln(c.conn, "PONG :tmi.twitch.tv")
				continue
			}

			// TODO fix bug
			if strings.Contains(data, "custom-reward-id=") {
				reward := c.parseRewardRedemption(data)
				if reward != nil {
					c.safeSendReward(*reward)
				}
			}

			if strings.Contains(data, "PRIVMSG") {
				msg := c.parsePrivMsg(data)
				if msg != nil {
					c.messageBuffer.Add(*msg)
					c.safeSendMessage(*msg)
				}
			}
		}
	}
}

// Parse the received message
func (c *Client) parsePrivMsg(data string) *Message {
	// Example PRIVMSG format:
	// @badge-info=;badges=;client-nonce=...;display-name=Username;emotes=;first-msg=0;flags=;id=...;mod=0;returning-chatter=0;room-id=...;subscriber=0;tmi-sent-ts=...;turbo=0;user-id=...;user-type= :username!username@username.tmi.twitch.tv PRIVMSG #channel :Hello world!

	parts := strings.Split(data, " ")
	if len(parts) < 4 {
		return nil
	}

	msg := &Message{
		RawData:   data,
		Timestamp: time.Now(),
		Tags:      make(map[string]string),
	}

	// Parse tags if they exist
	// if strings.HasPrefix(data, "@") {
	// 	tagEnd := strings.Index(data, " :")
	// 	if tagEnd != -1 {
	// 		tagStr := data[1:tagEnd]
	// 		tags := strings.Split(tagStr, ";")
	// 		for _, tag := range tags {
	// 			if kv := strings.SplitN(tag, "=", 2); len(kv) == 2 {
	// 				msg.Tags[kv[0]] = kv[1]
	// 			}
	// 		}
	// 	}
	// }
	if strings.HasPrefix(data, "@") {
		// Find the first space after tags, not " :"
		tagEnd := strings.Index(data[1:], " ")
		if tagEnd != -1 {
			tagStr := data[1 : tagEnd+1] // +1 because we started searching from index 1
			tags := strings.Split(tagStr, ";")
			for _, tag := range tags {
				if kv := strings.SplitN(tag, "=", 2); len(kv) == 2 {
					msg.Tags[kv[0]] = kv[1]
				}
			}
		}
	}

	privmsgIndex := strings.Index(data, " PRIVMSG ")
	if privmsgIndex == -1 {
		return nil
	}
	channelStart := privmsgIndex + len(" PRIVMSG ")
	channelEnd := strings.Index(data[channelStart:], " :")
	if channelEnd == -1 {
		return nil
	}
	msg.Channel = data[channelStart : channelStart+channelEnd]

	messageStart := channelStart + channelEnd + 2 // +2 for " :"
	if messageStart < len(data) {
		msg.Content = data[messageStart:]
	}

	// Extract username from display-name tag or parse from IRC format
	if displayName, ok := msg.Tags["display-name"]; ok && displayName != "" {
		msg.Username = displayName
	} else {
		// Parse from IRC :username!username@username.tmi.twitch.tv
		userStart := strings.Index(data, ":")
		if userStart != -1 {
			userEnd := strings.Index(data[userStart:], "!")
			if userEnd != -1 {
				msg.Username = data[userStart+1 : userStart+userEnd]
			}
		}
	}

	// Extract user color from tags
	if userColor, ok := msg.Tags["color"]; ok {
		// log.Printf("Color tag for %s: '%s'\n", msg.Username, userColor)
		if userColor != "" {
			msg.UserColor = convertToLightIfDark(userColor)
		} else {
			msg.UserColor = getTwitchDefaultColor(msg.Username)
		}
	} else {
		log.Printf("No color tag found for %s\n", msg.Username)
		msg.UserColor = "#FFFFFF"
	}

	return msg
}

// Parse channel point redemption messages
func (c *Client) parseRewardRedemption(data string) *RewardRedemption {
	reward := &RewardRedemption{
		RawData:   data,
		Timestamp: time.Now(),
	}

	// Parse tags to extract reward information
	if strings.HasPrefix(data, "@") {
		tagEnd := strings.Index(data, " :")
		if tagEnd != -1 {
			tagStr := data[1:tagEnd]
			tags := strings.Split(tagStr, ";")
			for _, tag := range tags {
				if kv := strings.SplitN(tag, "=", 2); len(kv) == 2 {
					switch kv[0] {
					case "custom-reward-id":
						reward.RewardID = kv[1]
					case "display-name":
						reward.Username = kv[1]
					}
				}
			}
		}
	}

	// Extract the user input (message content) after " PRIVMSG #channel :"
	privmsgIndex := strings.Index(data, " PRIVMSG ")
	if privmsgIndex != -1 {
		// Find the start of the message content
		messageStart := strings.Index(data[privmsgIndex:], " :")
		if messageStart != -1 {
			messageStart += privmsgIndex + 2 // +2 for " :"
			if messageStart < len(data) {
				reward.UserInput = data[messageStart:]
			}
		}
	}

	return reward
}

// Returns the last N messages from the buffer
func (c *Client) GetMessages(n int) []Message {
	return c.messageBuffer.GetLast(n)
}

// Returns all messages in the buffer
func (c *Client) GetAllMessages() []Message {
	return c.messageBuffer.GetAll()
}

// Returns the channel for receiving new messages
func (c *Client) MessageChannel() <-chan Message {
	return c.messageChan
}

// Returns the channel for receiving reward redemptions
func (c *Client) RewardChannel() <-chan RewardRedemption {
	return c.rewardChan
}

// Returns the channel for receiving errors
func (c *Client) ErrorChannel() <-chan error {
	return c.errorChan
}

// Returns whether the client is connected
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Stop the client
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stopped {
		return // Already stopped
	}

	c.stopped = true
	c.connected = false

	// Close the connection first to stop the scanner
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	// Close the stop channel to signal the listen goroutine
	close(c.stopChan)

	// Give the listen goroutine a moment to exit
	time.Sleep(10 * time.Millisecond)

	// Now safely close other channels
	close(c.messageChan)
	close(c.rewardChan)
	close(c.errorChan)
}

// Util

func convertToLightIfDark(hexColor string) string {
	color := strings.TrimPrefix(hexColor, "#")
	if len(color) != 6 {
		return hexColor
	}

	r, _ := strconv.ParseInt(color[0:2], 16, 0)
	g, _ := strconv.ParseInt(color[2:4], 16, 0)
	b, _ := strconv.ParseInt(color[4:6], 16, 0)

	if 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) < 128 {
		r += int64(float64(255-r) * 0.4)
		g += int64(float64(255-g) * 0.4)
		b += int64(float64(255-b) * 0.4)
	}

	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

func getTwitchDefaultColor(username string) string {
	colors := []string{
		"#FF0000", "#0000FF", "#00FF00", "#B22222", "#FF7F50",
		"#9ACD32", "#FF4500", "#2E8B57", "#DAA520", "#D2691E",
		"#5F9EA0", "#1E90FF", "#FF69B4", "#8A2BE2", "#00FF7F",
	}

	hash := 0
	for _, char := range strings.ToLower(username) {
		hash = (hash << 5) - hash + int(char)
	}

	return colors[abs(hash)%len(colors)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
