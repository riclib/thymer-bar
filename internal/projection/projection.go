// Package projection manages in-memory state derived from events.
package projection

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"thymer-bar/internal/events"
	"thymer-bar/internal/sqlite"

	"github.com/google/uuid"
)

// =============================================================================
// State - the projection derived from events
// =============================================================================

// State holds the in-memory projection state derived from events.
type State struct {
	mu sync.RWMutex

	// Task state (from line.* and task.* events)
	Tasks     map[string]*Task // keyed by GUID
	TaskOrder []string         // order in queue

	// Session state (from session.* events)
	CurrentSession    *Session
	CompletedSessions []*Session

	// Stats (derived)
	TodayStats *DailyStats

	// Internal
	currentDate string
	colorCounter int // for assigning rotating colors to tasks
	db          *sqlite.DB
	publisher   *events.Publisher
}

// Task represents a task from Thymer's daily note, enriched with planning data.
type Task struct {
	// From Thymer (line.updated events)
	GUID      string    `json:"guid"`
	Markdown  string    `json:"markdown"`
	Status    string    `json:"status"`     // "unchecked", "in-progress", "done"
	UpdatedAt time.Time `json:"updated_at"` // Thymer's timestamp

	// From planning events (thymer-bar UI)
	ScheduledTime *string    `json:"scheduled_time,omitempty"` // "14:00" (legacy, single slot)
	Duration      *string    `json:"duration,omitempty"`       // "30m", "1h"
	CompletedAt   *time.Time `json:"completed_at,omitempty"`

	// Multi-slot planning (drag-and-drop)
	PlannedSlots []PlannedSlot `json:"planned_slots,omitempty"` // Multiple time slots
	Color        int           `json:"color"`                   // 0-9 for pastel rotation
}

// PlannedSlot represents a single scheduled time slot for a task
type PlannedSlot struct {
	StartTime string `json:"start_time"` // "14:00" (24h format)
	Duration  int    `json:"duration"`   // minutes
}

// Session represents a work session.
type Session struct {
	ID         string     `json:"id"`
	TaskGUID   string     `json:"task_guid"`
	TaskTitle  string     `json:"task_title"`
	TaskSource string     `json:"task_source,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	PausedAt   *time.Time `json:"paused_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Elapsed    int        `json:"elapsed"` // accumulated seconds
	Status     string     `json:"status"`  // "active", "paused", "completed"
}

// DailyStats represents aggregated daily statistics.
type DailyStats struct {
	Date           string `json:"date"`
	TasksFound     int    `json:"tasks_found"`
	TasksCompleted int    `json:"tasks_completed"`
	TotalSeconds   int    `json:"total_seconds"`
	SessionCount   int    `json:"session_count"`
}

// =============================================================================
// Constructor and Initialization
// =============================================================================

// NewState creates a new projection state.
func NewState(db *sqlite.DB, publisher *events.Publisher) *State {
	today := time.Now().Format("2006-01-02")
	return &State{
		Tasks:             make(map[string]*Task),
		TaskOrder:         []string{},
		CompletedSessions: []*Session{},
		TodayStats:        &DailyStats{Date: today},
		currentDate:       today,
		db:                db,
		publisher:         publisher,
	}
}

// Initialize prepares empty state for event replay.
// State is rebuilt entirely from JetStream events - SQLite is only for analytics.
func (s *State) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	zone, offset := now.Zone()
	today := now.Format("2006-01-02")
	s.currentDate = today
	s.TodayStats = &DailyStats{Date: today}

	// Sessions and stats will be rebuilt from event replay.
	// SQLite is no longer read on startup - it's only for historical analytics.
	// This ensures JetStream events are the single source of truth.

	slog.Info("projection initialized (awaiting event replay)",
		"date", today,
		"now", now.Format(time.RFC3339),
		"timezone", zone,
		"offsetSeconds", offset)

	return nil
}

// CheckDayChange resets state if the date has changed.
func (s *State) CheckDayChange() {
	now := time.Now()
	today := now.Format("2006-01-02")
	if today != s.currentDate {
		s.mu.Lock()
		defer s.mu.Unlock()

		zone, offset := now.Zone()
		slog.Info("day changed, resetting state",
			"old", s.currentDate,
			"new", today,
			"now", now.Format(time.RFC3339),
			"timezone", zone,
			"offsetSeconds", offset)

		if s.CurrentSession != nil {
			s.CurrentSession.Status = "abandoned"
			s.CurrentSession = nil
		}

		s.currentDate = today
		s.Tasks = make(map[string]*Task)
		s.TaskOrder = []string{}
		s.CompletedSessions = []*Session{}
		s.TodayStats = &DailyStats{Date: today}
	}
}

