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
	"thymer-bar/internal/ui"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// HabitState tracks a habit for today.
// Note: Habits remain file-based for now, sessions use projection.
type HabitState struct {
	mu       sync.RWMutex
	Date     string      `json:"date"`
	Habits   []HabitData `json:"habits"`
	filePath string
}

// HabitData represents a habit.
type HabitData struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Type     string `json:"type"` // "virtue" or "vice"
	Done     bool   `json:"done"`
	Progress int    `json:"progress"` // 0-100
}

// DashboardData is returned to the frontend.
type DashboardData struct {
	Tasks     []DashboardTask  `json:"tasks"`
	Events    []DashboardEvent `json:"events"`
	Habits    []HabitData      `json:"habits"`
	Stats     DashboardStats   `json:"stats"`
	Session   *SessionData     `json:"session,omitempty"`
	Sessions  []SessionData    `json:"sessions,omitempty"` // Completed sessions for timeline
}

// DashboardStats represents daily statistics.
type DashboardStats struct {
	Date      string `json:"date"` // YYYY-MM-DD
	Planned   int    `json:"planned"`
	Completed int    `json:"completed"`
	Queued    int    `json:"queued"`
	TotalTime int    `json:"total_time"` // seconds worked today
}

// DashboardTask represents a task for the dashboard.
type DashboardTask struct {
	GUID          string `json:"guid"`
	Title         string `json:"title"`
	Source        string `json:"source"`
	Status        string `json:"status"` // pending, in_progress, done
	Estimate      string `json:"estimate,omitempty"`
	ScheduledTime string `json:"scheduledTime,omitempty"`
	ElapsedToday  int    `json:"elapsedToday,omitempty"`  // seconds worked today
	SessionCount  int    `json:"sessionCount,omitempty"`  // number of sessions today
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

// SessionData is sent to the frontend.
type SessionData struct {
	ID         string `json:"id,omitempty"`
	Active     bool   `json:"active"`
	Paused     bool   `json:"paused,omitempty"`
	TaskGUID   string `json:"taskGuid"`
	TaskTitle  string `json:"taskTitle"`
	TaskSource string `json:"taskSource,omitempty"`
	Elapsed    int    `json:"elapsed"`
	StartedAt  string `json:"startedAt,omitempty"`
	EndedAt    string `json:"endedAt,omitempty"`
}

var habitState *HabitState

func initHabitState() error {
	stateDir := config.StateDir()
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	habitState = &HabitState{
		Date:     todayDate(),
		Habits:   getDefaultHabits(),
		filePath: filepath.Join(stateDir, "habits.json"),
	}

	// Load existing state
	data, err := os.ReadFile(habitState.filePath)
	if err == nil {
		json.Unmarshal(data, habitState)
	}

	// Reset habits if it's a new day
	if habitState.Date != todayDate() {
		habitState.Date = todayDate()
		for i := range habitState.Habits {
			habitState.Habits[i].Done = false
			habitState.Habits[i].Progress = 0
		}
		habitState.save()
	}

	return nil
}

func getDefaultHabits() []HabitData {
	return []HabitData{
		{ID: "exercise", Name: "Exercise", Icon: "🏃", Type: "virtue", Done: false, Progress: 0},
		{ID: "read", Name: "Read", Icon: "📚", Type: "virtue", Done: false, Progress: 0},
		{ID: "meditate", Name: "Meditate", Icon: "🧘", Type: "virtue", Done: false, Progress: 0},
		{ID: "social-media", Name: "Social Media", Icon: "📱", Type: "vice", Done: false, Progress: 0},
	}
}

func todayDate() string {
	return time.Now().Format("2006-01-02")
}

func (s *HabitState) save() error {
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
	// Ensure habit state is initialized
	if habitState == nil {
		if err := initHabitState(); err != nil {
			return nil, err
		}
	}

	// Get tasks from projection state or Thymer
	tasks, err := a.getTodayTasks()
	if err != nil {
		tasks = []DashboardTask{}
	}

	// Get calendar events
	events, err := a.getTodayEvents()
	if err != nil {
		events = []DashboardEvent{}
	}

	// Get habits
	habitState.mu.RLock()
	habits := make([]HabitData, len(habitState.Habits))
	copy(habits, habitState.Habits)
	habitState.mu.RUnlock()

	// Build session data from projection
	var session *SessionData
	var completedSessions []SessionData

	if a.projection != nil {
		// Get current session
		if currentSession := a.projection.GetCurrentSession(); currentSession != nil {
			session = &SessionData{
				ID:         currentSession.ID,
				Active:     currentSession.Status == "active",
				Paused:     currentSession.Status == "paused",
				TaskGUID:   currentSession.TaskGUID,
				TaskTitle:  currentSession.TaskTitle,
				TaskSource: currentSession.TaskSource,
				Elapsed:    currentSession.Elapsed,
				StartedAt:  currentSession.StartedAt.Format(time.RFC3339),
			}
		}

		// Get completed sessions for timeline
		for _, sess := range a.projection.GetCompletedSessions() {
			endedAt := ""
			if sess.EndedAt != nil {
				endedAt = sess.EndedAt.Format(time.RFC3339)
			}
			completedSessions = append(completedSessions, SessionData{
				ID:         sess.ID,
				Active:     false,
				TaskGUID:   sess.TaskGUID,
				TaskTitle:  sess.TaskTitle,
				TaskSource: sess.TaskSource,
				Elapsed:    sess.Elapsed,
				StartedAt:  sess.StartedAt.Format(time.RFC3339),
				EndedAt:    endedAt,
			})
		}

		// Get stats from projection
		stats := a.projection.GetTodayStats()

		return &DashboardData{
			Tasks:    tasks,
			Events:   events,
			Habits:   habits,
			Stats:    DashboardStats{
				Date:      stats.Date,
				Planned:   len(tasks),
				Completed: countCompleted(tasks),
				Queued:    len(tasks) - countCompleted(tasks),
				TotalTime: stats.TotalSeconds,
			},
			Session:  session,
			Sessions: completedSessions,
		}, nil
	}

	// Fallback if projection not initialized
	return &DashboardData{
		Tasks:   tasks,
		Events:  events,
		Habits:  habits,
		Stats:   DashboardStats{Date: todayDate(), Planned: len(tasks), Completed: countCompleted(tasks), Queued: len(tasks) - countCompleted(tasks)},
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
			GUID:         t.GUID,
			Title:        t.Title,
			Source:       t.Source,
			Status:       t.Status,
			Estimate:     t.Estimate,
			Time:         t.ScheduledTime,
			ElapsedToday: t.ElapsedToday,
			SessionCount: t.SessionCount,
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

	// Stats from data
	uiData.Stats = ui.Stats{
		Planned:   data.Stats.Planned,
		Completed: data.Stats.Completed,
		Queued:    data.Stats.Queued,
	}

	// Convert session
	if data.Session != nil && (data.Session.Active || data.Session.Paused) {
		uiData.Session = &ui.Session{
			Active:    data.Session.Active,
			Paused:    data.Session.Paused,
			TaskGUID:  data.Session.TaskGUID,
			TaskTitle: data.Session.TaskTitle,
			Elapsed:   data.Session.Elapsed,
		}
	}

	// Convert completed sessions for timeline
	for _, s := range data.Sessions {
		uiData.Sessions = append(uiData.Sessions, ui.CompletedSession{
			ID:        s.ID,
			TaskGUID:  s.TaskGUID,
			TaskTitle: s.TaskTitle,
			StartedAt: s.StartedAt,
			EndedAt:   s.EndedAt,
			Elapsed:   s.Elapsed,
		})
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
	if a.projection == nil {
		return fmt.Errorf("projection not initialized")
	}

	// Get task info from projection state or Thymer
	taskTitle := "Task"
	taskSource := "daily"

	// Try to get task title from projection's daily tasks
	if tasks := a.projection.GetTodayTasks(); tasks != nil {
		if task, exists := tasks[taskGUID]; exists {
			taskTitle = task.Markdown
		}
	}

	// Start session via projection (which publishes events)
	ctx := context.Background()
	session, err := a.projection.StartSession(ctx, taskGUID, taskTitle, taskSource)
	if err != nil {
		return err
	}

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active":     true,
			"taskGuid":   session.TaskGUID,
			"taskTitle":  session.TaskTitle,
			"taskSource": session.TaskSource,
			"elapsed":    0,
		})
	}

	return nil
}

