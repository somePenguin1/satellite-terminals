// Package live fans telemetry out to browser sessions. Each browser gets a once-per-second
// fleet tick and, for the terminals it has subscribed to, every report as it is accepted.
// Slow clients never block ingest: their buffer fills and messages are dropped.
package live

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"satfleet/ingest/internal/telemetry"
)

const (
	clientBuffer     = 256
	MaxSubscriptions = 50
)

type Client struct {
	send    chan []byte
	subs    map[string]struct{} // guarded by Hub.mu
	dropped atomic.Uint64
}

// Messages yields encoded JSON messages for this client.
func (c *Client) Messages() <-chan []byte { return c.send }

func (c *Client) Dropped() uint64 { return c.dropped.Load() }

func (c *Client) offer(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.dropped.Add(1)
	}
}

type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]struct{}
	byTerminal map[string]map[*Client]struct{}
	watched    atomic.Int64 // number of terminals with at least one subscriber: fast-path check
}

func NewHub() *Hub {
	return &Hub{clients: map[*Client]struct{}{}, byTerminal: map[string]map[*Client]struct{}{}}
}

func (h *Hub) Register() *Client {
	c := &Client{send: make(chan []byte, clientBuffer), subs: map[string]struct{}{}}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return
	}
	for id := range c.subs {
		h.removeSub(c, id)
	}
	delete(h.clients, c)
	close(c.send)
}

// Subscribe adds terminal subscriptions, up to MaxSubscriptions per client.
func (h *Hub) Subscribe(c *Client, ids []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return
	}
	for _, id := range ids {
		if len(c.subs) >= MaxSubscriptions {
			return
		}
		if _, dup := c.subs[id]; dup {
			continue
		}
		c.subs[id] = struct{}{}
		set := h.byTerminal[id]
		if set == nil {
			set = map[*Client]struct{}{}
			h.byTerminal[id] = set
			h.watched.Add(1)
		}
		set[c] = struct{}{}
	}
}

func (h *Hub) Unsubscribe(c *Client, ids []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range ids {
		if _, ok := c.subs[id]; ok {
			h.removeSub(c, id)
		}
	}
}

func (h *Hub) removeSub(c *Client, id string) {
	delete(c.subs, id)
	if set := h.byTerminal[id]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.byTerminal, id)
			h.watched.Add(-1)
		}
	}
}

type telemetryMessage struct {
	Type   string         `json:"type"`
	Report telemetry.Wire `json:"report"`
}

// PublishReport is called for every accepted report, so the no-subscriber path is a single atomic load.
func (h *Hub) PublishReport(r *telemetry.Report) {
	if h.watched.Load() == 0 {
		return
	}
	h.mu.RLock()
	set := h.byTerminal[r.TerminalID]
	if len(set) == 0 {
		h.mu.RUnlock()
		return
	}
	targets := make([]*Client, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	msg, err := json.Marshal(telemetryMessage{Type: "telemetry", Report: r.ToWire()})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range targets {
		if _, alive := h.clients[c]; alive { // client may have unregistered (channel closed) meanwhile
			c.offer(msg)
		}
	}
}

// Broadcast sends a pre-encoded message to every client.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		c.offer(msg)
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ControlMessage is what browsers send: {"type":"subscribe","terminalIds":["SAT-00001"]}.
type ControlMessage struct {
	Type        string   `json:"type"`
	TerminalIDs []string `json:"terminalIds"`
}

// HandleControl applies a client control message; unknown types are ignored.
func (h *Hub) HandleControl(c *Client, data []byte) {
	var m ControlMessage
	if json.Unmarshal(data, &m) != nil {
		return
	}
	switch m.Type {
	case "subscribe":
		h.Subscribe(c, m.TerminalIDs)
	case "unsubscribe":
		h.Unsubscribe(c, m.TerminalIDs)
	}
}
