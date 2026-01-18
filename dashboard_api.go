package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"thymer-bar/internal/config"
	"thymer-bar/internal/events"
	"thymer-bar/internal/ui"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Dashboard state (in-memory, persisted to state file)
type DashboardState struct {
	mu       sync.RWMutex
	Session  *SessionState `json:"session,omitempty"`
	Habits   []HabitState  `json:"habits"`
	Stats    DailyStats    `json:"stats"`
	filePath string
}

// SessionState tracks the current work session.
type SessionState struct {
	Active    bool      `json:"active"`
	TaskGUID  string    `json:"task_guid"`
	TaskTitle string    `json:"task_title"`
	TaskSource string   `json:"task_source,omitempty"`
	StartTime time.Time `json:"start_time"`
	Elapsed   int       `json:"elapsed"` // seconds
	Paused    bool      `json:"paused"`
	PausedAt  *time.Time `json:"paused_at,omitempty"`
}

// HabitState tracks a habit for today.
type HabitState struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Type     string `json:"type"` // "virtue" or "vice"
	Done     bool   `json:"done"`
	Progress int    `json:"progress"` // 0-100
}

// DailyStats tracks daily progress.
type DailyStats struct {
	Date      string `json:"date"` // YYYY-MM-DD
	Planned   int    `json:"planned"`
	Completed int    `json:"completed"`
	Queued    int    `json:"queued"`
	TotalTime int    `json:"total_time"` // seconds worked today
}

// DashboardData is returned to the frontend.
type DashboardData struct {
	Tasks  []DashboardTask  `json:"tasks"`
	Events []DashboardEvent `json:"events"`
	Habits []HabitData      `json:"habits"`
	Stats  DailyStats       `json:"stats"`
	Session *SessionData    `json:"session,omitempty"`
}

// DashboardTask represents a task for the dashboard.
type DashboardTask struct {
	GUID          string `json:"guid"`
	Title         string `json:"title"`
	Source        string `json:"source"`
	Status        string `json:"status"` // pending, in_progress, done
	Estimate      string `json:"estimate,omitempty"`
	ScheduledTime string `json:"scheduledTime,omitempty"`
}

// DashboardEvent represents a calendar event.
type DashboardEvent struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Start    string `json:"start"`
	End      string `json:"end,omitempty"`
	Duration string `json:"duration,omitempty"`
	Location string `json:"location,omitempty"`
}

// HabitData is sent to the frontend.
type HabitData struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Type     string `json:"type"`
	Done     bool   `json:"done"`
	Progress int    `json:"progress"`
}

// SessionData is sent to the frontend.
type SessionData struct {
	Active     bool   `json:"active"`
	TaskGUID   string `json:"taskGuid"`
	TaskTitle  string `json:"taskTitle"`
	TaskSource string `json:"taskSource,omitempty"`
	Elapsed    int    `json:"elapsed"`
}

var dashboardState *DashboardState

func initDashboardState() error {
	stateDir := config.StateDir()
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	dashboardState = &DashboardState{
		Habits:   getDefaultHabits(),
		Stats:    DailyStats{Date: todayDate()},
		filePath: filepath.Join(stateDir, "dashboard.json"),
	}

	// Load existing state
	data, err := os.ReadFile(dashboardState.filePath)
	if err == nil {
		json.Unmarshal(data, dashboardState)
	}

	// Reset daily stats if it's a new day
	if dashboardState.Stats.Date != todayDate() {
		dashboardState.Stats = DailyStats{Date: todayDate()}
		// Reset habits for new day
		for i := range dashboardState.Habits {
			dashboardState.Habits[i].Done = false
			dashboardState.Habits[i].Progress = 0
		}
		dashboardState.save()
	}

	return nil
}

func getDefaultHabits() []HabitState {
	return []HabitState{
		{ID: "exercise", Name: "Exercise", Icon: "🏃", Type: "virtue", Done: false, Progress: 0},
		{ID: "read", Name: "Read", Icon: "📚", Type: "virtue", Done: false, Progress: 0},
		{ID: "meditate", Name: "Meditate", Icon: "🧘", Type: "virtue", Done: false, Progress: 0},
		{ID: "social-media", Name: "Social Media", Icon: "📱", Type: "vice", Done: false, Progress: 0},
	}
}

