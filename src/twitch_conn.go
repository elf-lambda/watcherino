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

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		messages: make([]Message, size),
		size:     size,
		index:    0,
	}
}

func (rb *RingBuffer) Add(msg Message) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.messages[rb.index] = msg
	rb.index = (rb.index + 1) % rb.size
}

func (rb *RingBuffer) GetAll() []Message {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]Message, 0, rb.size)
	for i := 0; i < rb.size; i++ {
		idx := (rb.index + i) % rb.size
		if !rb.messages[idx].Timestamp.IsZero() {
			result = append(result, rb.messages[idx])
		}
	}
	return result
}

func (rb *RingBuffer) GetLast(n int) []Message {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.size {
		n = rb.size
	}

	result := make([]Message, 0, n)
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

func NewClient(channel string, bufferSize int) *Client {
	return &Client{
		channel:       channel,
		messageBuffer: NewRingBuffer(bufferSize),
		rewardChan:    make(chan RewardRedemption, 100),
		messageChan:   make(chan Message, 100),
		errorChan:     make(chan error, 10),
		stopChan:      make(chan struct{}),
	}
}

func (c *Client) Connect() error {
	server := "irc.chat.twitch.tv"
	port := 6667

	c.mu.Lock()
	if c.username == "" {
		c.username = fmt.Sprintf("justinfan%d", rand.Intn(9999-1000)+1000)
	}
	c.mu.Unlock()

	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.Dial("tcp", net.JoinHostPort(server, fmt.Sprintf("%d", port)))
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	fmt.Fprintf(conn, "CAP REQ :twitch.tv/tags twitch.tv/commands\r\n")
	fmt.Fprintf(conn, "NICK %s\r\n", c.username)
	fmt.Fprintf(conn, "JOIN %s\r\n", c.channel)

	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	return nil
}

func (c *Client) Start() {
	go c.listen()
}

func (c *Client) listen() {
	for {
		c.mu.RLock()
		conn := c.conn
		stopped := c.stopped
		c.mu.RUnlock()

		if stopped || conn == nil {
			return
		}

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			data := scanner.Text()

			if strings.HasPrefix(data, "PING") {
				fmt.Fprintf(conn, "PONG :tmi.twitch.tv\r\n")
				continue
			}
			var msg *Message

			// Route based on command type
			if strings.Contains(data, " PRIVMSG ") {
				if strings.Contains(data, "custom-reward-id=") {
					reward := c.parseRewardRedemption(data)
					if reward != nil {
						select {
						case c.rewardChan <- *reward:
						default:
						}
					}
					continue
				}
				msg = c.parsePrivMsg(data)
			} else if strings.Contains(data, " CLEARCHAT ") {
				msg = c.parseClearChat(data)
			}

			if msg != nil {
				c.messageBuffer.Add(*msg)
				select {
				case c.messageChan <- *msg:
				default:
				}
			}
		}

		// 	if strings.Contains(data, "PRIVMSG") {
		// 		if strings.Contains(data, "custom-reward-id=") {
		// 			reward := c.parseRewardRedemption(data)
		// 			if reward != nil {
		// 				select {
		// 				case c.rewardChan <- *reward:
		// 				default:
		// 				}
		// 			}
		// 		} else {
		// 			msg := c.parsePrivMsg(data)
		// 			if msg != nil {
		// 				c.messageBuffer.Add(*msg)
		// 				select {
		// 				case c.messageChan <- *msg:
		// 				default:
		// 				}
		// 			}
		// 		}
		// 	}
		// }
		//

		c.mu.Lock()
		if c.stopped {
			c.mu.Unlock()
			return
		}
		c.connected = false
		c.mu.Unlock()

		log.Printf("Connection lost for %s, reconnecting...", c.channel)
		for {
			time.Sleep(5 * time.Second)
			if err := c.Connect(); err == nil {
				log.Printf("Reconnected to %s", c.channel)
				break
			}
			c.mu.RLock()
			if c.stopped {
				c.mu.RUnlock()
				return
			}
			c.mu.RUnlock()
		}
	}
}