// =============================================================================
// Session Management
// =============================================================================

// StartSession starts a new work session for a task.
func (s *State) StartSession(ctx context.Context, taskGUID, taskTitle, taskSource string) (*Session, error) {
	s.CheckDayChange()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.CurrentSession != nil && s.CurrentSession.Status == "active" {
		return nil, fmt.Errorf("session already active")
	}

	// If there's a paused session, move it to completed sessions to preserve elapsed time
	if s.CurrentSession != nil && s.CurrentSession.Status == "paused" {
		now := time.Now()
		prevSession := s.CurrentSession
		prevSession.Status = "completed"
		prevSession.EndedAt = &now
		s.CompletedSessions = append(s.CompletedSessions, prevSession)

		// Update stats
		s.TodayStats.TotalSeconds += prevSession.Elapsed
		s.TodayStats.SessionCount++

		// Publish event (sync - required for event sourcing durability)
		if s.publisher != nil {
			s.publisher.PublishToDay(ctx, events.EventSessionCompleted, map[string]any{
				"session_id":      prevSession.ID,
				"task_guid":       prevSession.TaskGUID,
				"elapsed_seconds": prevSession.Elapsed,
			})
		}

		// Write to SQLite (async - just for analytics, not source of truth)
		if s.db != nil {
			db := s.db
			date := s.currentDate
			elapsed := prevSession.Elapsed
			sessionID := prevSession.ID
			go func() {
				db.UpdateSessionStatus(context.Background(), sessionID, "completed", elapsed, &now)
				db.IncrementDailyStats(context.Background(), date, 0, elapsed, 1)
			}()
		}

		slog.Info("paused session auto-completed", "id", prevSession.ID, "elapsed", prevSession.Elapsed)
		s.CurrentSession = nil
	}

	session := &Session{
		ID:         uuid.New().String(),
		TaskGUID:   taskGUID,
		TaskTitle:  taskTitle,
		TaskSource: taskSource,
		StartedAt:  time.Now(),
		Status:     "active",
		Elapsed:    0,
	}
	s.CurrentSession = session

	// Publish event (sync - required for event sourcing durability)
	if s.publisher != nil {
		s.publisher.PublishToDay(ctx, events.EventSessionStarted, map[string]any{
			"session_id":  session.ID,
			"task_guid":   taskGUID,
			"task_title":  taskTitle,
			"task_source": taskSource,
		})
	}

	// Write to SQLite (async - just for analytics)
	if s.db != nil {
		db := s.db
		date := s.currentDate
		go func() {
			db.UpsertSession(context.Background(), &sqlite.Session{
				ID:         session.ID,
				TaskGUID:   taskGUID,
				TaskTitle:  taskTitle,
				TaskSource: taskSource,
				StartedAt:  session.StartedAt,
				Status:     "active",
				Date:       date,
			})
		}()
	}

	slog.Info("session started", "id", session.ID, "task", taskTitle)
	return session, nil
}

// PauseSession pauses the current session.
func (s *State) PauseSession(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.CurrentSession == nil || s.CurrentSession.Status != "active" {
		return fmt.Errorf("no active session to pause")
	}

	now := time.Now()
	elapsed := s.CurrentSession.Elapsed + int(now.Sub(s.CurrentSession.StartedAt).Seconds())
	s.CurrentSession.Elapsed = elapsed
	s.CurrentSession.PausedAt = &now
	s.CurrentSession.Status = "paused"

	sessionID := s.CurrentSession.ID

	// Publish event (sync - required for event sourcing durability)
	if s.publisher != nil {
		s.publisher.PublishToDay(ctx, events.EventSessionPaused, map[string]any{
			"session_id":      sessionID,
			"elapsed_seconds": elapsed,
		})
	}

	// Write to SQLite (async - just for analytics)
	if s.db != nil {
		db := s.db
		go func() {
			db.UpdateSessionStatus(context.Background(), sessionID, "paused", elapsed, nil)
		}()
	}

	slog.Info("session paused", "id", sessionID, "elapsed", elapsed)
	return nil
}

