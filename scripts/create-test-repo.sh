#!/usr/bin/env bash
# create-test-repo.sh — Generates a harder test repo for AtlasKB pipeline benchmarking.
# Creates an "eventbus" project (~2200 lines of Go) designed to stress every known extractor weakness.
set -euo pipefail

REPO_DIR="/tmp/atlaskb-test-repo"

echo "Creating test repo at $REPO_DIR..."
rm -rf "$REPO_DIR"
mkdir -p "$REPO_DIR"
cd "$REPO_DIR"
git init -q
git checkout -b main 2>/dev/null || true

# Helper: commit with a fixed date for reproducible git history
commit() {
    local msg="$1"
    local date="$2"
    GIT_AUTHOR_DATE="$date" GIT_COMMITTER_DATE="$date" \
        git add -A && git commit -q -m "$msg" --date="$date"
}

# ============================================================
# Commit 1: Initial project setup
# ============================================================
cat > go.mod << 'GOMOD'
module github.com/example/eventbus

go 1.22

require (
	github.com/google/uuid v1.6.0
	golang.org/x/sync v0.6.0
)
GOMOD

cat > README.md << 'README'
# EventBus

An event-driven message bus for Go applications.

## Features
- Pub/sub with topic-based routing
- Pluggable storage backends (memory, file)
- Middleware chain (logging, auth, retry)
- Worker pool for async event processing
- Generic Result type for error handling
README

commit "Initial project setup" "2024-01-15T10:00:00"

# ============================================================
# Commit 2: Add core event types and topic constants
# ============================================================
mkdir -p pkg/event

cat > pkg/event/event.go << 'EOF'
// Package event defines the core event types and topic constants for the event bus.
package event

import (
	"time"

	"github.com/google/uuid"
)

// Priority levels for events.
type Priority int

const (
	PriorityLow    Priority = 0
	PriorityNormal Priority = 1
	PriorityHigh   Priority = 2
	PriorityCritical Priority = 3
)

// Topic constants for well-known event categories.
const (
	TopicUserCreated  = "user.created"
	TopicUserUpdated  = "user.updated"
	TopicUserDeleted  = "user.deleted"
	TopicOrderCreated = "order.created"
	TopicOrderPaid    = "order.paid"
	TopicSystemHealth = "system.health"
	TopicAuditLog     = "audit.log"
)

// Event represents a single event in the system.
type Event struct {
	ID        uuid.UUID              `json:"id"`
	Topic     string                 `json:"topic"`
	Payload   map[string]interface{} `json:"payload"`
	Priority  Priority               `json:"priority"`
	Source    string                 `json:"source"`
	CreatedAt time.Time              `json:"created_at"`
	Metadata  map[string]string      `json:"metadata,omitempty"`
}

// New creates a new Event with a generated ID and current timestamp.
func New(topic string, payload map[string]interface{}) *Event {
	return &Event{
		ID:        uuid.New(),
		Topic:     topic,
		Payload:   payload,
		Priority:  PriorityNormal,
		CreatedAt: time.Now(),
		Metadata:  make(map[string]string),
	}
}

// WithPriority sets the event priority and returns the event for chaining.
func (e *Event) WithPriority(p Priority) *Event {
	e.Priority = p
	return e
}

// WithSource sets the event source and returns the event for chaining.
func (e *Event) WithSource(source string) *Event {
	e.Source = source
	return e
}

// WithMetadata adds a metadata key-value pair.
func (e *Event) WithMetadata(key, value string) *Event {
	e.Metadata[key] = value
	return e
}

// IsHighPriority returns true if the event has high or critical priority.
func (e *Event) IsHighPriority() bool {
	return e.Priority >= PriorityHigh
}

// TopicPrefix returns the first segment of the topic (before the first dot).
func (e *Event) TopicPrefix() string {
	for i, c := range e.Topic {
		if c == '.' {
			return e.Topic[:i]
		}
	}
	return e.Topic
}
EOF

commit "Add core event types and topic constants" "2024-01-17T14:00:00"

# ============================================================
# Commit 3: Add generic Result type for error handling
# ============================================================
cat > pkg/event/result.go << 'EOF'
package event

import "fmt"

// Result is a generic type that represents either a success value or an error.
// It provides a monadic interface for chaining operations that may fail.
type Result[T any] struct {
	value T
	err   error
	ok    bool
}

// OK creates a successful Result containing the given value.
func OK[T any](value T) Result[T] {
	return Result[T]{value: value, ok: true}
}

// Err creates a failed Result containing the given error.
func Err[T any](err error) Result[T] {
	return Result[T]{err: err, ok: false}
}

// IsOK returns true if the Result contains a success value.
func (r Result[T]) IsOK() bool {
	return r.ok
}

// Unwrap returns the success value or panics if the Result is an error.
func (r Result[T]) Unwrap() T {
	if !r.ok {
		panic(fmt.Sprintf("called Unwrap on error Result: %v", r.err))
	}
	return r.value
}

// UnwrapOr returns the success value or the provided default if the Result is an error.
func (r Result[T]) UnwrapOr(def T) T {
	if r.ok {
		return r.value
	}
	return def
}

// Error returns the error, or nil if the Result is OK.
func (r Result[T]) Error() error {
	return r.err
}

// Map transforms the success value using the given function.
// If the Result is an error, the function is not called.
func Map[T, U any](r Result[T], fn func(T) U) Result[U] {
	if !r.ok {
		return Err[U](r.err)
	}
	return OK(fn(r.value))
}

// FlatMap transforms the success value using a function that returns a Result.
func FlatMap[T, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if !r.ok {
		return Err[U](r.err)
	}
	return fn(r.value)
}
EOF

commit "Add generic Result type for error handling" "2024-01-19T09:00:00"

# ============================================================
# Commit 4: Add storage interface and memory implementation
# ============================================================
mkdir -p pkg/storage

cat > pkg/storage/storage.go << 'EOF'
// Package storage defines the Storage interface and implementations for event persistence.
package storage

import (
	"errors"

	"github.com/example/eventbus/pkg/event"
	"github.com/google/uuid"
)

// ErrNotFound is returned when a requested event does not exist.
var ErrNotFound = errors.New("event not found")

// Storage defines the interface for persisting and retrieving events.
type Storage interface {
	// Save persists an event to storage.
	Save(e *event.Event) error
	// Get retrieves an event by ID.
	Get(id uuid.UUID) (*event.Event, error)
	// Delete removes an event by ID.
	Delete(id uuid.UUID) error
	// List returns all events for a given topic, ordered by creation time.
	List(topic string) ([]*event.Event, error)
	// Count returns the total number of stored events.
	Count() (int, error)
	// Name returns the storage backend name for logging/diagnostics.
	Name() string
}
EOF

cat > pkg/storage/memory.go << 'EOF'
package storage

import (
	"sort"
	"sync"

	"github.com/example/eventbus/pkg/event"
	"github.com/google/uuid"
)

// MemoryStorage implements Storage using an in-memory map.
// It is safe for concurrent use.
type MemoryStorage struct {
	mu     sync.RWMutex
	events map[uuid.UUID]*event.Event
}

// NewMemoryStorage creates a new empty MemoryStorage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		events: make(map[uuid.UUID]*event.Event),
	}
}

// Save persists an event in memory.
func (m *MemoryStorage) Save(e *event.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[e.ID] = e
	return nil
}

// Get retrieves an event by ID from memory.
func (m *MemoryStorage) Get(id uuid.UUID) (*event.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.events[id]
	if !ok {
		return nil, ErrNotFound
	}
	return e, nil
}

