package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/aerol-ai/kubeagent/pkg/config"
	"github.com/aerol-ai/kubeagent/pkg/upgrade"
	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 30 * time.Second
)

// Version is set via ldflags at build time.
var Version = "dev"

// MessageHandler processes an incoming command.
type MessageHandler func(cmd config.Command)

// ConnectionCallback is called when connection state changes.
type ConnectionCallback func(connected bool)

// Client manages a WebSocket connection to the platform.
type Client struct {
	cfg         *config.Config
	conn        *websocket.Conn
	mu          sync.Mutex
	handler     MessageHandler
	done        chan struct{}
	onConnect   ConnectionCallback
	agentStatus *config.AgentStatus
}

// NewClient creates a new WebSocket client.
func NewClient(cfg *config.Config, handler MessageHandler) *Client {
	return &Client{
		cfg:     cfg,
		handler: handler,
		done:    make(chan struct{}),
	}
}

// SetConnectionCallback registers a callback for connection state changes.
func (c *Client) SetConnectionCallback(cb ConnectionCallback) {
	c.onConnect = cb
}

// SetAgentStatus sets the status payload sent on each connection.
func (c *Client) SetAgentStatus(status *config.AgentStatus) {
	c.agentStatus = status
}

// ConnectWithRetry connects to the platform with exponential backoff.
func (c *Client) ConnectWithRetry() error {
	interval := c.cfg.ReconnectInterval
	for {
		err := c.connect()
		if err == nil {
			log.Println("Connected to platform")
			interval = c.cfg.ReconnectInterval
			c.readPump()
			select {
			case <-c.done:
				return nil
			default:
			}
			if c.onConnect != nil {
				c.onConnect(false)
			}
			log.Println("Connection lost, reconnecting...")
		} else {
			log.Println("Connection failed, retrying...")
		}

		select {
		case <-c.done:
			return nil
		case <-time.After(interval):
		}
		interval = interval * 2
		if interval > c.cfg.MaxReconnectInterval {
			interval = c.cfg.MaxReconnectInterval
		}
	}
}

func (c *Client) connect() error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.cfg.Token)
	header.Set("X-Agent-ID", c.cfg.AgentID)
	header.Set("X-Agent-Version", Version)

	conn, _, err := websocket.DefaultDialer.Dial(c.cfg.PlatformURL, header)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Send agent status on connect
	if c.agentStatus != nil {
		c.SendJSON(map[string]any{
			"type":    "agent_status",
			"payload": c.agentStatus,
		})
	}

	if c.onConnect != nil {
		c.onConnect(true)
	}

	go c.writePump()
	return nil
}

func (c *Client) readPump() {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Println("WebSocket read error")
			}
			return
		}
		var cmd config.Command
		if err := json.Unmarshal(message, &cmd); err != nil {
			log.Println("Failed to parse command")
			continue
		}

		if cmd.Type == "ping" {
			c.SendJSON(map[string]string{"type": "pong"})
			if cmd.Version != "" && c.cfg.AutoUpgradeEnabled && cmd.Version != c.cfg.HelmChartVersion {
				log.Printf("Version mismatch detected. Gateway: %s, Agent: %s. Triggering automatic upgrade.", cmd.Version, c.cfg.HelmChartVersion)
				go upgrade.Perform(c.cfg, cmd.Version)
			}
			continue
		}

		if cmd.RequestID != "" {
			go c.handler(cmd)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			if c.conn == nil {
				c.mu.Unlock()
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.mu.Unlock()
			if err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// SendResult sends a result back to the platform.
func (c *Client) SendResult(result config.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteJSON(result)
}

// SendJSON sends an arbitrary JSON payload.
func (c *Client) SendJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteJSON(v)
}

// Close gracefully shuts down the client.
func (c *Client) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.mu.Lock()
	if c.conn != nil {
		c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
}
