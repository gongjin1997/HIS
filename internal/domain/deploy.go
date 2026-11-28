package domain

import "time"

type JobState string

const (
	JobPending   JobState = "PENDING"
	JobRunning   JobState = "RUNNING"
	JobSucceeded JobState = "SUCCEEDED"
	JobFailed    JobState = "FAILED"
	JobCancelled JobState = "CANCELLED"
)

func (s JobState) IsTerminal() bool {
	return s == JobSucceeded || s == JobFailed || s == JobCancelled
}

type TaskState string

const (
	TaskPending     TaskState = "PENDING"
	TaskReady       TaskState = "READY"
	TaskRunning     TaskState = "RUNNING"
	TaskReconciling TaskState = "RECONCILING"
	TaskSucceeded   TaskState = "SUCCEEDED"
	TaskFailed      TaskState = "FAILED"
	TaskCancelled   TaskState = "CANCELLED"
	TaskSkipped     TaskState = "SKIPPED"
)

func (s TaskState) IsTerminal() bool {
	return s == TaskSucceeded || s == TaskFailed || s == TaskCancelled || s == TaskSkipped
}

type DeployJob struct {
	TenantID        string       `json:"tenant_id"`
	AppID           string       `json:"app_id"`
	JobID           string       `json:"job_id"`
	ProjectID       string       `json:"project_id"`
	Name            string       `json:"name"`
	TriggerType     string       `json:"trigger_type"`
	State           JobState     `json:"state"`
	CancelRequested bool         `json:"cancel_requested"`
	Version         int64        `json:"version"`
	LastSeq         int64        `json:"last_seq"`
	MinRetainedSeq  int64        `json:"min_retained_seq"`
	SpecJSON        string       `json:"spec_json"`
	SummaryJSON     string       `json:"summary_json"`
	CreatedBy       string       `json:"created_by"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	FinishedAt      *time.Time   `json:"finished_at,omitempty"`
	Tasks           []DeployTask `json:"tasks,omitempty"`
	Edges           []DeployEdge `json:"edges,omitempty"`
}

type DeployTask struct {
	TenantID   string         `json:"tenant_id"`
	AppID      string         `json:"app_id"`
	JobID      string         `json:"job_id"`
	TaskID     string         `json:"task_id"`
	TaskKey    string         `json:"task_key"`
	Name       string         `json:"name"`
	TaskType   string         `json:"task_type"`
	NodeID     string         `json:"node_id"`
	Payload    map[string]any `json:"payload"`
	State      TaskState      `json:"state"`
	Remaining  int            `json:"remaining"`
	LeaseEpoch int64          `json:"lease_epoch"`
	AttemptNo  int            `json:"attempt_no"`
	Owner      string         `json:"owner"`
	LeaseUntil *time.Time     `json:"lease_until,omitempty"`
	ActionKey  string         `json:"action_key"`
	MaxRetries int            `json:"max_retries"`
	LastError  string         `json:"last_error,omitempty"`
	ResultJSON string         `json:"result_json,omitempty"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type DeployEdge struct {
	TenantID   string `json:"tenant_id"`
	AppID      string `json:"app_id"`
	JobID      string `json:"job_id"`
	FromTaskID string `json:"from_task_id"`
	ToTaskID   string `json:"to_task_id"`
	Satisfied  bool   `json:"satisfied"`
}

type DeployAttempt struct {
	TenantID     string     `json:"tenant_id"`
	AppID        string     `json:"app_id"`
	JobID        string     `json:"job_id"`
	TaskID       string     `json:"task_id"`
	AttemptNo    int        `json:"attempt_no"`
	LeaseEpoch   int64      `json:"lease_epoch"`
	Owner        string     `json:"owner"`
	ActionKey    string     `json:"action_key"`
	State        TaskState  `json:"state"`
	ErrorMessage string     `json:"error_message,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

type DeployReceipt struct {
	TenantID    string    `json:"tenant_id"`
	AppID       string    `json:"app_id"`
	Provider    string    `json:"provider"`
	ReceiptID   string    `json:"receipt_id"`
	JobID       string    `json:"job_id"`
	TaskID      string    `json:"task_id"`
	AttemptNo   int       `json:"attempt_no"`
	LeaseEpoch  int64     `json:"lease_epoch"`
	Status      TaskState `json:"status"` // SUCCEEDED | FAILED | CANCELLED
	Digest      string    `json:"digest"`
	PayloadJSON string    `json:"payload_json"`
	ReceivedAt  time.Time `json:"received_at"`
}

type TimelineEvent struct {
	TenantID    string    `json:"tenant_id"`
	AppID       string    `json:"app_id"`
	JobID       string    `json:"job_id"`
	Seq         int64     `json:"seq"`
	EventType   string    `json:"event_type"`
	TaskID      string    `json:"task_id,omitempty"`
	AttemptNo   int       `json:"attempt_no,omitempty"`
	LeaseEpoch  int64     `json:"lease_epoch,omitempty"`
	Message     string    `json:"message"`
	PayloadJSON string    `json:"payload_json,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type OutboxEvent struct {
	EventID       string     `json:"event_id"`
	TenantID      string     `json:"tenant_id"`
	AppID         string     `json:"app_id"`
	DedupKey      string     `json:"dedup_key"`
	Topic         string     `json:"topic"`
	AggregateType string     `json:"aggregate_type"`
	AggregateID   string     `json:"aggregate_id"`
	PayloadJSON   string     `json:"payload_json"`
	Status        string     `json:"status"` // PENDING | PUBLISHED
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// JobSnapshot is the consistent point-in-time read returned by GET /api/v1/deploy/jobs/{jobId} (LiveOps D19).
type JobSnapshot struct {
	Job      DeployJob       `json:"job"`
	Tasks    []DeployTask    `json:"tasks"`
	Edges    []DeployEdge    `json:"edges"`
	Timeline []TimelineEvent `json:"timeline,omitempty"`
	LastSeq  int64           `json:"last_seq"`
}

// ReduceJobState computes the deterministic aggregate state of a DeployJob from its tasks and cancel_requested flag
// (implements LiveOps D12 & D18 convergence rules).
func ReduceJobState(tasks []DeployTask, cancelRequested bool) JobState {
	if len(tasks) == 0 {
		if cancelRequested {
			return JobCancelled
		}
		return JobSucceeded
	}
	var active, failed, cancelled, succeeded, skipped int
	for _, t := range tasks {
		switch t.State {
		case TaskRunning, TaskReconciling, TaskReady, TaskPending:
			active++
		case TaskSucceeded:
			succeeded++
		case TaskFailed:
			failed++
		case TaskCancelled:
			cancelled++
		case TaskSkipped:
			skipped++
		}
	}
	if active > 0 {
		return JobRunning
	}
	if cancelRequested || cancelled > 0 {
		if failed > 0 {
			return JobFailed
		}
		return JobCancelled
	}
	if failed > 0 {
		return JobFailed
	}
	if succeeded+skipped == len(tasks) {
		return JobSucceeded
	}
	return JobRunning
}