// Delete removes an event by ID from memory.
func (m *MemoryStorage) Delete(id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.events[id]; !ok {
		return ErrNotFound
	}
	delete(m.events, id)
	return nil
}

// List returns all events matching the given topic, sorted by creation time.
func (m *MemoryStorage) List(topic string) ([]*event.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*event.Event
	for _, e := range m.events {
		if e.Topic == topic {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// Count returns the total number of events in memory.
func (m *MemoryStorage) Count() (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.events), nil
}

// Name returns "memory" as the storage backend identifier.
func (m *MemoryStorage) Name() string {
	return "memory"
}
EOF

commit "Add storage interface and memory implementation" "2024-01-22T11:00:00"

# ============================================================
# Commit 5: Add file-based storage backend
# ============================================================
cat > pkg/storage/file.go << 'EOF'
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/example/eventbus/pkg/event"
	"github.com/google/uuid"
)

// FileStorage implements Storage by writing events as JSON files to a directory.
// Each event is stored as a separate file named by its UUID.
type FileStorage struct {
	mu      sync.RWMutex
	baseDir string
}

// NewFileStorage creates a FileStorage rooted at the given directory.
// The directory is created if it does not exist.
func NewFileStorage(baseDir string) (*FileStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("creating storage directory: %w", err)
	}
	return &FileStorage{baseDir: baseDir}, nil
}

// Save writes an event to disk as a JSON file.
func (f *FileStorage) Save(e *event.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling event: %w", err)
	}
	path := f.eventPath(e.ID)
	return os.WriteFile(path, data, 0644)
}

// Get reads an event from disk by its UUID.
func (f *FileStorage) Get(id uuid.UUID) (*event.Event, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.readEvent(id)
}

// Delete removes an event file from disk.
func (f *FileStorage) Delete(id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.eventPath(id)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return ErrNotFound
	}
	return os.Remove(path)
}