func todayDate() string {
	return time.Now().Format("2006-01-02")
}

func (s *DashboardState) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

// =============================================================================
// Dashboard API (exposed to frontend)
// =============================================================================

// GetDashboardData returns all dashboard data.
func (a *App) GetDashboardData() (*DashboardData, error) {
	if dashboardState == nil {
		if err := initDashboardState(); err != nil {
			return nil, err
		}
	}

	dashboardState.mu.RLock()
	defer dashboardState.mu.RUnlock()

	// Get tasks from today's plan (from Thymer via WebSocket or local bits)
	tasks, err := a.getTodayTasks()
	if err != nil {
		tasks = []DashboardTask{}
	}

	// Get calendar events
	events, err := a.getTodayEvents()
	if err != nil {
		events = []DashboardEvent{}
	}

	// Build habits data
	habits := make([]HabitData, len(dashboardState.Habits))
	for i, h := range dashboardState.Habits {
		habits[i] = HabitData{
			ID:       h.ID,
			Name:     h.Name,
			Icon:     h.Icon,
			Type:     h.Type,
			Done:     h.Done,
			Progress: h.Progress,
		}
	}

	// Build session data
	var session *SessionData
	if dashboardState.Session != nil && dashboardState.Session.Active {
		// Calculate current elapsed time
		elapsed := dashboardState.Session.Elapsed
		if !dashboardState.Session.Paused {
			elapsed += int(time.Since(dashboardState.Session.StartTime).Seconds())
		}
		session = &SessionData{
			Active:     true,
			TaskGUID:   dashboardState.Session.TaskGUID,
			TaskTitle:  dashboardState.Session.TaskTitle,
			TaskSource: dashboardState.Session.TaskSource,
			Elapsed:    elapsed,
		}
	}

	return &DashboardData{
		Tasks:   tasks,
		Events:  events,
		Habits:  habits,
		Stats:   dashboardState.Stats,
		Session: session,
	}, nil
}

// GetDashboardHTML returns the rendered dashboard HTML using templ.
func (a *App) GetDashboardHTML() (string, error) {
	data, err := a.GetDashboardData()
	if err != nil {
		return "", err
	}

	// Convert to UI types
	uiData := ui.DashboardData{
		Connected: a.thymer != nil && a.thymer.ConnectionCount() > 0,
		Date:      time.Now().Format("Monday, January 2"),
	}

	// Convert tasks
	for _, t := range data.Tasks {
		uiData.Tasks = append(uiData.Tasks, ui.Task{
			GUID:   t.GUID,
			Title:  t.Title,
			Source: t.Source,
			Status: t.Status,
		})
	}

	// Convert events
	for _, e := range data.Events {
		eventTime := ""
		if e.Start != "" {
			if t, err := time.Parse(time.RFC3339, e.Start); err == nil {
				eventTime = t.Format("3:04 PM")
			}
		}
		uiData.Events = append(uiData.Events, ui.Event{
			ID:       e.ID,
			Title:    e.Title,
			Time:     eventTime,
			Duration: e.Duration,
			Location: e.Location,
		})
	}

	// Convert habits
	for _, h := range data.Habits {
		uiData.Habits = append(uiData.Habits, ui.Habit{
			ID:   h.ID,
			Name: h.Name,
			Icon: h.Icon,
			Type: h.Type,
			Done: h.Done,
		})
	}

	// Calculate real stats from tasks
	uiData.Stats = ui.Stats{
		Planned:   len(data.Tasks),
		Completed: countCompleted(data.Tasks),
		Queued:    len(data.Tasks) - countCompleted(data.Tasks),
	}

	// Convert session
	if data.Session != nil && data.Session.Active {
		uiData.Session = &ui.Session{
			Active:    true,
			TaskGUID:  data.Session.TaskGUID,
			TaskTitle: data.Session.TaskTitle,
			Elapsed:   data.Session.Elapsed,
		}
	}

	var buf bytes.Buffer
	err = ui.Dashboard(uiData).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func countCompleted(tasks []DashboardTask) int {
	count := 0
	for _, t := range tasks {
		if t.Status == "done" {
			count++
		}
	}
	return count
}

// GetTodayTasks returns tasks planned for today.
func (a *App) GetTodayTasks() ([]DashboardTask, error) {
	return a.getTodayTasks()
}

// GetCalendarEvents returns calendar events for today.
func (a *App) GetCalendarEvents() ([]DashboardEvent, error) {
	return a.getTodayEvents()
}

// StartSession starts a work session for a task.
func (a *App) StartSession(taskGUID string) error {
	if dashboardState == nil {
		if err := initDashboardState(); err != nil {
			return err
		}
	}

	// Get task info (from Thymer or local)
	taskTitle := "Task"
	taskSource := ""

	// Try to get task info from bits table
	if a.db != nil {
		ctx := context.Background()
		bits, err := a.db.GetAllBits(ctx)
		if err == nil {
			for _, b := range bits {
				// Match by Thymer GUID if we have a ref
				// For now, just use the title from the GUID
				if b.MasterURI == taskGUID || b.GetString("thymer_guid") == taskGUID {
					taskTitle = b.Title()
					taskSource = b.GetString("source")
					break
				}
			}
		}
	}

	dashboardState.mu.Lock()
	dashboardState.Session = &SessionState{
		Active:    true,
		TaskGUID:  taskGUID,
		TaskTitle: taskTitle,
		TaskSource: taskSource,
		StartTime: time.Now(),
		Elapsed:   0,
		Paused:    false,
	}
	dashboardState.mu.Unlock()

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active":     true,
			"taskGuid":   taskGUID,
			"taskTitle":  taskTitle,
			"taskSource": taskSource,
			"elapsed":    0,
		})
	}

	return dashboardState.save()
}

