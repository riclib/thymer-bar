// Package tray provides system tray functionality.
package tray

import (
	_ "embed"
	"fmt"

	"fyne.io/systray"
)

//go:embed icon.png
var iconData []byte

// Tray manages the system tray icon and menu.
type Tray struct {
	onDashboard func()
	onSettings  func()
	onQuit      func()
	onSync      func()

	// Menu items (set during onReady)
	mStatus      *systray.MenuItem
	mConnections *systray.MenuItem
}

// Config holds tray configuration.
type Config struct {
	OnDashboard func() // Called when "Dashboard" is clicked
	OnSettings  func() // Called when "Settings" is clicked
	OnQuit      func() // Called when "Quit" is clicked
	OnSync      func() // Called when "Sync All" is clicked
}

// New creates a new tray manager.
func New(cfg Config) *Tray {
	return &Tray{
		onDashboard: cfg.OnDashboard,
		onSettings:  cfg.OnSettings,
		onQuit:      cfg.OnQuit,
		onSync:      cfg.OnSync,
	}
}

// Run starts the system tray (blocks until quit).
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

// RunAsync starts the system tray in a goroutine.
func (t *Tray) RunAsync() {
	go systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("thymer-bar")
	systray.SetTooltip("thymer-bar - Productivity Hub")

	// Status (non-clickable)
	t.mStatus = systray.AddMenuItem("Starting...", "Connection status")
	t.mStatus.Disable()

	t.mConnections = systray.AddMenuItem("0 connections", "Thymer connections")
	t.mConnections.Disable()

	systray.AddSeparator()

	// Window actions
	mDashboard := systray.AddMenuItem("Dashboard", "Open dashboard")
	mSettings := systray.AddMenuItem("Settings", "Open settings")
	mSync := systray.AddMenuItem("Sync All", "Trigger sync for all sources")

	systray.AddSeparator()

	// Info
	mPort := systray.AddMenuItem("Port: 8496", "HTTP/WebSocket port")
	mPort.Disable()

	systray.AddSeparator()

	// Quit
	mQuit := systray.AddMenuItem("Quit", "Quit thymer-bar")

	// Handle clicks
	go func() {
		for {
			select {
			case <-mDashboard.ClickedCh:
				if t.onDashboard != nil {
					t.onDashboard()
				}
			case <-mSettings.ClickedCh:
				if t.onSettings != nil {
					t.onSettings()
				}
			case <-mSync.ClickedCh:
				if t.onSync != nil {
					t.onSync()
				}
			case <-mQuit.ClickedCh:
				if t.onQuit != nil {
					t.onQuit()
				}
				systray.Quit()
				return
			}
		}
	}()
}

func (t *Tray) onExit() {
	fmt.Println("System tray exiting...")
}

// UpdateStatus updates the status menu item.
func (t *Tray) UpdateStatus(status string) {
	if t.mStatus != nil {
		t.mStatus.SetTitle(status)
	}
}

// UpdateConnections updates the connection count.
func (t *Tray) UpdateConnections(count int) {
	if t.mConnections != nil {
		if count == 1 {
			t.mConnections.SetTitle("1 connection")
		} else {
			t.mConnections.SetTitle(fmt.Sprintf("%d connections", count))
		}
	}
}