// PauseSession pauses the current work session.
func (a *App) PauseSession() error {
	if a.projection == nil {
		return fmt.Errorf("projection not initialized")
	}

	ctx := context.Background()
	if err := a.projection.PauseSession(ctx); err != nil {
		return err
	}

	// Get updated session info
	session := a.projection.GetCurrentSession()
	elapsed := 0
	if session != nil {
		elapsed = session.Elapsed
	}

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active":  false,
			"paused":  true,
			"elapsed": elapsed,
		})
	}

	return nil
}

// ResumeSession resumes a paused session.
func (a *App) ResumeSession() error {
	if a.projection == nil {
		return fmt.Errorf("projection not initialized")
	}

	ctx := context.Background()
	if err := a.projection.ResumeSession(ctx); err != nil {
		return err
	}

	// Get updated session info
	session := a.projection.GetCurrentSession()
	if session == nil {
		return fmt.Errorf("no session to resume")
	}

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active":     true,
			"taskGuid":   session.TaskGUID,
			"taskTitle":  session.TaskTitle,
			"taskSource": session.TaskSource,
			"elapsed":    session.Elapsed,
		})
	}

	return nil
}

// CompleteSession completes the current session and marks the task as done.
func (a *App) CompleteSession(taskGUID string, elapsed int) error {
	if a.projection == nil {
		return fmt.Errorf("projection not initialized")
	}

	ctx := context.Background()
	_, err := a.projection.CompleteSession(ctx)
	if err != nil {
		return err
	}

	// Emit session update to frontend
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "session:update", map[string]any{
			"active": false,
		})
	}

	return nil
}

