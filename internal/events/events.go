// Package events provides event publishing and consumption via JetStream.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Event represents an event to be processed.
type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data"`
}

// Event types - 12 core events for session tracking and planning
const (
	// ==========================================================================
	// Line events (from Thymer observations)
	// ==========================================================================
	EventLineUpdated = "line.updated" // {guid, markdown, status, updated_at}
	EventLineRemoved = "line.removed" // {guid}

	// ==========================================================================
	// Session events (focus timer from thymer-bar UI)
	// ==========================================================================
	EventSessionStarted   = "session.started"   // {session_id, task_guid, task_title}
	EventSessionPaused    = "session.paused"    // {session_id, elapsed_seconds}
	EventSessionResumed   = "session.resumed"   // {session_id}
	EventSessionCompleted = "session.completed" // {session_id, task_guid, elapsed_seconds}

	// ==========================================================================
	// Planning events (thymer-bar UI actions)
	// ==========================================================================
	EventTaskReordered   = "task.reordered"   // {guid, after_guid} - drag in queue
	EventTaskScheduled   = "task.scheduled"   // {guid, time} - drag to timeline
	EventTaskUnscheduled = "task.unscheduled" // {guid} - remove from timeline
	EventTaskDuration    = "task.duration"    // {guid, duration} - resize on timeline
	EventTaskCompleted   = "task.completed"   // {guid} - mark done in thymer-bar
	EventTaskUncompleted = "task.uncompleted" // {guid} - mark not done in thymer-bar

	// ==========================================================================
	// Legacy aliases (for migration, will remove later)
	// ==========================================================================
	EventDailyTaskFound   = EventLineUpdated
	EventDailyTaskUpdated = EventLineUpdated
	EventDailyTaskRemoved = EventLineRemoved
)

// Publisher publishes events to JetStream.
type Publisher struct {
	js     jetstream.JetStream
	stream string
}

// NewPublisher creates a new event publisher.
func NewPublisher(js jetstream.JetStream, stream string) *Publisher {
	return &Publisher{js: js, stream: stream}
}

// Publish publishes an event to the stream.
func (p *Publisher) Publish(ctx context.Context, eventType string, data map[string]any) error {
	event := Event{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	subject := fmt.Sprintf("events.%s", eventType)
	_, err = p.js.Publish(ctx, subject, payload)
	if err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	slog.Debug("event published", "type", eventType, "id", event.ID)
	return nil
}

// DayStreamName returns the stream name for a given date (e.g., "day.20250119").
func DayStreamName(date time.Time) string {
	return fmt.Sprintf("day.%s", date.Format("20060102"))
}

// TodayStreamName returns the stream name for today.
func TodayStreamName() string {
	return DayStreamName(time.Now())
}

// PublishToDay publishes an event to the day-partitioned stream.
func (p *Publisher) PublishToDay(ctx context.Context, eventType string, data map[string]any) error {
	event := Event{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Publish to today's stream
	subject := TodayStreamName()
	_, err = p.js.Publish(ctx, subject, payload)
	if err != nil {
		return fmt.Errorf("failed to publish event to day stream: %w", err)
	}

	slog.Debug("event published to day stream", "type", eventType, "id", event.ID, "stream", subject)
	return nil
}

// Consumer consumes events from JetStream.
type Consumer struct {
	js       jetstream.JetStream
	stream   string
	consumer jetstream.Consumer
	handler  func(context.Context, *Event) error
	stopCh   chan struct{}
}

// NewConsumer creates a new event consumer with a filter subject.
func NewConsumer(js jetstream.JetStream, stream, consumerName string, handler func(context.Context, *Event) error) (*Consumer, error) {
	// Create or get the consumer
	consumer, err := js.CreateOrUpdateConsumer(context.Background(), stream, jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		FilterSubject: "events.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	return &Consumer{
		js:       js,
		stream:   stream,
		consumer: consumer,
		handler:  handler,
		stopCh:   make(chan struct{}),
	}, nil
}

// NewDayConsumer creates a consumer for the DAYS stream (day.YYYYMMDD subjects).
func NewDayConsumer(js jetstream.JetStream, stream, consumerName string, handler func(context.Context, *Event) error) (*Consumer, error) {
	// Create or get the consumer - matches all day.* subjects
	consumer, err := js.CreateOrUpdateConsumer(context.Background(), stream, jetstream.ConsumerConfig{
		Name:          consumerName,
		Durable:       consumerName,
		FilterSubject: "day.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy, // Only process new events (not replay on restart)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create day consumer: %w", err)
	}

	return &Consumer{
		js:       js,
		stream:   stream,
		consumer: consumer,
		handler:  handler,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start starts consuming events.
func (c *Consumer) Start(ctx context.Context) error {
	slog.Info("starting event consumer")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopCh:
			return nil
		default:
			// Fetch messages with timeout
			msgs, err := c.consumer.Fetch(10, jetstream.FetchMaxWait(5*time.Second))
			if err != nil {
				// Timeout is expected, continue
				continue
			}

			for msg := range msgs.Messages() {
				var event Event
				if err := json.Unmarshal(msg.Data(), &event); err != nil {
					slog.Error("failed to unmarshal event", "error", err)
					msg.Nak()
					continue
				}

				// Process the event
				if err := c.handler(ctx, &event); err != nil {
					slog.Error("failed to process event", "type", event.Type, "error", err)
					// NAK to retry later
					msg.NakWithDelay(30 * time.Second)
					continue
				}

				// Acknowledge successful processing
				msg.Ack()
				slog.Debug("event processed", "type", event.Type, "id", event.ID)
			}
		}
	}
}

// Stop stops the consumer.
func (c *Consumer) Stop() {
	close(c.stopCh)
}

// ReplayTodayEvents fetches and replays all events from today's stream.
// This should be called on startup to rebuild state before processing new events.
func ReplayTodayEvents(js jetstream.JetStream, stream string, handler func(context.Context, *Event) error) error {
	ctx := context.Background()
	todaySubject := TodayStreamName()

	slog.Info("replaying today's events", "subject", todaySubject)

	// Create a temporary ephemeral consumer to replay events
	consumerName := fmt.Sprintf("replay-%d", time.Now().UnixNano())
	consumer, err := js.CreateOrUpdateConsumer(ctx, stream, jetstream.ConsumerConfig{
		Name:          consumerName,
		FilterSubject: todaySubject,
		AckPolicy:     jetstream.AckNonePolicy, // Don't need acks for replay
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to create replay consumer: %w", err)
	}

	// Fetch all messages (with a reasonable limit)
	msgs, err := consumer.Fetch(1000, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		slog.Debug("no events to replay or fetch timeout", "error", err)
		return nil // Not an error if no events
	}

	count := 0
	for msg := range msgs.Messages() {
		var event Event
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			slog.Error("failed to unmarshal event during replay", "error", err)
			continue
		}

		// Process the event (replay mode - apply to state)
		if err := handler(ctx, &event); err != nil {
			slog.Error("failed to process event during replay", "type", event.Type, "error", err)
			continue
		}
		count++
	}

	// Delete the temporary consumer
	js.DeleteConsumer(ctx, stream, consumerName)

	slog.Info("replayed events", "count", count, "subject", todaySubject)
	return nil
}
