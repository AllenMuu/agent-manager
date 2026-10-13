// Package weblinks holds the optional peer URLs for a paired local Web session.
package weblinks

import "sync"

// Links shares the two loopback page URLs while their servers are being started.
type Links struct {
	mu           sync.RWMutex
	consoleURL   string
	installUIURL string
}

// New creates an empty link set for one paired local Web session.
func New() *Links { return &Links{} }

// SetConsoleURL records the session URL for the main Web console.
func (l *Links) SetConsoleURL(value string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.consoleURL = value
	l.mu.Unlock()
}

// ConsoleURL returns the main Web console's session URL, if available.
func (l *Links) ConsoleURL() string {
	if l == nil {
		return ""
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.consoleURL
}

// SetInstallUIURL records the URL for the local Skill installation UI.
func (l *Links) SetInstallUIURL(value string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.installUIURL = value
	l.mu.Unlock()
}

// InstallUIURL returns the local Skill installation UI URL, if available.
func (l *Links) InstallUIURL() string {
	if l == nil {
		return ""
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.installUIURL
}