// ResumeSession resumes a paused session.
func (s *State) ResumeSession(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.CurrentSession == nil || s.CurrentSession.Status != "paused" {
		return fmt.Errorf("no paused session to resume")
	}

	s.CurrentSession.StartedAt = time.Now()
	s.CurrentSession.PausedAt = nil
	s.CurrentSession.Status = "active"

	sessionID := s.CurrentSession.ID
	elapsed := s.CurrentSession.Elapsed

	// Publish event (sync - required for event sourcing durability)
	if s.publisher != nil {
		s.publisher.PublishToDay(ctx, events.EventSessionResumed, map[string]any{
			"session_id": sessionID,
		})
	}

	// Write to SQLite (async - just for analytics)
	if s.db != nil {
		db := s.db
		go func() {
			db.UpdateSessionStatus(context.Background(), sessionID, "active", elapsed, nil)
		}()
	}

	slog.Info("session resumed", "id", sessionID)
	return nil
}

// CompleteSession completes the current session.
func (s *State) CompleteSession(ctx context.Context) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.CurrentSession == nil {
		return nil, fmt.Errorf("no session to complete")
	}

	now := time.Now()
	elapsed := s.CurrentSession.Elapsed
	if s.CurrentSession.Status == "active" {
		elapsed += int(now.Sub(s.CurrentSession.StartedAt).Seconds())
	}

	s.CurrentSession.Elapsed = elapsed
	s.CurrentSession.EndedAt = &now
	s.CurrentSession.Status = "completed"

	completed := s.CurrentSession
	s.CompletedSessions = append(s.CompletedSessions, completed)
	s.CurrentSession = nil

	// Update stats
	s.TodayStats.TasksCompleted++
	s.TodayStats.TotalSeconds += elapsed
	s.TodayStats.SessionCount++

	// Publish event (sync - required for event sourcing durability)
	if s.publisher != nil {
		s.publisher.PublishToDay(ctx, events.EventSessionCompleted, map[string]any{
			"session_id":      completed.ID,
			"task_guid":       completed.TaskGUID,
			"elapsed_seconds": elapsed,
		})
	}

	// Write to SQLite (async - just for analytics)
	if s.db != nil {
		db := s.db
		date := s.currentDate
		sessionID := completed.ID
		go func() {
			db.UpdateSessionStatus(context.Background(), sessionID, "completed", elapsed, &now)
			db.IncrementDailyStats(context.Background(), date, 1, elapsed, 1)
		}()
	}

	slog.Info("session completed", "id", completed.ID, "elapsed", elapsed)
	return completed, nil
}

// =============================================================================
// Getters
// =============================================================================

// GetCurrentSession returns the current session with calculated elapsed time.
func (s *State) GetCurrentSession() *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.CurrentSession == nil {
		return nil
	}

	session := *s.CurrentSession
	if session.Status == "active" {
		session.Elapsed += int(time.Since(session.StartedAt).Seconds())
	}
	return &session
}

// GetCompletedSessions returns completed sessions for today.
func (s *State) GetCompletedSessions() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*Session, len(s.CompletedSessions))
	for i, sess := range s.CompletedSessions {
		copy := *sess
		sessions[i] = &copy
	}
	return sessions
}

// GetTodayStats returns today's stats.
func (s *State) GetTodayStats() *DailyStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.TodayStats == nil {
		return &DailyStats{Date: s.currentDate}
	}
	stats := *s.TodayStats
	return &stats
}

// GetTasksOrdered returns all tasks in queue order.
// Merges tasks from Thymer push with tasks from session history for resilience.
func (s *State) GetTasksOrdered() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build task map from multiple sources for resilience
	taskMap := make(map[string]*Task)
	var taskOrder []string
	seen := make(map[string]bool)

	// 1. First, add tasks from Thymer push (s.TaskOrder preserves Thymer's order)
	for _, guid := range s.TaskOrder {
		if task, exists := s.Tasks[guid]; exists {
			copy := *task
			taskMap[guid] = &copy
			taskOrder = append(taskOrder, guid)
			seen[guid] = true
		}
	}

	// 2. Add current session's task (if not already included)
	if s.CurrentSession != nil && !seen[s.CurrentSession.TaskGUID] {
		taskMap[s.CurrentSession.TaskGUID] = &Task{
			GUID:     s.CurrentSession.TaskGUID,
			Markdown: s.CurrentSession.TaskTitle,
			Status:   "unchecked",
		}
		taskOrder = append(taskOrder, s.CurrentSession.TaskGUID)
		seen[s.CurrentSession.TaskGUID] = true
	}

	// 3. Add tasks from completed sessions (if not already included)
	for _, session := range s.CompletedSessions {
		if !seen[session.TaskGUID] {
			taskMap[session.TaskGUID] = &Task{
				GUID:     session.TaskGUID,
				Markdown: session.TaskTitle,
				Status:   "unchecked",
			}
			taskOrder = append(taskOrder, session.TaskGUID)
			seen[session.TaskGUID] = true
		}
	}

	// Build final task list in order
	tasks := make([]*Task, 0, len(taskOrder))
	for _, guid := range taskOrder {
		tasks = append(tasks, taskMap[guid])
	}
	return tasks
}