// List reads all event files and returns those matching the given topic.
func (f *FileStorage) List(topic string) ([]*event.Event, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	entries, err := os.ReadDir(f.baseDir)
	if err != nil {
		return nil, fmt.Errorf("reading storage directory: %w", err)
	}
	var result []*event.Event
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id, err := uuid.Parse(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		e, err := f.readEvent(id)
		if err != nil {
			continue // skip corrupted files
		}
		if e.Topic == topic {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

// Count returns the number of event files in the storage directory.
func (f *FileStorage) Count() (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	entries, err := os.ReadDir(f.baseDir)
	if err != nil {
		return 0, fmt.Errorf("reading storage directory: %w", err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count, nil
}

// Name returns "file" as the storage backend identifier.
func (f *FileStorage) Name() string {
	return "file"
}

func (f *FileStorage) eventPath(id uuid.UUID) string {
	return filepath.Join(f.baseDir, id.String()+".json")
}

func (f *FileStorage) readEvent(id uuid.UUID) (*event.Event, error) {
	data, err := os.ReadFile(f.eventPath(id))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading event file: %w", err)
	}
	var e event.Event
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("unmarshaling event: %w", err)
	}
	return &e, nil
}
EOF

commit "Add file-based storage backend" "2024-01-25T16:00:00"

# ============================================================
# Commit 6: Add event bus with pub/sub and channel-based dispatch
# ============================================================
mkdir -p pkg/bus

cat > pkg/bus/bus.go << 'BUSEOF'
// Package bus provides the core event bus with pub/sub, channel-based dispatch,
// topic filtering, and priority queue support.
package bus

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/eventbus/pkg/event"
	"github.com/example/eventbus/pkg/storage"
	"github.com/google/uuid"
)

// Handler is a function that processes an event.
type Handler func(ctx context.Context, e *event.Event) error

// Subscription represents a topic subscription with its handler.
type Subscription struct {
	ID      uuid.UUID
	Topic   string
	Handler Handler
	Filter  func(*event.Event) bool // optional filter predicate
}

// BusConfig holds configuration for the event bus.
type BusConfig struct {
	BufferSize     int
	WorkerCount    int
	RetryAttempts  int
	RetryDelay     time.Duration
	EnablePriority bool
}

// DefaultConfig returns a BusConfig with sensible defaults.
func DefaultConfig() BusConfig {
	return BusConfig{
		BufferSize:     1024,
		WorkerCount:    4,
		RetryAttempts:  3,
		RetryDelay:     100 * time.Millisecond,
		EnablePriority: true,
	}
}

// Stats holds runtime statistics for the bus.
type Stats struct {
	Published    atomic.Int64
	Delivered    atomic.Int64
	Failed       atomic.Int64
	Retried      atomic.Int64
	Dropped      atomic.Int64
	ActiveSubs   atomic.Int64
}

// Bus is the central event bus that manages subscriptions and dispatches events.
type Bus struct {
	mu           sync.RWMutex
	subs         map[string][]*Subscription
	allSubs      []*Subscription // for wildcard matching
	store        storage.Storage
	config       BusConfig
	stats        Stats
	eventCh      chan *event.Event
	priorityCh   chan *event.Event
	done         chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	middlewares  []func(Handler) Handler
	logger      interface{} // accepts internal/logger.Logger
}

// New creates a new Bus with the given storage backend and configuration.
func New(store storage.Storage, config BusConfig) *Bus {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bus{
		subs:       make(map[string][]*Subscription),
		store:      store,
		config:     config,
		eventCh:    make(chan *event.Event, config.BufferSize),
		priorityCh: make(chan *event.Event, config.BufferSize/4),
		done:       make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
	}
	return b
}

// Start begins the bus event processing loop with the configured number of workers.
func (b *Bus) Start() {
	for i := 0; i < b.config.WorkerCount; i++ {
		go b.processLoop(i)
	}
	log.Printf("[bus] started with %d workers, buffer=%d", b.config.WorkerCount, b.config.BufferSize)
}

// Stop gracefully shuts down the bus, waiting for in-flight events to complete.
func (b *Bus) Stop() {
	b.cancel()
	close(b.eventCh)
	close(b.priorityCh)
	<-b.done
	log.Println("[bus] stopped")
}

// Subscribe registers a handler for the given topic pattern.
// Topic patterns support wildcards: "user.*" matches "user.created", "user.updated", etc.
func (b *Bus) Subscribe(topic string, handler Handler) *Subscription {
	sub := &Subscription{
		ID:      uuid.New(),
		Topic:   topic,
		Handler: handler,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if strings.Contains(topic, "*") {
		b.allSubs = append(b.allSubs, sub)
	} else {
		b.subs[topic] = append(b.subs[topic], sub)
	}
	b.stats.ActiveSubs.Add(1)
	return sub
}

// SubscribeWithFilter registers a handler with an additional filter predicate.
func (b *Bus) SubscribeWithFilter(topic string, handler Handler, filter func(*event.Event) bool) *Subscription {
	sub := b.Subscribe(topic, handler)
	sub.Filter = filter
	return sub
}

// Unsubscribe removes a subscription by ID.
func (b *Bus) Unsubscribe(id uuid.UUID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for topic, subs := range b.subs {
		for i, sub := range subs {
			if sub.ID == id {
				b.subs[topic] = append(subs[:i], subs[i+1:]...)
				b.stats.ActiveSubs.Add(-1)
				return true
			}
		}
	}

	for i, sub := range b.allSubs {
		if sub.ID == id {
			b.allSubs = append(b.allSubs[:i], b.allSubs[i+1:]...)
			b.stats.ActiveSubs.Add(-1)
			return true
		}
	}
	return false
}

// Publish sends an event to all matching subscribers.
// The event is persisted to storage before dispatch.
func (b *Bus) Publish(ctx context.Context, e *event.Event) error {
	// Persist first
	if err := b.store.Save(e); err != nil {
		return fmt.Errorf("persisting event: %w", err)
	}
	b.stats.Published.Add(1)

	// Route to appropriate channel
	if b.config.EnablePriority && e.IsHighPriority() {
		select {
		case b.priorityCh <- e:
		default:
			b.stats.Dropped.Add(1)
			return fmt.Errorf("priority channel full, event dropped: %s", e.ID)
		}
	} else {
		select {
		case b.eventCh <- e:
		default:
			b.stats.Dropped.Add(1)
			return fmt.Errorf("event channel full, event dropped: %s", e.ID)
		}
	}
	return nil
}

// PublishBatch sends multiple events, returning the first error encountered.
func (b *Bus) PublishBatch(ctx context.Context, events []*event.Event) error {
	// Sort by priority (highest first)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Priority > events[j].Priority
	})
	for _, e := range events {
		if err := b.Publish(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// GetStats returns a snapshot of bus statistics.
func (b *Bus) GetStats() map[string]int64 {
	return map[string]int64{
		"published":   b.stats.Published.Load(),
		"delivered":   b.stats.Delivered.Load(),
		"failed":      b.stats.Failed.Load(),
		"retried":     b.stats.Retried.Load(),
		"dropped":     b.stats.Dropped.Load(),
		"active_subs": b.stats.ActiveSubs.Load(),
	}
}

// Use adds a middleware to the bus. Middlewares wrap handlers in LIFO order.
func (b *Bus) Use(mw func(Handler) Handler) {
	b.middlewares = append(b.middlewares, mw)
}

// processLoop is the main event processing goroutine.
func (b *Bus) processLoop(workerID int) {
	defer func() {
		if workerID == 0 {
			close(b.done)
		}
	}()

	for {
		select {
		case e, ok := <-b.priorityCh:
			if !ok {
				return
			}
			b.dispatch(b.ctx, e)
		case e, ok := <-b.eventCh:
			if !ok {
				return
			}
			b.dispatch(b.ctx, e)
		case <-b.ctx.Done():
			return
		}
	}
}

// dispatch delivers an event to all matching subscribers.
func (b *Bus) dispatch(ctx context.Context, e *event.Event) {
	b.mu.RLock()
	handlers := b.matchingHandlers(e)
	b.mu.RUnlock()

	for _, sub := range handlers {
		if sub.Filter != nil && !sub.Filter(e) {
			continue
		}

		handler := sub.Handler
		// Apply middlewares in reverse order
		for i := len(b.middlewares) - 1; i >= 0; i-- {
			handler = b.middlewares[i](handler)
		}

		if err := b.executeWithRetry(ctx, handler, e); err != nil {
			b.stats.Failed.Add(1)
			log.Printf("[bus] handler failed for event %s on topic %s: %v", e.ID, e.Topic, err)
		} else {
			b.stats.Delivered.Add(1)
		}
	}
}

// matchingHandlers returns all subscriptions that match the event's topic.
func (b *Bus) matchingHandlers(e *event.Event) []*Subscription {
	var matched []*Subscription

	// Exact match
	if subs, ok := b.subs[e.Topic]; ok {
		matched = append(matched, subs...)
	}

	// Wildcard match
	for _, sub := range b.allSubs {
		if topicMatches(sub.Topic, e.Topic) {
			matched = append(matched, sub)
		}
	}

	return matched
}

// topicMatches checks if a pattern (possibly with wildcards) matches a topic.
func topicMatches(pattern, topic string) bool {
	if pattern == "*" {
		return true
	}
	patternParts := strings.Split(pattern, ".")
	topicParts := strings.Split(topic, ".")

	if len(patternParts) != len(topicParts) {
		return false
	}
	for i, pp := range patternParts {
		if pp == "*" {
			continue
		}
		if pp != topicParts[i] {
			return false
		}
	}
	return true
}

// executeWithRetry runs a handler with the configured retry policy.
func (b *Bus) executeWithRetry(ctx context.Context, handler Handler, e *event.Event) error {
	var lastErr error
	for attempt := 0; attempt <= b.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			b.stats.Retried.Add(1)
			select {
			case <-time.After(b.config.RetryDelay * time.Duration(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := handler(ctx, e); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// generateID creates a new unique identifier. Used internally for correlation IDs.
func generateID() string {
	return uuid.New().String()
}

// cleanup removes expired or processed events from storage.
// TODO: Implement TTL-based cleanup — currently a no-op.
func cleanup(store storage.Storage, maxAge time.Duration) error {
	// FIXME: This needs access to event timestamps which requires a storage scan.
	// For now, this is a placeholder that should be implemented before production use.
	return nil
}

// drainChannel reads all remaining events from a channel without processing.
// NOTE: This discards events silently — only use during shutdown when persistence
// has already been handled.
func drainChannel(ch <-chan *event.Event) int {
	count := 0
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return count
			}
			count++
		default:
			return count
		}
	}
}

// validateTopic checks that a topic string follows the expected format.
// TODO: Add regex validation for topic format (e.g., "segment.segment.segment")
func validateTopic(topic string) error {
	if topic == "" {
		return fmt.Errorf("topic cannot be empty")
	}
	if strings.HasPrefix(topic, ".") || strings.HasSuffix(topic, ".") {
		return fmt.Errorf("topic cannot start or end with a dot: %q", topic)
	}
	return nil
}
BUSEOF

commit "Add event bus with pub/sub and channel-based dispatch" "2024-01-30T10:00:00"

# ============================================================
# Commit 7: Add middleware chain with logging, auth, and retry
# ============================================================
mkdir -p pkg/middleware

cat > pkg/middleware/middleware.go << 'EOF'
// Package middleware provides composable event processing middleware.
package middleware

import (
	"context"

	"github.com/example/eventbus/pkg/event"
)

// Middleware is a function that wraps a handler to add cross-cutting behavior.
type Middleware func(next HandlerFunc) HandlerFunc

// HandlerFunc processes a single event.
type HandlerFunc func(ctx context.Context, e *event.Event) error

// BaseMiddleware provides shared functionality for all middleware implementations.
type BaseMiddleware struct {
	Name    string
	Enabled bool
}

// IsEnabled returns whether this middleware is active.
func (b *BaseMiddleware) IsEnabled() bool {
	return b.Enabled
}

// GetName returns the middleware name for logging/diagnostics.
func (b *BaseMiddleware) GetName() string {
	return b.Name
}

// Chain applies a sequence of middlewares to a handler, in order.
// The first middleware in the slice is the outermost wrapper.
func Chain(handler HandlerFunc, middlewares ...Middleware) HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}
EOF

cat > pkg/middleware/logging.go << 'EOF'
package middleware

import (
	"context"
	"log"
	"time"

	"github.com/example/eventbus/pkg/event"
)

// LoggingMiddleware logs event processing duration and errors.
// It embeds BaseMiddleware for shared middleware behavior.
type LoggingMiddleware struct {
	BaseMiddleware
	LogPayload bool // whether to include event payload in logs
}

// NewLoggingMiddleware creates a logging middleware with the given configuration.
func NewLoggingMiddleware(logPayload bool) *LoggingMiddleware {
	return &LoggingMiddleware{
		BaseMiddleware: BaseMiddleware{Name: "logging", Enabled: true},
		LogPayload:     logPayload,
	}
}

// Wrap returns a Middleware function that adds logging to a handler.
func (l *LoggingMiddleware) Wrap() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, e *event.Event) error {
			if !l.IsEnabled() {
				return next(ctx, e)
			}
			start := time.Now()
			log.Printf("[%s] processing event %s on topic %s", l.GetName(), e.ID, e.Topic)
			err := next(ctx, e)
			duration := time.Since(start)
			if err != nil {
				log.Printf("[%s] event %s FAILED after %v: %v", l.GetName(), e.ID, duration, err)
			} else {
				log.Printf("[%s] event %s completed in %v", l.GetName(), e.ID, duration)
			}
			return err
		}
	}
}
EOF

cat > pkg/middleware/auth.go << 'EOF'
package middleware

import (
	"context"
	"fmt"

	"github.com/example/eventbus/pkg/event"
)

// AuthMiddleware validates that events have a valid source before processing.
// It embeds BaseMiddleware for shared middleware behavior.
type AuthMiddleware struct {
	BaseMiddleware
	AllowedSources map[string]bool
}

// NewAuthMiddleware creates an auth middleware with the given allowed sources.
func NewAuthMiddleware(sources []string) *AuthMiddleware {
	allowed := make(map[string]bool, len(sources))
	for _, s := range sources {
		allowed[s] = true
	}
	return &AuthMiddleware{
		BaseMiddleware: BaseMiddleware{Name: "auth", Enabled: true},
		AllowedSources: allowed,
	}
}

// Wrap returns a Middleware function that validates event sources.
func (a *AuthMiddleware) Wrap() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, e *event.Event) error {
			if !a.IsEnabled() {
				return next(ctx, e)
			}
			if e.Source == "" {
				return fmt.Errorf("auth: event %s has no source", e.ID)
			}
			if !a.AllowedSources[e.Source] {
				return fmt.Errorf("auth: source %q not allowed for event %s", e.Source, e.ID)
			}
			return next(ctx, e)
		}
	}
}