// ToggleHabit toggles a habit's completion status.
func (a *App) ToggleHabit(habitID string) error {
	if habitState == nil {
		if err := initHabitState(); err != nil {
			return err
		}
	}

	habitState.mu.Lock()
	for i := range habitState.Habits {
		if habitState.Habits[i].ID == habitID {
			habitState.Habits[i].Done = !habitState.Habits[i].Done
			if habitState.Habits[i].Done {
				habitState.Habits[i].Progress = 100
			} else {
				habitState.Habits[i].Progress = 0
			}
			break
		}
	}
	habitState.mu.Unlock()

	return habitState.save()
}

// GetSessionHistory returns session history for a date range.
func (a *App) GetSessionHistory(date string) ([]SessionData, error) {
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	ctx := context.Background()
	sessions, err := a.db.GetSessionsByDate(ctx, date)
	if err != nil {
		return nil, err
	}

	var result []SessionData
	for _, s := range sessions {
		endedAt := ""
		if s.EndedAt != nil {
			endedAt = s.EndedAt.Format(time.RFC3339)
		}
		result = append(result, SessionData{
			ID:         s.ID,
			Active:     s.Status == "active",
			Paused:     s.Status == "paused",
			TaskGUID:   s.TaskGUID,
			TaskTitle:  s.TaskTitle,
			TaskSource: s.TaskSource,
			Elapsed:    s.ElapsedSeconds,
			StartedAt:  s.StartedAt.Format(time.RFC3339),
			EndedAt:    endedAt,
		})
	}

	return result, nil
}

// =============================================================================
// Internal helpers
// =============================================================================

// getTodayTasks fetches tasks for today from projection state or Thymer.
func (a *App) getTodayTasks() ([]DashboardTask, error) {
	var tasks []DashboardTask
	ctx := context.Background()

	// First, try to get tasks from projection state (populated by push from Thymer)
	if a.projection != nil {
		projTasks := a.projection.GetTodayTasksOrdered() // Use ordered version
		currentSession := a.projection.GetCurrentSession()
		elapsedByTask := a.projection.GetElapsedByTask()
		sessionCounts := a.projection.GetSessionCountByTask()

		for _, task := range projTasks {
			status := "pending"
			if task.Status == "done" {
				status = "done"
			} else if currentSession != nil && currentSession.TaskGUID == task.GUID {
				status = "in_progress"
			}

			tasks = append(tasks, DashboardTask{
				GUID:         task.GUID,
				Title:        task.Markdown,
				Source:       "daily",
				Status:       status,
				ElapsedToday: elapsedByTask[task.GUID],
				SessionCount: sessionCounts[task.GUID],
			})
		}

		// If we have tasks from projection, use them
		if len(tasks) > 0 {
			return tasks, nil
		}
	}

	// Fallback: Get tasks from Thymer's daily note directly (READ path via WebSocket)
	if a.thymer != nil && a.thymer.Available() {
		journal, err := a.thymer.GetTodayJournal(ctx)
		if err != nil {
			slog.Debug("failed to get today's journal", "error", err)
		} else if journal != nil {
			currentSession := a.projection.GetCurrentSession()

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
				} else if currentSession != nil && currentSession.TaskGUID == item.GUID {
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

// NavigateToRecord sends a navigate message to Thymer to open a record.
func (a *App) NavigateToRecord(guid string) error {
	if a.thymer == nil || a.thymer.ConnectionCount() == 0 {
		return fmt.Errorf("not connected to Thymer")
	}

	// Send navigate message to Thymer
	ctx := context.Background()
	msg := map[string]any{
		"type":   "navigate",
		"target": "GUID:" + guid,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal navigate message: %w", err)
	}

	slog.Info("navigating to record", "guid", guid)
	return a.thymer.SendRaw(ctx, data)
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