// GroupedTasks holds tasks separated by category for queue display.
type GroupedTasks struct {
	Scheduled []*Task // Has PlannedSlots, sorted by earliest start time
	Floating  []*Task // No PlannedSlots, not done, user-ordered
	Done      []*Task // Status == "done"
}

// GetTasksGrouped returns tasks grouped by category for queue display.
// - Scheduled: tasks with planned slots, sorted by earliest start time
// - Floating: tasks without planned slots that aren't done, in user order
// - Done: completed tasks
func (s *State) GetTasksGrouped() GroupedTasks {
	allTasks := s.GetTasksOrdered()

	var scheduled, floating, done []*Task

	for _, task := range allTasks {
		if task.Status == "done" {
			done = append(done, task)
		} else if len(task.PlannedSlots) > 0 {
			scheduled = append(scheduled, task)
		} else {
			floating = append(floating, task)
		}
	}

	// Sort scheduled tasks by earliest start time
	sort.Slice(scheduled, func(i, j int) bool {
		return earliestSlotTime(scheduled[i]) < earliestSlotTime(scheduled[j])
	})

	return GroupedTasks{
		Scheduled: scheduled,
		Floating:  floating,
		Done:      done,
	}
}

// earliestSlotTime returns the earliest start time from a task's planned slots.
// Returns "99:99" if no slots (sorts to end).
func earliestSlotTime(task *Task) string {
	if len(task.PlannedSlots) == 0 {
		return "99:99"
	}
	earliest := task.PlannedSlots[0].StartTime
	for _, slot := range task.PlannedSlots[1:] {
		if slot.StartTime < earliest {
			earliest = slot.StartTime
		}
	}
	return earliest
}

// GetTask returns a task by GUID.
func (s *State) GetTask(guid string) *Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if task, exists := s.Tasks[guid]; exists {
		copy := *task
		return &copy
	}
	return nil
}

// =============================================================================
// Planning - schedule tasks to timeline
// =============================================================================

// ScheduleTask schedules a task to a specific time slot.
// Returns error if there's a conflict with existing events/tasks.
func (s *State) ScheduleTask(ctx context.Context, guid, startTime string, durationMinutes int) error {
	slog.Info("[projection] ScheduleTask called",
		"guid", guid,
		"startTime", startTime,
		"durationMinutes", durationMinutes,
		"currentDate", s.currentDate)

	s.CheckDayChange()
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.Tasks[guid]
	if !exists {
		slog.Error("[projection] ScheduleTask failed - task not found",
			"guid", guid,
			"totalTasks", len(s.Tasks))
		return fmt.Errorf("task not found: %s", guid)
	}

	slog.Info("[projection] Task found",
		"guid", guid,
		"markdown", task.Markdown[:min(30, len(task.Markdown))],
		"existingSlots", len(task.PlannedSlots),
		"color", task.Color)

	// Assign color if not set
	if task.Color == 0 && len(task.PlannedSlots) == 0 {
		s.colorCounter++
		task.Color = s.colorCounter % 10
		slog.Info("[projection] Assigned color", "color", task.Color)
	}

	// Check for conflicts (simplified - could be enhanced)
	startMinutes := parseTimeToMinutes(startTime)
	endMinutes := startMinutes + durationMinutes

	slog.Info("[projection] Checking conflicts",
		"startMinutes", startMinutes,
		"endMinutes", endMinutes,
		"asTime", fmt.Sprintf("%02d:%02d - %02d:%02d", startMinutes/60, startMinutes%60, endMinutes/60, endMinutes%60))

	for taskGuid, t := range s.Tasks {
		for _, slot := range t.PlannedSlots {
			slotStart := parseTimeToMinutes(slot.StartTime)
			slotEnd := slotStart + slot.Duration
			// Check overlap
			if startMinutes < slotEnd && endMinutes > slotStart {
				slog.Warn("[projection] Conflict detected",
					"conflictingTask", taskGuid,
					"conflictSlot", slot.StartTime,
					"conflictDuration", slot.Duration)
				return fmt.Errorf("conflict with existing planned task")
			}
		}
	}

	slog.Info("[projection] No conflicts, publishing event",
		"eventType", events.EventTaskScheduled,
		"hasPublisher", s.publisher != nil)

	// Publish event
	if s.publisher != nil {
		err := s.publisher.PublishToDay(ctx, events.EventTaskScheduled, map[string]any{
			"guid":     guid,
			"time":     startTime,
			"duration": float64(durationMinutes),
		})
		if err != nil {
			slog.Error("[projection] Failed to publish event", "error", err)
		} else {
			slog.Info("[projection] Event published successfully")
		}
	}

	// Apply locally
	task.PlannedSlots = append(task.PlannedSlots, PlannedSlot{
		StartTime: startTime,
		Duration:  durationMinutes,
	})
	task.ScheduledTime = &startTime

	slog.Info("[projection] Task scheduled successfully",
		"guid", guid,
		"time", startTime,
		"duration", durationMinutes,
		"totalSlots", len(task.PlannedSlots))
	return nil
}