// PauseSession pauses the current work session.
func (a *App) PauseSession() error {
	if dashboardState == nil || dashboardState.Session == nil {
		return fmt.Errorf("no active session")
	}

	dashboardState.mu.Lock()
	if !dashboardState.Session.Paused {
		// Calculate elapsed time up to now
		dashboardState.Session.Elapsed += int(time.Since(dashboardState.Session.StartTime).Seconds())
		dashboardState.Session.Paused = true
		now := time.Now()
		dashboardState.Session.PausedAt = &now
	}
	dashboardState.mu.Unlock()

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active":  false,
			"elapsed": dashboardState.Session.Elapsed,
		})
	}

	return dashboardState.save()
}

// ResumeSession resumes a paused session.
func (a *App) ResumeSession() error {
	if dashboardState == nil || dashboardState.Session == nil {
		return fmt.Errorf("no active session")
	}

	dashboardState.mu.Lock()
	if dashboardState.Session.Paused {
		dashboardState.Session.Paused = false
		dashboardState.Session.StartTime = time.Now()
		dashboardState.Session.PausedAt = nil
	}
	dashboardState.mu.Unlock()

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active":     true,
			"taskGuid":   dashboardState.Session.TaskGUID,
			"taskTitle":  dashboardState.Session.TaskTitle,
			"taskSource": dashboardState.Session.TaskSource,
			"elapsed":    dashboardState.Session.Elapsed,
		})
	}

	return dashboardState.save()
}

// CompleteSession completes the current session and marks the task as done.
func (a *App) CompleteSession(taskGUID string, elapsed int) error {
	if dashboardState == nil {
		if err := initDashboardState(); err != nil {
			return err
		}
	}

	dashboardState.mu.Lock()
	taskTitle := ""
	if dashboardState.Session != nil {
		taskTitle = dashboardState.Session.TaskTitle
	}
	// Update stats
	dashboardState.Stats.Completed++
	dashboardState.Stats.TotalTime += elapsed

	// Clear session
	dashboardState.Session = nil
	dashboardState.mu.Unlock()

	// Publish task completion event to JetStream (resilient - queued if Thymer offline)
	if a.eventPublisher != nil {
		ctx := context.Background()
		err := a.eventPublisher.Publish(ctx, events.EventTaskCompleted, map[string]any{
			"task_guid":  taskGUID,
			"task_title": taskTitle,
			"elapsed":    elapsed,
			"completed":  time.Now().Format(time.RFC3339),
		})
		if err != nil {
			slog.Error("failed to publish task completion event", "error", err)
		} else {
			slog.Info("task completion event published", "guid", taskGUID)
		}
	}

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active": false,
		})
	}

	return dashboardState.save()
}