// AddSource adds a source to the allowed list at runtime.
func (a *AuthMiddleware) AddSource(source string) {
	a.AllowedSources[source] = true
}

// RemoveSource removes a source from the allowed list.
func (a *AuthMiddleware) RemoveSource(source string) {
	delete(a.AllowedSources, source)
}
EOF

cat > pkg/middleware/retry.go << 'EOF'
package middleware

import (
	"context"
	"log"
	"time"

	"github.com/example/eventbus/pkg/event"
)

// RetryMiddleware retries failed event handlers with exponential backoff.
// It embeds BaseMiddleware for shared middleware behavior.
type RetryMiddleware struct {
	BaseMiddleware
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// NewRetryMiddleware creates a retry middleware with the given configuration.
func NewRetryMiddleware(maxRetries int, baseDelay time.Duration) *RetryMiddleware {
	return &RetryMiddleware{
		BaseMiddleware: BaseMiddleware{Name: "retry", Enabled: true},
		MaxRetries:     maxRetries,
		BaseDelay:      baseDelay,
		MaxDelay:       30 * time.Second,
	}
}

// Wrap returns a Middleware function that retries failed handlers.
func (r *RetryMiddleware) Wrap() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, e *event.Event) error {
			if !r.IsEnabled() {
				return next(ctx, e)
			}
			var lastErr error
			for attempt := 0; attempt <= r.MaxRetries; attempt++ {
				if attempt > 0 {
					delay := r.calculateDelay(attempt)
					log.Printf("[%s] retrying event %s (attempt %d/%d, delay %v)",
						r.GetName(), e.ID, attempt, r.MaxRetries, delay)
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				lastErr = next(ctx, e)
				if lastErr == nil {
					return nil
				}
			}
			return lastErr
		}
	}
}

// calculateDelay computes the backoff delay for a given attempt.
func (r *RetryMiddleware) calculateDelay(attempt int) time.Duration {
	delay := r.BaseDelay
	for i := 0; i < attempt-1; i++ {
		delay *= 2
		if delay > r.MaxDelay {
			return r.MaxDelay
		}
	}
	return delay
}
EOF

commit "Add middleware chain with logging, auth, and retry" "2024-02-01T14:00:00"

# ============================================================
# Commit 8: Add worker pool for async event processing
# ============================================================
mkdir -p pkg/worker

cat > pkg/worker/pool.go << 'EOF'
// Package worker provides a generic worker pool for concurrent event processing.
package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/example/eventbus/pkg/event"
)

// Job represents a unit of work for the worker pool.
type Job struct {
	ID    string
	Event *event.Event
}

// WorkerPool manages a pool of goroutines that process jobs concurrently.
type WorkerPool struct {
	name      string
	size      int
	jobCh     chan Job
	resultCh  chan event.Result[string]
	handler   func(context.Context, *event.Event) (string, error)
	wg        sync.WaitGroup
	processed atomic.Int64
	errors    atomic.Int64
}

// NewWorkerPool creates a worker pool with the given size and job handler.
func NewWorkerPool(name string, size int, handler func(context.Context, *event.Event) (string, error)) *WorkerPool {
	return &WorkerPool{
		name:     name,
		size:     size,
		jobCh:    make(chan Job, size*2),
		resultCh: make(chan event.Result[string], size*2),
		handler:  handler,
	}
}

// Start launches the worker goroutines.
func (p *WorkerPool) Start(ctx context.Context) {
	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
	log.Printf("[pool:%s] started %d workers", p.name, p.size)
}

// Submit adds a job to the pool's work queue.
func (p *WorkerPool) Submit(job Job) error {
	select {
	case p.jobCh <- job:
		return nil
	default:
		return fmt.Errorf("worker pool %s: job queue full", p.name)
	}
}

// Results returns the channel for reading job results.
func (p *WorkerPool) Results() <-chan event.Result[string] {
	return p.resultCh
}

// Stop signals all workers to finish and waits for them to complete.
func (p *WorkerPool) Stop() {
	close(p.jobCh)
	p.wg.Wait()
	close(p.resultCh)
}

// Stats returns the pool's processing statistics.
func (p *WorkerPool) Stats() (processed, errors int64) {
	return p.processed.Load(), p.errors.Load()
}

// worker is the goroutine that processes jobs from the queue.
func (p *WorkerPool) worker(ctx context.Context, id int) {
	defer p.wg.Done()
	for job := range p.jobCh {
		select {
		case <-ctx.Done():
			return
		default:
		}

		result, err := p.handler(ctx, job.Event)
		if err != nil {
			p.errors.Add(1)
			p.resultCh <- event.Err[string](fmt.Errorf("job %s: %w", job.ID, err))
		} else {
			p.processed.Add(1)
			p.resultCh <- event.OK(result)
		}
	}
}
EOF

commit "Add worker pool for async event processing" "2024-02-05T09:00:00"

# ============================================================
# Commit 9: Add HTTP API layer with service and handlers
# ============================================================
mkdir -p pkg/api

cat > pkg/api/service.go << 'EOF'
// Package api provides the HTTP API layer for the event bus.
package api

import (
	"context"

	"github.com/example/eventbus/pkg/bus"
	"github.com/example/eventbus/pkg/event"
	"github.com/example/eventbus/pkg/storage"
	"github.com/google/uuid"
)

// Service is the application service layer that delegates to the bus and storage.
// It provides a clean API for the HTTP handlers to call.
type Service struct {
	bus   *bus.Bus
	store storage.Storage
}