// UnscheduleTask removes a task from a specific time slot (or all slots).
func (s *State) UnscheduleTask(ctx context.Context, guid, startTime string) error {
	s.CheckDayChange()
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.Tasks[guid]
	if !exists {
		return fmt.Errorf("task not found: %s", guid)
	}

	// Publish event
	if s.publisher != nil {
		s.publisher.PublishToDay(ctx, events.EventTaskUnscheduled, map[string]any{
			"guid": guid,
			"time": startTime,
		})
	}

	// Apply locally
	if startTime != "" {
		newSlots := make([]PlannedSlot, 0, len(task.PlannedSlots))
		for _, slot := range task.PlannedSlots {
			if slot.StartTime != startTime {
				newSlots = append(newSlots, slot)
			}
		}
		task.PlannedSlots = newSlots
	} else {
		task.PlannedSlots = nil
	}

	if len(task.PlannedSlots) == 0 {
		task.ScheduledTime = nil
	}

	slog.Info("task unscheduled", "guid", guid, "time", startTime)
	return nil
}

// ResizeTask changes the duration of a planned slot.
func (s *State) ResizeTask(ctx context.Context, guid, startTime string, newDuration int) error {
	s.CheckDayChange()
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.Tasks[guid]
	if !exists {
		return fmt.Errorf("task not found: %s", guid)
	}

	// Enforce minimum 15 minutes
	if newDuration < 15 {
		newDuration = 15
	}

	// Publish event
	if s.publisher != nil {
		s.publisher.PublishToDay(ctx, events.EventTaskDuration, map[string]any{
			"guid":     guid,
			"time":     startTime,
			"duration": float64(newDuration),
		})
	}

	// Apply locally
	for i := range task.PlannedSlots {
		if task.PlannedSlots[i].StartTime == startTime {
			task.PlannedSlots[i].Duration = newDuration
			break
		}
	}

	slog.Info("task resized", "guid", guid, "time", startTime, "duration", newDuration)
	return nil
}

// ReorderTask moves a task to a new position in the queue.
func (s *State) ReorderTask(ctx context.Context, guid, afterGuid string) error {
	s.CheckDayChange()
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify task exists
	if _, exists := s.Tasks[guid]; !exists {
		return fmt.Errorf("task not found: %s", guid)
	}

	// Publish event
	if s.publisher != nil {
		s.publisher.PublishToDay(ctx, events.EventTaskReordered, map[string]any{
			"guid":       guid,
			"after_guid": afterGuid,
		})
	}

	// Apply locally - remove from current position
	for i, g := range s.TaskOrder {
		if g == guid {
			s.TaskOrder = append(s.TaskOrder[:i], s.TaskOrder[i+1:]...)
			break
		}
	}

	// Insert at new position
	if afterGuid == "" {
		s.TaskOrder = append([]string{guid}, s.TaskOrder...)
	} else {
		for i, g := range s.TaskOrder {
			if g == afterGuid {
				s.TaskOrder = append(s.TaskOrder[:i+1], append([]string{guid}, s.TaskOrder[i+1:]...)...)
				break
			}
		}
	}

	slog.Info("task reordered", "guid", guid, "after", afterGuid)
	return nil
}

