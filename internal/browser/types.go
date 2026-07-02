package browser

import (
	"time"
)

// Cookie represents a browser cookie.
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires"`
	HTTPOnly bool      `json:"httpOnly"`
	Secure   bool      `json:"secure"`
}

// ConsoleMsg represents a message printed to the browser console.
type ConsoleMsg struct {
	Type string    `json:"type"`
	Text string    `json:"text"`
	Time time.Time `json:"time"`
}

// SnapshotOpts holds configuration options for taking a snapshot of a page.
type SnapshotOpts struct{}

// ShotOpts holds options for taking a screenshot of a page.
type ShotOpts struct {
	FullPage bool `json:"fullPage"`
}

// Snapshot represents a parsed snapshot of the page.
type Snapshot struct {
	AXTree string `json:"axTree"` // Accessibility tree snapshot
	Text   string `json:"text"`   // Page text content
}

// ActRequest defines the interaction parameters on a page.
type ActRequest struct {
	Kind  string        `json:"kind"`            // click, type, press, hover, wait, evaluate
	Ref   string        `json:"ref"`             // Element reference e.g., "e1"
	Text  string        `json:"text,omitempty"`  // For "type" or "press"
	Code  string        `json:"code,omitempty"`  // For "evaluate"
	Delay time.Duration `json:"delay,omitempty"` // For "wait"
}

// LightpandaConfig holds configuration specific to Lightpanda engine.
type LightpandaConfig struct {
	BinPath string `json:"binPath"`
	Port    int    `json:"port"`
}

// ChromiumConfig holds configuration specific to Chromium engine.
type ChromiumConfig struct {
	BinPath string `json:"binPath"`
}

// RemoteConfig holds configuration specific to Remote CDP connection.
type RemoteConfig struct {
	URL string `json:"url"`
}

// Config represents the browser configuration schema.
type Config struct {
	Engine     string           `json:"engine"` // "lightpanda", "chromium", "remote"
	Headless   bool             `json:"headless"`
	Lightpanda LightpandaConfig `json:"lightpanda"`
	Chromium   ChromiumConfig   `json:"chromium"`
	Remote     RemoteConfig     `json:"remote"`
}