// NewService creates a Service backed by the given bus and storage.
func NewService(b *bus.Bus, s storage.Storage) *Service {
	return &Service{bus: b, store: s}
}

// Publish delegates to the bus to publish an event.
func (s *Service) Publish(ctx context.Context, e *event.Event) error {
	return s.bus.Publish(ctx, e)
}

// GetEvent delegates to storage to retrieve an event by ID.
func (s *Service) GetEvent(ctx context.Context, id uuid.UUID) (*event.Event, error) {
	return s.store.Get(id)
}

// ListEvents delegates to storage to list events by topic.
func (s *Service) ListEvents(ctx context.Context, topic string) ([]*event.Event, error) {
	return s.store.List(topic)
}

// DeleteEvent delegates to storage to remove an event.
func (s *Service) DeleteEvent(ctx context.Context, id uuid.UUID) error {
	return s.store.Delete(id)
}

// GetStats delegates to the bus to retrieve runtime statistics.
func (s *Service) GetStats(ctx context.Context) map[string]int64 {
	return s.bus.GetStats()
}
EOF

cat > pkg/api/handler.go << 'EOF'
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/example/eventbus/pkg/event"
	"github.com/google/uuid"
)

// Handler provides HTTP endpoints for the event bus API.
type Handler struct {
	service *Service
}

// NewHandler creates a Handler backed by the given Service.
func NewHandler(svc *Service) *Handler {
	return &Handler{service: svc}
}

// RegisterRoutes sets up the HTTP routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /events", h.Publish)
	mux.HandleFunc("GET /events/{id}", h.GetEvent)
	mux.HandleFunc("GET /events", h.ListEvents)
	mux.HandleFunc("DELETE /events/{id}", h.DeleteEvent)
	mux.HandleFunc("GET /stats", h.GetStats)
}