// ToggleHabit toggles a habit's completion status.
func (a *App) ToggleHabit(habitID string) error {
	if dashboardState == nil {
		if err := initDashboardState(); err != nil {
			return err
		}
	}

	dashboardState.mu.Lock()
	for i := range dashboardState.Habits {
		if dashboardState.Habits[i].ID == habitID {
			dashboardState.Habits[i].Done = !dashboardState.Habits[i].Done
			if dashboardState.Habits[i].Done {
				dashboardState.Habits[i].Progress = 100
			} else {
				dashboardState.Habits[i].Progress = 0
			}
			break
		}
	}
	dashboardState.mu.Unlock()

	return dashboardState.save()
}

// =============================================================================
// Internal helpers
// =============================================================================

// getTodayTasks fetches tasks for today from Thymer's daily note and local bits.
func (a *App) getTodayTasks() ([]DashboardTask, error) {
	var tasks []DashboardTask
	ctx := context.Background()

	// PRIMARY: Get tasks from Thymer's daily note (READ path via WebSocket)
	if a.thymer != nil && a.thymer.Available() {
		journal, err := a.thymer.GetTodayJournal(ctx)
		if err != nil {
			slog.Debug("failed to get today's journal", "error", err)
		} else if journal != nil {
			// Extract tasks from line items
			for _, item := range journal.LineItems {
				if item.Type != "task" {
					continue
				}

				// Text is already markdown with [title](thymer:uuid) links
				title := item.Text
				if title == "" {
					continue
				}

				status := "pending"
				if item.Checked {
					status = "done"
				} else if dashboardState != nil && dashboardState.Session != nil &&
					dashboardState.Session.TaskGUID == item.GUID {
					status = "in_progress"
				}

				tasks = append(tasks, DashboardTask{
					GUID:   item.GUID,
					Title:  title,
					Source: "daily",
					Status: status,
				})
			}
		}
	}

	// NOTE: We only show tasks from Thymer's daily note.
	// When not connected, the dashboard shows an empty task list with
	// a prompt to connect to Thymer. We don't fall back to showing all
	// Issues because those aren't "today's planned tasks".

	return tasks, nil
}

// GetConnectionStatus returns the Thymer connection status for the dashboard.
func (a *App) GetConnectionStatus() map[string]any {
	connected := a.thymer != nil && a.thymer.ConnectionCount() > 0
	count := 0
	if a.thymer != nil {
		count = a.thymer.ConnectionCount()
	}
	return map[string]any{
		"connected": connected,
		"count":     count,
	}
}

// getTodayEvents fetches calendar events for today.
func (a *App) getTodayEvents() ([]DashboardEvent, error) {
	var events []DashboardEvent

	// Get events from local bits (Calendar collection)
	if a.db != nil {
		ctx := context.Background()
		bits, err := a.db.GetAllBits(ctx)
		if err == nil {
			today := time.Now().Format("2006-01-02")
			for _, b := range bits {
				// Only include calendar events
				if b.GetString("type") != "event" && !b.HasField("start_time") && !b.HasField("start_date") {
					continue
				}

				// Check if event is today
				startTime := b.GetTime("start_time")
				startDate := b.GetString("start_date")
				if startTime.IsZero() && startDate == "" {
					continue
				}

				eventDate := ""
				if !startTime.IsZero() {
					eventDate = startTime.Format("2006-01-02")
				} else if startDate != "" {
					eventDate = startDate
				}

				if eventDate != today {
					continue
				}

				// Format times
				startStr := ""
				endStr := ""
				duration := ""

				if !startTime.IsZero() {
					startStr = startTime.Format(time.RFC3339)
					if endTime := b.GetTime("end_time"); !endTime.IsZero() {
						endStr = endTime.Format(time.RFC3339)
						dur := endTime.Sub(startTime)
						if dur.Hours() >= 1 {
							duration = fmt.Sprintf("%.0fh", dur.Hours())
						} else {
							duration = fmt.Sprintf("%dm", int(dur.Minutes()))
						}
					}
				}

				events = append(events, DashboardEvent{
					ID:       b.MasterURI,
					Title:    b.Title(),
					Start:    startStr,
					End:      endStr,
					Duration: duration,
					Location: b.GetString("location"),
				})
			}
		}
	}

	return events, nil
}