// GetPlannedSlots returns all planned slots for a given task.
func (s *State) GetPlannedSlots(guid string) []PlannedSlot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if task, exists := s.Tasks[guid]; exists {
		slots := make([]PlannedSlot, len(task.PlannedSlots))
		copy(slots, task.PlannedSlots)
		return slots
	}
	return nil
}

// GetAllPlannedSlots returns all planned slots across all tasks.
func (s *State) GetAllPlannedSlots() map[string][]PlannedSlot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]PlannedSlot)
	for guid, task := range s.Tasks {
		if len(task.PlannedSlots) > 0 {
			slots := make([]PlannedSlot, len(task.PlannedSlots))
			copy(slots, task.PlannedSlots)
			result[guid] = slots
		}
	}
	return result
}

// parseTimeToMinutes converts "HH:MM" to minutes since midnight
func parseTimeToMinutes(timeStr string) int {
	// Parse "14:00" or "14:30" format
	var hour, minute int
	fmt.Sscanf(timeStr, "%d:%d", &hour, &minute)
	return hour*60 + minute
}

// =============================================================================
// Apply - rebuild state from events
// =============================================================================

// Apply applies an event to update the in-memory state.
// This is used when rebuilding state from the event log.
func (s *State) Apply(event *events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch event.Type {

	// -------------------------------------------------------------------------
	// Line events (from Thymer)
	// -------------------------------------------------------------------------
	case events.EventLineUpdated:
		guid, _ := event.Data["guid"].(string)
		markdown, _ := event.Data["markdown"].(string)
		status, _ := event.Data["status"].(string)

		task, exists := s.Tasks[guid]
		if !exists {
			task = &Task{GUID: guid}
			s.Tasks[guid] = task
			s.TaskOrder = append(s.TaskOrder, guid)
			s.TodayStats.TasksFound++
		}
		task.Markdown = markdown
		task.Status = status
		task.UpdatedAt = event.Timestamp

	case events.EventLineRemoved:
		guid, _ := event.Data["guid"].(string)
		delete(s.Tasks, guid)
		for i, g := range s.TaskOrder {
			if g == guid {
				s.TaskOrder = append(s.TaskOrder[:i], s.TaskOrder[i+1:]...)
				break
			}
		}

	// -------------------------------------------------------------------------
	// Session events
	// -------------------------------------------------------------------------
	case events.EventSessionStarted:
		sessionID, _ := event.Data["session_id"].(string)
		taskGUID, _ := event.Data["task_guid"].(string)
		taskTitle, _ := event.Data["task_title"].(string)
		taskSource, _ := event.Data["task_source"].(string)

		s.CurrentSession = &Session{
			ID:         sessionID,
			TaskGUID:   taskGUID,
			TaskTitle:  taskTitle,
			TaskSource: taskSource,
			StartedAt:  event.Timestamp,
			Status:     "active",
		}

	case events.EventSessionPaused:
		if s.CurrentSession != nil {
			elapsed, _ := event.Data["elapsed_seconds"].(float64)
			s.CurrentSession.Elapsed = int(elapsed)
			s.CurrentSession.PausedAt = &event.Timestamp
			s.CurrentSession.Status = "paused"
		}

	case events.EventSessionResumed:
		if s.CurrentSession != nil {
			s.CurrentSession.StartedAt = event.Timestamp
			s.CurrentSession.PausedAt = nil
			s.CurrentSession.Status = "active"
		}

	case events.EventSessionCompleted:
		sessionID, _ := event.Data["session_id"].(string)

		// Check for duplicate completion (already in CompletedSessions)
		isDuplicate := false
		for _, sess := range s.CompletedSessions {
			if sess.ID == sessionID {
				isDuplicate = true
				break
			}
		}

		if !isDuplicate && s.CurrentSession != nil && s.CurrentSession.ID == sessionID {
			elapsed, _ := event.Data["elapsed_seconds"].(float64)
			s.CurrentSession.Elapsed = int(elapsed)
			s.CurrentSession.EndedAt = &event.Timestamp
			s.CurrentSession.Status = "completed"
			s.CompletedSessions = append(s.CompletedSessions, s.CurrentSession)
			s.CurrentSession = nil

			s.TodayStats.TasksCompleted++
			s.TodayStats.TotalSeconds += int(elapsed)
			s.TodayStats.SessionCount++
		}

	// -------------------------------------------------------------------------
	// Planning events (thymer-bar UI)
	// -------------------------------------------------------------------------
	case events.EventTaskReordered:
		guid, _ := event.Data["guid"].(string)
		afterGUID, _ := event.Data["after_guid"].(string)

		// Remove from current position
		for i, g := range s.TaskOrder {
			if g == guid {
				s.TaskOrder = append(s.TaskOrder[:i], s.TaskOrder[i+1:]...)
				break
			}
		}

		// Insert after afterGUID (or at start if empty)
		if afterGUID == "" {
			s.TaskOrder = append([]string{guid}, s.TaskOrder...)
		} else {
			for i, g := range s.TaskOrder {
				if g == afterGUID {
					s.TaskOrder = append(s.TaskOrder[:i+1], append([]string{guid}, s.TaskOrder[i+1:]...)...)
					break
				}
			}
		}

	case events.EventTaskScheduled:
		guid, _ := event.Data["guid"].(string)
		timeStr, _ := event.Data["time"].(string)
		durationMin, _ := event.Data["duration"].(float64)
		if durationMin == 0 {
			durationMin = 30 // default 30 minutes
		}
		if task, exists := s.Tasks[guid]; exists {
			// Add to PlannedSlots
			task.PlannedSlots = append(task.PlannedSlots, PlannedSlot{
				StartTime: timeStr,
				Duration:  int(durationMin),
			})
			// Legacy support
			task.ScheduledTime = &timeStr
		}

	case events.EventTaskUnscheduled:
		guid, _ := event.Data["guid"].(string)
		timeStr, _ := event.Data["time"].(string)
		if task, exists := s.Tasks[guid]; exists {
			if timeStr != "" {
				// Remove specific slot
				newSlots := make([]PlannedSlot, 0, len(task.PlannedSlots))
				for _, slot := range task.PlannedSlots {
					if slot.StartTime != timeStr {
						newSlots = append(newSlots, slot)
					}
				}
				task.PlannedSlots = newSlots
			} else {
				// Remove all slots (legacy behavior)
				task.PlannedSlots = nil
			}
			if len(task.PlannedSlots) == 0 {
				task.ScheduledTime = nil
			}
		}

	case events.EventTaskDuration:
		guid, _ := event.Data["guid"].(string)
		timeStr, _ := event.Data["time"].(string)
		durationMin, _ := event.Data["duration"].(float64)
		durationStr, _ := event.Data["duration_str"].(string)
		if task, exists := s.Tasks[guid]; exists {
			// Update specific slot
			for i := range task.PlannedSlots {
				if task.PlannedSlots[i].StartTime == timeStr {
					task.PlannedSlots[i].Duration = int(durationMin)
					break
				}
			}
			// Legacy support
			if durationStr != "" {
				task.Duration = &durationStr
			}
		}

	case events.EventTaskCompleted:
		guid, _ := event.Data["guid"].(string)
		if task, exists := s.Tasks[guid]; exists {
			task.Status = "done"
			now := event.Timestamp
			task.CompletedAt = &now
		}

	case events.EventTaskUncompleted:
		guid, _ := event.Data["guid"].(string)
		if task, exists := s.Tasks[guid]; exists {
			task.Status = "unchecked"
			task.CompletedAt = nil
		}
	}
}