func (c *Client) parseClearChat(data string) *Message {
	msg := &Message{
		RawData:   data,
		Timestamp: time.Now(),
		Tags:      make(map[string]string),
	}

	payload := data
	if strings.HasPrefix(data, "@") {
		spaceIdx := strings.Index(data, " ")
		if spaceIdx == -1 {
			return nil
		}
		tagStr := data[1:spaceIdx]
		for _, tag := range strings.Split(tagStr, ";") {
			kv := strings.SplitN(tag, "=", 2)
			if len(kv) == 2 {
				msg.Tags[kv[0]] = kv[1]
			}
		}
		payload = data[spaceIdx+1:]
	}

	parts := strings.SplitN(payload, " CLEARCHAT ", 2)
	if len(parts) < 2 {
		return nil
	}

	// Format is usually: :tmi.twitch.tv CLEARCHAT #channel :targetuser
	// Or for a full chat clear: :tmi.twitch.tv CLEARCHAT #channel
	remaining := parts[1]
	var username string
	if colonIdx := strings.Index(remaining, " :"); colonIdx != -1 {
		msg.Channel = remaining[:colonIdx]
		// msg.Username = remaining[colonIdx+2:]
		username = remaining[colonIdx+2:]
		msg.Username = "<-- SYSTEM -->"
	} else {
		msg.Channel = remaining
	}

	if duration, ok := msg.Tags["ban-duration"]; ok {
		msg.Content = fmt.Sprintf("[TIMEOUT] %s for %ss", username, duration)
	} else if msg.Username != "" {
		msg.Content = fmt.Sprintf("[BAN] %s", username)
	} else {
		msg.Content = "[CLEARED] Chat was cleared by a moderator"
	}
	msg.UserColor = "#FF0000"

	return msg
}

func (c *Client) parsePrivMsg(data string) *Message {
	msg := &Message{
		RawData:   data,
		Timestamp: time.Now(),
		Tags:      make(map[string]string),
	}

	payload := data
	if strings.HasPrefix(data, "@") {
		spaceIdx := strings.Index(data, " ")
		if spaceIdx == -1 {
			return nil
		}
		tagStr := data[1:spaceIdx]
		for _, tag := range strings.Split(tagStr, ";") {
			kv := strings.SplitN(tag, "=", 2)
			if len(kv) == 2 {
				msg.Tags[kv[0]] = kv[1]
			}
		}
		payload = data[spaceIdx+1:]
	}

	parts := strings.SplitN(payload, " PRIVMSG ", 2)
	if len(parts) < 2 {
		return nil
	}

	if disp, ok := msg.Tags["display-name"]; ok && disp != "" {
		msg.Username = disp
	} else {
		userPart := parts[0]
		if bangIdx := strings.Index(userPart, "!"); bangIdx != -1 {
			msg.Username = userPart[1:bangIdx]
		}
	}

	contentParts := strings.SplitN(parts[1], " :", 2)
	if len(contentParts) == 2 {
		msg.Channel = contentParts[0]
		msg.Content = contentParts[1]
	}

	if col, ok := msg.Tags["color"]; ok && col != "" {
		msg.UserColor = convertToLightIfDark(col)
	} else {
		msg.UserColor = getTwitchDefaultColor(msg.Username)
	}

	return msg
}

func (c *Client) parseRewardRedemption(data string) *RewardRedemption {
	msg := c.parsePrivMsg(data)
	if msg == nil {
		return nil
	}

	return &RewardRedemption{
		RewardID:   msg.Tags["custom-reward-id"],
		Username:   msg.Username,
		RewardName: "Custom Reward",
		UserInput:  msg.Content,
		RawData:    data,
		Timestamp:  msg.Timestamp,
	}
}

func (c *Client) GetMessages(n int) []Message            { return c.messageBuffer.GetLast(n) }
func (c *Client) GetAllMessages() []Message              { return c.messageBuffer.GetAll() }
func (c *Client) MessageChannel() <-chan Message         { return c.messageChan }
func (c *Client) RewardChannel() <-chan RewardRedemption { return c.rewardChan }
func (c *Client) ErrorChannel() <-chan error             { return c.errorChan }

func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *Client) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	c.connected = false
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

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
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func getTwitchDefaultColor(username string) string {
	colors := []string{
		"#FF0000", "#0000FF", "#008000", "#B22222", "#FF7F50",
		"#9ACD32", "#FF4500", "#2E8B57", "#DAA520", "#D2691E",
		"#5F9EA0", "#1E90FF", "#FF69B4", "#8A2BE2", "#00FF7F",
	}
	if username == "" {
		return colors[0]
	}
	hash := 0
	for _, char := range username {
		hash += int(char)
	}
	return colors[hash%len(colors)]
}