// Publish handles POST /events — creates and publishes a new event.
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Topic    string                 `json:"topic"`
		Payload  map[string]interface{} `json:"payload"`
		Priority int                    `json:"priority"`
		Source   string                 `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Topic == "" {
		writeError(w, http.StatusBadRequest, "topic is required")
		return
	}

	e := event.New(req.Topic, req.Payload).
		WithPriority(event.Priority(req.Priority)).
		WithSource(req.Source)

	if err := h.service.Publish(r.Context(), e); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to publish: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      e.ID,
		"topic":   e.Topic,
		"message": "event published",
	})
}

// GetEvent handles GET /events/{id} — retrieves a single event.
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid event ID")
		return
	}

	e, err := h.service.GetEvent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}

	writeJSON(w, http.StatusOK, e)
}

// ListEvents handles GET /events?topic=... — lists events by topic.
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	if topic == "" {
		writeError(w, http.StatusBadRequest, "topic query parameter is required")
		return
	}

	events, err := h.service.ListEvents(r.Context(), topic)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list events: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, events)
}

// DeleteEvent handles DELETE /events/{id} — removes an event.
func (h *Handler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid event ID")
		return
	}

	if err := h.service.DeleteEvent(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "event deleted"})
}

// GetStats handles GET /stats — returns bus runtime statistics.
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats := h.service.GetStats(r.Context())
	writeJSON(w, http.StatusOK, stats)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
EOF

commit "Add HTTP API layer with service and handlers" "2024-02-08T13:00:00"

# ============================================================
# Commit 10: Add unit tests for bus, storage, and worker pool
# ============================================================
cat > pkg/bus/bus_test.go << 'EOF'
package bus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/eventbus/pkg/event"
	"github.com/example/eventbus/pkg/storage"
)

func TestPublishAndSubscribe(t *testing.T) {
	store := storage.NewMemoryStorage()
	b := New(store, DefaultConfig())
	b.Start()
	defer b.Stop()

	var received atomic.Int64
	b.Subscribe("test.topic", func(ctx context.Context, e *event.Event) error {
		received.Add(1)
		return nil
	})

	e := event.New("test.topic", map[string]interface{}{"key": "value"})
	if err := b.Publish(context.Background(), e); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if received.Load() != 1 {
		t.Errorf("received = %d, want 1", received.Load())
	}
}

func TestWildcardSubscription(t *testing.T) {
	store := storage.NewMemoryStorage()
	b := New(store, DefaultConfig())
	b.Start()
	defer b.Stop()

	var received atomic.Int64
	b.Subscribe("user.*", func(ctx context.Context, e *event.Event) error {
		received.Add(1)
		return nil
	})

	events := []*event.Event{
		event.New("user.created", nil),
		event.New("user.updated", nil),
		event.New("order.created", nil), // should not match
	}

	for _, e := range events {
		b.Publish(context.Background(), e)
	}

	time.Sleep(200 * time.Millisecond)
	if received.Load() != 2 {
		t.Errorf("received = %d, want 2", received.Load())
	}
}

func TestTopicMatches(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		want    bool
	}{
		{"*", "anything", true},
		{"user.*", "user.created", true},
		{"user.*", "order.created", false},
		{"user.created", "user.created", true},
		{"user.created", "user.updated", false},
	}

	for _, tt := range tests {
		if got := topicMatches(tt.pattern, tt.topic); got != tt.want {
			t.Errorf("topicMatches(%q, %q) = %v, want %v", tt.pattern, tt.topic, got, tt.want)
		}
	}
}

func TestValidateTopic(t *testing.T) {
	if err := validateTopic(""); err == nil {
		t.Error("expected error for empty topic")
	}
	if err := validateTopic(".leading"); err == nil {
		t.Error("expected error for leading dot")
	}
	if err := validateTopic("trailing."); err == nil {
		t.Error("expected error for trailing dot")
	}
	if err := validateTopic("valid.topic"); err != nil {
		t.Errorf("unexpected error for valid topic: %v", err)
	}
}
EOF

cat > pkg/storage/memory_test.go << 'EOF'
package storage

import (
	"testing"

	"github.com/example/eventbus/pkg/event"
	"github.com/google/uuid"
)

func TestMemoryStorage_SaveAndGet(t *testing.T) {
	store := NewMemoryStorage()
	e := event.New("test.topic", map[string]interface{}{"key": "value"})

	if err := store.Save(e); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := store.Get(e.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Topic != e.Topic {
		t.Errorf("Topic = %q, want %q", got.Topic, e.Topic)
	}
}

func TestMemoryStorage_Delete(t *testing.T) {
	store := NewMemoryStorage()
	e := event.New("test.topic", nil)
	store.Save(e)

	if err := store.Delete(e.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := store.Get(e.ID)
	if err != ErrNotFound {
		t.Errorf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStorage_DeleteNotFound(t *testing.T) {
	store := NewMemoryStorage()
	err := store.Delete(uuid.New())
	if err != ErrNotFound {
		t.Errorf("Delete non-existent: err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStorage_List(t *testing.T) {
	store := NewMemoryStorage()
	store.Save(event.New("topic.a", nil))
	store.Save(event.New("topic.a", nil))
	store.Save(event.New("topic.b", nil))

	events, err := store.List("topic.a")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("List returned %d events, want 2", len(events))
	}
}

func TestMemoryStorage_Count(t *testing.T) {
	store := NewMemoryStorage()
	store.Save(event.New("a", nil))
	store.Save(event.New("b", nil))

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Count = %d, want 2", count)
	}
}

func TestMemoryStorage_Name(t *testing.T) {
	store := NewMemoryStorage()
	if store.Name() != "memory" {
		t.Errorf("Name = %q, want memory", store.Name())
	}
}
EOF

cat > pkg/worker/pool_test.go << 'EOF'
package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/example/eventbus/pkg/event"
)

func TestWorkerPool_ProcessJobs(t *testing.T) {
	handler := func(ctx context.Context, e *event.Event) (string, error) {
		return "processed:" + e.Topic, nil
	}

	pool := NewWorkerPool("test", 2, handler)
	ctx := context.Background()
	pool.Start(ctx)

	pool.Submit(Job{ID: "1", Event: event.New("test.a", nil)})
	pool.Submit(Job{ID: "2", Event: event.New("test.b", nil)})

	pool.Stop()

	processed, errors := pool.Stats()
	if processed != 2 {
		t.Errorf("processed = %d, want 2", processed)
	}
	if errors != 0 {
		t.Errorf("errors = %d, want 0", errors)
	}
}

func TestWorkerPool_HandleErrors(t *testing.T) {
	handler := func(ctx context.Context, e *event.Event) (string, error) {
		return "", fmt.Errorf("intentional error")
	}

	pool := NewWorkerPool("err-test", 1, handler)
	ctx := context.Background()
	pool.Start(ctx)

	pool.Submit(Job{ID: "1", Event: event.New("test", nil)})
	pool.Stop()

	_, errors := pool.Stats()
	if errors != 1 {
		t.Errorf("errors = %d, want 1", errors)
	}
}

func TestWorkerPool_Results(t *testing.T) {
	handler := func(ctx context.Context, e *event.Event) (string, error) {
		return "ok", nil
	}

	pool := NewWorkerPool("result-test", 1, handler)
	ctx := context.Background()
	pool.Start(ctx)

	pool.Submit(Job{ID: "1", Event: event.New("test", nil)})

	// Drain results before stopping
	result := <-pool.Results()
	if !result.IsOK() {
		t.Errorf("expected OK result, got error: %v", result.Error())
	}
	if result.Unwrap() != "ok" {
		t.Errorf("result = %q, want ok", result.Unwrap())
	}

	pool.Stop()
}
EOF

commit "Add unit tests for bus, storage, and worker pool" "2024-02-10T10:00:00"

# ============================================================
# Commit 11: Add main entry point and wire dependencies
# ============================================================
mkdir -p cmd/eventbus

cat > cmd/eventbus/main.go << 'EOF'
// Package main wires together the event bus application.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/eventbus/pkg/api"
	"github.com/example/eventbus/pkg/bus"
	"github.com/example/eventbus/pkg/middleware"
	"github.com/example/eventbus/pkg/storage"
)

func main() {
	// TODO: Add configuration loading from environment variables or config file
	// TODO: Add structured logging instead of log.Printf
	// FIXME: Port should be configurable, not hardcoded
	port := "8080"

	// Initialize storage
	// TODO: Make storage backend configurable (memory vs file vs future backends)
	store := storage.NewMemoryStorage()

	// Initialize bus
	config := bus.DefaultConfig()
	eventBus := bus.New(store, config)

	// Set up middleware chain
	logging := middleware.NewLoggingMiddleware(false)
	auth := middleware.NewAuthMiddleware([]string{"api", "worker", "system"})
	retry := middleware.NewRetryMiddleware(3, 100*time.Millisecond)

	_ = logging // TODO: Wire middleware into bus via Use() method
	_ = auth
	_ = retry

	// Start bus
	eventBus.Start()
	defer eventBus.Stop()

	// Set up API
	svc := api.NewService(eventBus, store)
	handler := api.NewHandler(svc)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("starting server on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	fmt.Println("\nshutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(shutdownCtx)
}
EOF

commit "Add main entry point and wire dependencies" "2024-02-15T11:00:00"

# ============================================================
# Commit 12: Audit and mark known tech debt and risks
# ============================================================

# Add TODOs/FIXMEs to existing files via patches
# We'll use sed to inject comments

# bus.go already has TODOs from commit 6, but add more
cat >> pkg/bus/bus.go << 'APPENDEOF'

// FIXME: Stats should be persisted across restarts — currently they reset to zero.
// TODO: Add dead letter queue for events that fail all retries.
// NOTE: The priority channel size is 1/4 of the main buffer. This ratio
// was chosen empirically but should be tunable per deployment.
APPENDEOF

# Add risk comments to storage
cat >> pkg/storage/file.go << 'APPENDEOF'

// FIXME: FileStorage has no file locking — concurrent processes may corrupt data.
// TODO: Add compression for large event payloads.
// NOTE: Event files are never automatically cleaned up. Manual or cron-based
// cleanup is needed to prevent unbounded disk growth.
APPENDEOF

# Add debt comments to middleware
cat >> pkg/middleware/auth.go << 'APPENDEOF'

// TODO: Add token-based authentication in addition to source-based.
// FIXME: AllowedSources map is not thread-safe when modified at runtime.
APPENDEOF

# Add comments to worker pool
cat >> pkg/worker/pool.go << 'APPENDEOF'

// TODO: Add graceful drain — currently Stop() discards in-flight jobs.
// NOTE: Worker pool size should be tuned based on the workload type.
// CPU-bound handlers benefit from GOMAXPROCS workers; I/O-bound handlers
// can use significantly more.
APPENDEOF

commit "Audit and mark known tech debt and risks" "2024-02-28T16:00:00"

# ============================================================
# Commit 13: Add internal packages, custom errors, and structured logger
# ============================================================
mkdir -p internal/errors internal/logger internal/config

cat > internal/errors/errors.go << 'EOF'
// Package errors provides custom error types for the eventbus application.
package errors

import "fmt"

// Error codes for categorizing errors.
const (
	CodeValidation = "VALIDATION"
	CodeStorage    = "STORAGE"
	CodeBus        = "BUS"
	CodeTimeout    = "TIMEOUT"
	CodeNotFound   = "NOT_FOUND"
)

// Sentinel errors for common failure cases.
var (
	ErrInvalidTopic   = &ValidationError{Field: "topic", Message: "topic must not be empty"}
	ErrInvalidPayload = &ValidationError{Field: "payload", Message: "payload must not be nil"}
	ErrBusStopped     = &EventBusError{Code: CodeBus, Message: "bus is not running"}
)

// EventBusError represents errors from the event bus core.
type EventBusError struct {
	Code    string
	Message string
	Err     error
}

func (e *EventBusError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error for errors.Is/errors.As support.
func (e *EventBusError) Unwrap() error { return e.Err }

// StorageError represents errors from storage backends.
type StorageError struct {
	Op      string
	Backend string
	Err     error
}

func (e *StorageError) Error() string {
	return fmt.Sprintf("storage(%s) %s: %v", e.Backend, e.Op, e.Err)
}

// Unwrap returns the underlying error for errors.Is/errors.As support.
func (e *StorageError) Unwrap() error { return e.Err }

// ValidationError represents input validation failures.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation(%s): %s", e.Field, e.Message)
}

// NewStorageError creates a StorageError for the given operation.
func NewStorageError(op, backend string, err error) *StorageError {
	return &StorageError{Op: op, Backend: backend, Err: err}
}

// NewBusError creates an EventBusError with the given code and cause.
func NewBusError(code, message string, err error) *EventBusError {
	return &EventBusError{Code: code, Message: message, Err: err}
}
EOF

cat > internal/logger/logger.go << 'EOF'
// Package logger provides structured logging for the eventbus application.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// Logger defines the logging interface used throughout the application.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
	WithContext(ctx context.Context) Logger
}

// StructuredLogger wraps slog.Logger to implement the Logger interface.
type StructuredLogger struct {
	inner *slog.Logger
}

// NewStructuredLogger creates a StructuredLogger writing JSON to the given writer.
func NewStructuredLogger(w io.Writer, level slog.Level) *StructuredLogger {
	opts := &slog.HandlerOptions{Level: level}
	handler := slog.NewJSONHandler(w, opts)
	return &StructuredLogger{inner: slog.New(handler)}
}

// Default returns a StructuredLogger writing to stdout at info level.
func Default() *StructuredLogger {
	return NewStructuredLogger(os.Stdout, slog.LevelInfo)
}

func (l *StructuredLogger) Debug(msg string, args ...any) { l.inner.Debug(msg, args...) }
func (l *StructuredLogger) Info(msg string, args ...any)  { l.inner.Info(msg, args...) }
func (l *StructuredLogger) Warn(msg string, args ...any)  { l.inner.Warn(msg, args...) }
func (l *StructuredLogger) Error(msg string, args ...any) { l.inner.Error(msg, args...) }

// With returns a new logger with the given key-value pairs attached.
func (l *StructuredLogger) With(args ...any) Logger {
	return &StructuredLogger{inner: l.inner.With(args...)}
}

// WithContext returns a new logger with context values attached.
func (l *StructuredLogger) WithContext(ctx context.Context) Logger {
	return &StructuredLogger{inner: l.inner}
}

// NopLogger is a logger that discards all output. Useful for testing.
type NopLogger struct{}

func (n *NopLogger) Debug(msg string, args ...any)        {}
func (n *NopLogger) Info(msg string, args ...any)         {}
func (n *NopLogger) Warn(msg string, args ...any)         {}
func (n *NopLogger) Error(msg string, args ...any)        {}
func (n *NopLogger) With(args ...any) Logger              { return n }
func (n *NopLogger) WithContext(ctx context.Context) Logger { return n }
EOF

cat > internal/config/config.go << 'EOF'
// Package config handles application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Port            string
	StorageBackend  string
	StoragePath     string
	BusBufferSize   int
	BusTimeout      time.Duration
	LogLevel        string
	MaxRetries      int
	ShutdownTimeout time.Duration
}

// LoadFromEnv reads configuration from environment variables with sensible defaults.
func LoadFromEnv() *Config {
	cfg := &Config{
		Port:            envOrDefault("EVENTBUS_PORT", "8080"),
		StorageBackend:  envOrDefault("EVENTBUS_STORAGE", "memory"),
		StoragePath:     envOrDefault("EVENTBUS_STORAGE_PATH", "/tmp/eventbus-data"),
		BusBufferSize:   envIntOrDefault("EVENTBUS_BUFFER_SIZE", 1024),
		BusTimeout:      envDurationOrDefault("EVENTBUS_TIMEOUT", 30*time.Second),
		LogLevel:        envOrDefault("EVENTBUS_LOG_LEVEL", "info"),
		MaxRetries:      envIntOrDefault("EVENTBUS_MAX_RETRIES", 3),
		ShutdownTimeout: envDurationOrDefault("EVENTBUS_SHUTDOWN_TIMEOUT", 5*time.Second),
	}
	return cfg
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("config: port must not be empty")
	}
	if c.BusBufferSize <= 0 {
		return fmt.Errorf("config: buffer size must be positive, got %d", c.BusBufferSize)
	}
	if c.StorageBackend != "memory" && c.StorageBackend != "file" {
		return fmt.Errorf("config: unknown storage backend %q", c.StorageBackend)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
EOF

commit "Add internal packages, custom errors, and structured logger" "2024-03-05T10:00:00"

# ============================================================
# Commit 14: Add functional options pattern and Makefile
# ============================================================
cat > pkg/bus/options.go << 'EOF'
package bus

import (
	"context"
	"time"

	"github.com/example/eventbus/internal/logger"
	"github.com/example/eventbus/pkg/event"
	"github.com/example/eventbus/pkg/middleware"
)

// Option is a functional option for configuring the Bus.
type Option func(*Bus)

// WithBufferSize sets the event channel buffer size.
func WithBufferSize(size int) Option {
	return func(b *Bus) {
		if size > 0 {
			b.config.BufferSize = size
		}
	}
}

// WithTimeout sets the publish timeout duration.
func WithTimeout(d time.Duration) Option {
	return func(b *Bus) {
		b.config.PublishTimeout = d
	}
}

// WithLogger sets a structured logger on the bus.
func WithLogger(l logger.Logger) Option {
	return func(b *Bus) {
		b.logger = l
	}
}

// WithMiddleware appends middleware to the bus processing chain.
func WithMiddleware(mw ...middleware.Middleware) Option {
	return func(b *Bus) {
		b.middlewares = append(b.middlewares, mw...)
	}
}

// WithMaxSubscribers sets the maximum subscribers per topic.
func WithMaxSubscribers(max int) Option {
	return func(b *Bus) {
		b.config.MaxSubscribers = max
	}
}

// NewBusWithOptions creates a Bus using functional options.
// This is the preferred constructor for production use.
func NewBusWithOptions(opts ...Option) *Bus {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bus{
		config:     DefaultConfig(),
		subs:       make(map[string][]*Subscription),
		eventCh:    make(chan *event.Event, 1024),
		priorityCh: make(chan *event.Event, 256),
		done:       make(chan struct{}),
		ctx:        ctx,
		cancel:     cancel,
		logger:     nil,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}
EOF

cat > Makefile << 'MAKEFILE'
.PHONY: build test lint fmt vet clean generate run

BINARY=eventbus
EVENTCTL=eventctl

build:
	go build -o bin/$(BINARY) ./cmd/eventbus
	go build -o bin/$(EVENTCTL) ./cmd/eventctl

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -rf bin/
	go clean -cache

generate:
	go generate ./...

run: build
	./bin/$(BINARY)

.DEFAULT_GOAL := build
MAKEFILE

commit "Add functional options pattern and Makefile" "2024-03-10T10:00:00"

# ============================================================
# Commit 15: Add eventctl CLI and go:generate directive
# ============================================================
mkdir -p cmd/eventctl

cat > cmd/eventctl/main.go << 'EOF'
// Package main implements the eventctl CLI for managing the event bus.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/example/eventbus/internal/config"
	"github.com/example/eventbus/internal/errors"
)

//go:generate stringer -type=Command

// Command represents a CLI subcommand.
type Command int

const (
	CmdStatus Command = iota
	CmdPublish
	CmdListTopics
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg := config.LoadFromEnv()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "status":
		statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
		verbose := statusCmd.Bool("verbose", false, "show detailed status")
		statusCmd.Parse(os.Args[2:])
		runStatus(cfg, *verbose)
	case "publish":
		publishCmd := flag.NewFlagSet("publish", flag.ExitOnError)
		topic := publishCmd.String("topic", "", "event topic")
		publishCmd.Parse(os.Args[2:])
		if *topic == "" {
			fmt.Fprintln(os.Stderr, errors.ErrInvalidTopic.Error())
			os.Exit(1)
		}
		runPublish(cfg, *topic)
	case "list-topics":
		runListTopics(cfg)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: eventctl <command> [flags]\n\nCommands:\n  status       Show bus status\n  publish      Publish an event\n  list-topics  List all topics\n")
}

func runStatus(cfg *config.Config, verbose bool) {
	fmt.Printf("Bus status: running on port %s\n", cfg.Port)
	if verbose {
		fmt.Printf("  storage: %s\n  buffer: %d\n  retries: %d\n",
			cfg.StorageBackend, cfg.BusBufferSize, cfg.MaxRetries)
	}
}

func runPublish(cfg *config.Config, topic string) {
	fmt.Printf("Publishing event to topic %q (port %s)\n", topic, cfg.Port)
}

func runListTopics(cfg *config.Config) {
	fmt.Printf("Listing topics from bus on port %s\n", cfg.Port)
}
EOF

cat > pkg/event/topic_string.go << 'EOF'
package event

//go:generate stringer -type=Priority

// String returns the human-readable name for a Priority level.
func (p Priority) String() string {
	switch p {
	case Low:
		return "low"
	case Normal:
		return "normal"
	case High:
		return "high"
	case Critical:
		return "critical"
	default:
		return "unknown"
	}
}
EOF

# Update go.mod to add replace directive
cat > go.mod << 'GOMOD'
module github.com/example/eventbus

go 1.22

require (
	github.com/google/uuid v1.6.0
	golang.org/x/sync v0.6.0
)

replace golang.org/x/sync => golang.org/x/sync v0.6.0
GOMOD

commit "Add eventctl CLI and go:generate directive" "2024-03-15T10:00:00"

# ============================================================
# Commit 16: Add health check and metrics endpoint
# ============================================================
cat > pkg/api/health.go << 'EOF'
package api

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

// HealthStatus represents the response from the health endpoint.
type HealthStatus struct {
	Status    string    `json:"status"`
	Uptime    string    `json:"uptime"`
	Version   string    `json:"version"`
	GoVersion string    `json:"go_version"`
	Timestamp time.Time `json:"timestamp"`
}

// Metrics represents runtime and application metrics.
type Metrics struct {
	Goroutines   int              `json:"goroutines"`
	HeapAlloc    uint64           `json:"heap_alloc_bytes"`
	HeapObjects  uint64           `json:"heap_objects"`
	GCPauses     uint32           `json:"gc_pauses"`
	BusStats     map[string]int64 `json:"bus_stats"`
	EventCount   int64            `json:"event_count"`
}

// HealthChecker provides health and metrics endpoints.
// It embeds *Service to access bus statistics.
type HealthChecker struct {
	*Service
	startTime time.Time
	version   string
}

// NewHealthChecker creates a HealthChecker for the given service.
func NewHealthChecker(svc *Service, version string) *HealthChecker {
	return &HealthChecker{
		Service:   svc,
		startTime: time.Now(),
		version:   version,
	}
}

// RegisterHealthRoutes adds health and metrics routes to the mux.
func (hc *HealthChecker) RegisterHealthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", hc.HealthHandler)
	mux.HandleFunc("GET /metrics", hc.MetricsHandler)
}

// HealthHandler responds with the application health status.
func (hc *HealthChecker) HealthHandler(w http.ResponseWriter, r *http.Request) {
	status := HealthStatus{
		Status:    "ok",
		Uptime:    time.Since(hc.startTime).String(),
		Version:   hc.version,
		GoVersion: runtime.Version(),
		Timestamp: time.Now(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// MetricsHandler responds with runtime and application metrics.
func (hc *HealthChecker) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	metrics := Metrics{
		Goroutines:  runtime.NumGoroutine(),
		HeapAlloc:   memStats.HeapAlloc,
		HeapObjects: memStats.HeapObjects,
		GCPauses:    memStats.NumGC,
		BusStats:    hc.Service.GetStats(r.Context()),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}
EOF

cat > pkg/api/health_test.go << 'EOF'
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	svc := &Service{} // nil bus/store is fine for health check
	hc := NewHealthChecker(svc, "test-v1")

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	hc.HealthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var status HealthStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Status != "ok" {
		t.Errorf("status = %q, want ok", status.Status)
	}
	if status.Version != "test-v1" {
		t.Errorf("version = %q, want test-v1", status.Version)
	}
}

func TestMetricsHandler(t *testing.T) {
	svc := &Service{} // nil bus/store
	hc := NewHealthChecker(svc, "test-v1")

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	hc.MetricsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var metrics Metrics
	if err := json.NewDecoder(w.Body).Decode(&metrics); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if metrics.Goroutines <= 0 {
		t.Error("expected positive goroutine count")
	}
}

func TestHealthRouteRegistration(t *testing.T) {
	svc := &Service{}
	hc := NewHealthChecker(svc, "test-v1")

	mux := http.NewServeMux()
	hc.RegisterHealthRoutes(mux)

	// Verify /health route exists
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/health status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMetricsContainsRuntimeInfo(t *testing.T) {
	svc := &Service{}
	hc := NewHealthChecker(svc, "test-v1")

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	hc.MetricsHandler(w, req)

	var metrics Metrics
	json.NewDecoder(w.Body).Decode(&metrics)

	if metrics.HeapAlloc == 0 {
		t.Error("expected non-zero heap allocation")
	}
}
EOF

commit "Add health check and metrics endpoint" "2024-03-20T10:00:00"

# ============================================================
# Commit 17: Add tech debt markers for new packages
# ============================================================

cat >> internal/errors/errors.go << 'APPENDEOF'

// TODO: Add error wrapping helpers that preserve stack traces.
// FIXME: Sentinel errors are package-level vars — they could be mutated accidentally.
// NOTE: Error codes are strings for extensibility. Consider switching to iota-based
// codes if performance becomes a concern.
APPENDEOF

cat >> internal/logger/logger.go << 'APPENDEOF'

// TODO: Add log rotation support for file-based logging.
// FIXME: NopLogger should still track log calls in test mode for assertion.
// NOTE: slog was chosen over zerolog/zap for zero external dependencies.
// Consider switching if sub-microsecond logging is needed.
APPENDEOF

cat >> internal/config/config.go << 'APPENDEOF'

// TODO: Add support for loading config from YAML/TOML files.
// FIXME: No config hot-reload — changes require a restart.
// NOTE: Environment variable names use EVENTBUS_ prefix to avoid collisions.
APPENDEOF

cat >> pkg/bus/options.go << 'APPENDEOF'

// TODO: Add WithMetrics option to enable Prometheus metric collection.
// NOTE: Functional options are preferred over config structs because they
// provide better API evolution without breaking changes.
APPENDEOF

cat >> cmd/eventctl/main.go << 'APPENDEOF'

// TODO: Add --format flag for JSON/table output.
// FIXME: eventctl connects to the bus over HTTP but has no auth token support.
// NOTE: Subcommands are implemented as simple functions. Consider migrating
// to cobra if the CLI grows beyond 5 subcommands.
APPENDEOF

cat >> pkg/api/health.go << 'APPENDEOF'

// TODO: Add readiness probe endpoint separate from liveness.
// FIXME: Metrics endpoint exposes heap stats which may be a security concern.
// NOTE: Health check does not verify storage connectivity — it only reports uptime.
APPENDEOF

commit "Add tech debt markers for new packages" "2024-03-25T10:00:00"

echo ""
echo "Test repo created at $REPO_DIR"
echo "  - $(find "$REPO_DIR" -name '*.go' | wc -l | tr -d ' ') Go files"
echo "  - $(cat $(find "$REPO_DIR" -name '*.go') | wc -l | tr -d ' ') lines of Go"
echo "  - $(git -C "$REPO_DIR" log --oneline | wc -l | tr -d ' ') commits"
echo ""
echo "Run: go run ./cmd/atlaskb index --force $REPO_DIR"
