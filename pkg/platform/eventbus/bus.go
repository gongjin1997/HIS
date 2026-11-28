package eventbus

import (
	"sync"
	"time"
)

// LogLine is a single real-time log frame delivered over SSE.
type LogLine struct {
	Seq       int64  `json:"seq"`
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream"` // "stdout" | "stderr" | "system"
	Level     string `json:"level"`  // "info" | "warn" | "error" | "ok"
	Message   string `json:"message"`
	Done      bool   `json:"done,omitempty"`
	Status    string `json:"status,omitempty"`
}

type topicState struct {
	nextSeq int64
	history []LogLine
	closed  bool
	status  string
	subs    map[chan LogLine]struct{}
}

// LogBroker stores bounded per-topic log histories and fans out real-time lines to SSE subscribers.
type LogBroker struct {
	mu      sync.RWMutex
	maxHist int
	topics  map[string]*topicState
}

// NewLogBroker initializes a broker retaining up to maxHistory lines per topic.
func NewLogBroker(maxHistory int) *LogBroker {
	if maxHistory <= 0 {
		maxHistory = 2000
	}
	return &LogBroker{
		maxHist: maxHistory,
		topics:  make(map[string]*topicState),
	}
}

func (b *LogBroker) getOrCreateLocked(topic string) *topicState {
	ts, ok := b.topics[topic]
	if !ok {
		ts = &topicState{
			history: make([]LogLine, 0, 64),
			subs:    make(map[chan LogLine]struct{}),
		}
		b.topics[topic] = ts
	}
	return ts
}

// Publish appends a log line to topic and broadcasts it without blocking slow subscribers.
func (b *LogBroker) Publish(topic, stream, level, msg string) LogLine {
	b.mu.Lock()
	defer b.mu.Unlock()
	ts := b.getOrCreateLocked(topic)
	ts.nextSeq++
	line := LogLine{
		Seq:       ts.nextSeq,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Stream:    stream,
		Level:     level,
		Message:   msg,
	}
	if len(ts.history) >= b.maxHist {
		copy(ts.history, ts.history[1:])
		ts.history[len(ts.history)-1] = line
	} else {
		ts.history = append(ts.history, line)
	}
	for ch := range ts.subs {
		select {
		case ch <- line:
		default:
		}
	}
	return line
}

// Complete marks a topic finished and broadcasts a terminal Done event to all subscribers.
func (b *LogBroker) Complete(topic, status, summary string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ts := b.getOrCreateLocked(topic)
	ts.nextSeq++
	line := LogLine{
		Seq:       ts.nextSeq,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Stream:    "system",
		Level:     "info",
		Message:   summary,
		Done:      true,
		Status:    status,
	}
	ts.closed = true
	ts.status = status
	ts.history = append(ts.history, line)
	for ch := range ts.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// Subscribe returns historical lines with seq > afterSeq plus a live channel and unsubscribe callback.
func (b *LogBroker) Subscribe(topic string, afterSeq int64) ([]LogLine, <-chan LogLine, bool, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ts := b.getOrCreateLocked(topic)
	hist := make([]LogLine, 0, len(ts.history))
	for _, l := range ts.history {
		if l.Seq > afterSeq {
			hist = append(hist, l)
		}
	}
	ch := make(chan LogLine, 256)
	if !ts.closed {
		ts.subs[ch] = struct{}{}
	} else {
		close(ch)
	}
	closed := ts.closed
	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if cur, ok := b.topics[topic]; ok {
			if _, exists := cur.subs[ch]; exists {
				delete(cur.subs, ch)
				close(ch)
			}
		}
	}
	return hist, ch, closed, cancel
}

// History returns a snapshot of all retained lines for topic.
func (b *LogBroker) History(topic string) []LogLine {
	b.mu.RLock()
	defer b.mu.RUnlock()
	ts, ok := b.topics[topic]
	if !ok {
		return make([]LogLine, 0)
	}
	cp := make([]LogLine, len(ts.history))
	copy(cp, ts.history)
	return cp
}
