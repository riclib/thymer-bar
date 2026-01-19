package ui

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

)

// Helper functions
func countPending(tasks []Task) int {
	count := 0
	for _, t := range tasks {
		if t.Status != "done" {
			count++
		}
	}
	return count
}

func hasDoneTasks(tasks []Task) bool {
	for _, t := range tasks {
		if t.Status == "done" {
			return true
		}
	}
	return false
}

func isPast(hour int) bool {
	return hour < time.Now().Hour()
}

func isNow(hour int) bool {
	return hour == time.Now().Hour()
}

func formatHour(hour int) string {
	if hour == 0 {
		return "12a"
	} else if hour < 12 {
		return fmt.Sprintf("%da", hour)
	} else if hour == 12 {
		return "12p"
	}
	return fmt.Sprintf("%dp", hour-12)
}

func formatElapsed(seconds int) string {
	h := seconds / 3600
	m := (seconds % 3600) / 60
	s := seconds % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func eventAtHour(event Event, hour int) bool {
	// Parse event time and check if it's at this hour
	// For now, simple check - would need proper time parsing
	t, err := time.Parse("3:04 PM", event.Time)
	if err != nil {
		return false
	}
	return t.Hour() == hour
}

// Timeline constants: 24 hours (midnight to midnight)
const timelineStartHour = 0
const timelineEndHour = 24
const timelineTotalMinutes = 24 * 60 // 1440 minutes

// nowLinePosition returns CSS top position for current time indicator
func nowLinePosition() string {
	now := time.Now()
	hour := now.Hour()
	minute := now.Minute()

	minutesFromStart := hour*60 + minute
	percent := float64(minutesFromStart) / float64(timelineTotalMinutes) * 100
	return fmt.Sprintf("%.2f%%", percent)
}

// blockStyle returns CSS style for positioning a block by time and duration
func blockStyle(startTime, duration string) string {
	top := timeToPercent(startTime)
	height := durationToPercent(duration)
	return fmt.Sprintf("top: %.2f%%; height: %.2f%%;", top, height)
}

// timeToPercent converts a time string to a percentage position in the timeline
func timeToPercent(timeStr string) float64 {
	// Try parsing various formats
	var t time.Time
	var err error

	formats := []string{"3:04 PM", "3:04PM", "3PM", "3pm", "15:04", "3:04"}
	for _, format := range formats {
		t, err = time.Parse(format, timeStr)
		if err == nil {
			break
		}
	}
	if err != nil {
		return 0
	}

	hour := t.Hour()
	minute := t.Minute()
	minutesFromStart := hour*60 + minute
	return float64(minutesFromStart) / float64(timelineTotalMinutes) * 100
}

// durationToPercent converts a duration string to a percentage height
func durationToPercent(durationStr string) float64 {
	minutes := parseDurationMinutes(durationStr)
	if minutes == 0 {
		minutes = 60 // default to 1 hour if not specified
	}
	return float64(minutes) / float64(timelineTotalMinutes) * 100
}

// parseDurationMinutes parses duration strings like "2h", "30m", "1h30m", "90m"
func parseDurationMinutes(s string) int {
	if s == "" {
		return 0
	}

	s = strings.ToLower(strings.TrimSpace(s))
	total := 0

	// Try to parse as Go duration first
	if d, err := time.ParseDuration(s); err == nil {
		return int(d.Minutes())
	}

	// Parse manually: look for hours and minutes
	// Handles: "2h", "30m", "1h30m", "1.5h"
	numStr := ""
	for _, c := range s {
		if c >= '0' && c <= '9' || c == '.' {
			numStr += string(c)
		} else if c == 'h' {
			if num, err := strconv.ParseFloat(numStr, 64); err == nil {
				total += int(num * 60)
			}
			numStr = ""
		} else if c == 'm' {
			if num, err := strconv.ParseFloat(numStr, 64); err == nil {
				total += int(num)
			}
			numStr = ""
		}
	}

	return total
}

// isTimePast checks if a time string represents a time before now
func isTimePast(timeStr string) bool {
	var t time.Time
	var err error

	formats := []string{"3:04 PM", "3:04PM", "3PM", "3pm", "15:04"}
	for _, format := range formats {
		t, err = time.Parse(format, timeStr)
		if err == nil {
			break
		}
	}
	if err != nil {
		return false
	}

	now := time.Now()
	// Compare just the time portion
	eventMinutes := t.Hour()*60 + t.Minute()
	nowMinutes := now.Hour()*60 + now.Minute()
	return eventMinutes < nowMinutes
}

// renderMarkdownTitle converts markdown links to HTML with thymer: links styled
// thymerLinkRe matches [title](thymer:GUID) markdown links
var thymerLinkRe = regexp.MustCompile(`\[([^\]]+)\]\(thymer:([^)]+)\)`)

func renderMarkdownTitle(title string) string {
	// Escape HTML first
	escaped := html.EscapeString(title)

	// Replace [text](thymer:GUID) with clickable refs that navigate to Thymer
	result := thymerLinkRe.ReplaceAllStringFunc(escaped, func(match string) string {
		parts := thymerLinkRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		linkText := parts[1]
		guid := parts[2]
		// Clickable ref styled in Thymer teal
		return fmt.Sprintf(`<span class="thymer-ref" data-action="navigate-ref" data-guid="%s">%s</span>`, guid, linkText)
	})

	return result
}

// sessionColumnClass returns a CSS class for horizontal offset when sessions overlap
func sessionColumnClass(sessions []CompletedSession, index int) string {
	if index == 0 {
		return ""
	}

	// Parse current session's start time
	currentStart, err := time.Parse(time.RFC3339, sessions[index].StartedAt)
	if err != nil {
		return ""
	}

	// Check if this session overlaps with any previous session
	for i := 0; i < index; i++ {
		prevStart, err := time.Parse(time.RFC3339, sessions[i].StartedAt)
		if err != nil {
			continue
		}
		prevEnd := prevStart.Add(time.Duration(sessions[i].Elapsed) * time.Second)

		// If current starts before previous ends, they overlap
		if currentStart.Before(prevEnd) {
			return "col-1" // Offset to the right
		}
	}

	return ""
}

// sessionBlockStyle returns CSS style for positioning a completed session block
func sessionBlockStyle(startedAt string, elapsedSeconds int) string {
	// Parse RFC3339 start time
	t, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return ""
	}

	// Convert to local time
	local := t.Local()
	hour := local.Hour()
	minute := local.Minute()

	minutesFromStart := hour*60 + minute
	top := float64(minutesFromStart) / float64(timelineTotalMinutes) * 100

	// Calculate height based on elapsed time
	height := float64(elapsedSeconds) / 60 / float64(timelineTotalMinutes) * 100
	if height < 1 {
		height = 1 // Minimum height for visibility
	}

	return fmt.Sprintf("top: %.2f%%; height: %.2f%%;", top, height)
}

// activeSessionBlockStyle returns CSS style for the currently active session.
// It positions the block starting at the current time and uses a minimum visual
// height of 30 minutes unless the actual elapsed time is longer.
func activeSessionBlockStyle(elapsedSeconds int) string {
	// Active session starts at "now" minus elapsed time
	now := time.Now()
	startTime := now.Add(-time.Duration(elapsedSeconds) * time.Second)

	hour := startTime.Hour()
	minute := startTime.Minute()
	minutesFromStart := hour*60 + minute
	top := float64(minutesFromStart) / float64(timelineTotalMinutes) * 100

	// Visual height: minimum 30 minutes, or actual elapsed time if longer
	elapsedMinutes := float64(elapsedSeconds) / 60
	visualMinutes := elapsedMinutes
	if visualMinutes < 30 {
		visualMinutes = 30 // Minimum 30-minute visual height
	}
	height := visualMinutes / float64(timelineTotalMinutes) * 100

	return fmt.Sprintf("top: %.2f%%; height: %.2f%%;", top, height)
}

// formatDuration formats seconds into a human-readable duration string
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, remainingMinutes)
}