// =============================================================================
// Handle Thymer Line Snapshot (the proper event sourcing way)
// =============================================================================

// LineSnapshot represents a snapshot of lines from Thymer.
type LineSnapshot struct {
	Date       string         `json:"date"`
	ObservedAt time.Time      `json:"observed_at"`
	Lines      []LineObserved `json:"lines"`
}

// LineObserved represents a single line observed from Thymer.
type LineObserved struct {
	GUID      string    `json:"guid"`
	Markdown  string    `json:"markdown"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ApplyLineSnapshot processes a snapshot from Thymer and publishes events for changes.
// This is the proper event sourcing approach - compare timestamps, publish only new events.
func (s *State) ApplyLineSnapshot(ctx context.Context, snapshot *LineSnapshot) {
	s.CheckDayChange()
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build set of GUIDs in snapshot
	snapshotGUIDs := make(map[string]bool)
	for _, line := range snapshot.Lines {
		snapshotGUIDs[line.GUID] = true
	}

	// Build set of existing task orders to check for missing tasks
	existingOrder := make(map[string]bool)
	for _, guid := range s.TaskOrder {
		existingOrder[guid] = true
	}

	// Process each line - compare with projection, publish if newer
	for i, line := range snapshot.Lines {
		existing := s.Tasks[line.GUID]

		// Always ensure task is in TaskOrder (even if timestamps match)
		if !existingOrder[line.GUID] {
			s.TaskOrder = append(s.TaskOrder, line.GUID)
			existingOrder[line.GUID] = true
		}

		// Check if content changed (status is most important for Thymer as source of truth)
		statusChanged := existing != nil && existing.Status != line.Status
		contentChanged := existing != nil && existing.Markdown != line.Markdown
		isNewer := existing == nil || line.UpdatedAt.After(existing.UpdatedAt)
		shouldUpdate := existing == nil || isNewer || statusChanged || contentChanged

		slog.Debug("ApplyLineSnapshot processing",
			"guid", line.GUID,
			"status", line.Status,
			"existingStatus", func() string { if existing != nil { return existing.Status }; return "nil" }(),
			"lineUpdatedAt", line.UpdatedAt,
			"existingUpdatedAt", func() time.Time { if existing != nil { return existing.UpdatedAt }; return time.Time{} }(),
			"isNewer", isNewer,
			"statusChanged", statusChanged,
			"shouldUpdate", shouldUpdate)

		if shouldUpdate {
			// New or updated - publish event to day stream
			if s.publisher != nil {
				s.publisher.PublishToDay(ctx, events.EventLineUpdated, map[string]any{
					"guid":       line.GUID,
					"markdown":   line.Markdown,
					"status":     line.Status,
					"updated_at": time.Now().UnixMilli(), // Use current time for events
					"order":      i,
				})
			}

			// Update local state
			if existing == nil {
				s.Tasks[line.GUID] = &Task{
					GUID:      line.GUID,
					Markdown:  line.Markdown,
					Status:    line.Status,
					UpdatedAt: time.Now(), // Use current time
				}
				s.TodayStats.TasksFound++
			} else {
				existing.Markdown = line.Markdown
				existing.Status = line.Status
				existing.UpdatedAt = time.Now() // Use current time when updating
			}
		}
	}

	// Check for removed lines
	for guid := range s.Tasks {
		if !snapshotGUIDs[guid] {
			if s.publisher != nil {
				s.publisher.PublishToDay(ctx, events.EventLineRemoved, map[string]any{
					"guid": guid,
				})
			}

			delete(s.Tasks, guid)
			for i, g := range s.TaskOrder {
				if g == guid {
					s.TaskOrder = append(s.TaskOrder[:i], s.TaskOrder[i+1:]...)
					break
				}
			}
		}
	}

	// Rebuild order from snapshot (Thymer is authoritative for order until user reorders in thymer-bar)
	s.TaskOrder = make([]string, 0, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		s.TaskOrder = append(s.TaskOrder, line.GUID)
	}

	slog.Info("line snapshot applied",
		"lines", len(snapshot.Lines),
		"tasks", len(s.Tasks))
}

// =============================================================================
// Convenience Accessors
// =============================================================================

// GetTodayTasksOrdered returns tasks in queue order (alias for GetTasksOrdered).
func (s *State) GetTodayTasksOrdered() []*Task {
	return s.GetTasksOrdered()
}

// GetTodayTasks returns tasks as a map keyed by GUID.
func (s *State) GetTodayTasks() map[string]*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make(map[string]*Task, len(s.Tasks))
	for guid, task := range s.Tasks {
		copy := *task
		tasks[guid] = &copy
	}
	return tasks
}

// GetElapsedByTask returns the total elapsed seconds today for each task.
// This sums all completed sessions plus any active/paused session for each task.
func (s *State) GetElapsedByTask() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	elapsed := make(map[string]int)

	// Sum completed sessions
	for _, session := range s.CompletedSessions {
		elapsed[session.TaskGUID] += session.Elapsed
	}

	// Add current session (if any)
	if s.CurrentSession != nil {
		e := s.CurrentSession.Elapsed
		if s.CurrentSession.Status == "active" {
			e += int(time.Since(s.CurrentSession.StartedAt).Seconds())
		}
		elapsed[s.CurrentSession.TaskGUID] += e
	}

	return elapsed
}

// GetSessionCountByTask returns the number of sessions today for each task.
func (s *State) GetSessionCountByTask() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]int)

	for _, session := range s.CompletedSessions {
		counts[session.TaskGUID]++
	}

	if s.CurrentSession != nil {
		counts[s.CurrentSession.TaskGUID]++
	}

	return counts
}
