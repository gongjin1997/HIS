package domain

import "fmt"

// OpState is the canonical 7-state Operation state machine (D1 §1.3-C, D2 §1.3, ARCHITECTURE §5.2).
type OpState string

const (
	OpAwaitingApproval OpState = "awaiting_approval"
	OpPending          OpState = "pending"
	OpRunning          OpState = "running"
	OpSucceeded        OpState = "succeeded"
	OpFailed           OpState = "failed"
	OpRejected         OpState = "rejected"
	OpCancelled        OpState = "cancelled"
)

var opTransitions = map[OpState][]OpState{
	OpAwaitingApproval: {OpPending, OpRejected, OpCancelled},
	OpPending:          {OpRunning, OpCancelled},
	OpRunning:          {OpRunning /* lease takeover fencing_token+1 */, OpSucceeded, OpFailed, OpCancelled},
	OpFailed:           {OpPending /* --resume re-enqueue */},
}

func (s OpState) CanTransitionTo(next OpState) bool {
	for _, allowed := range opTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

func (s OpState) IsTerminal() bool {
	return s == OpSucceeded || s == OpRejected || s == OpCancelled || s == OpFailed
}

// ValidateOpTransition asserts a legal OpState transition.
func ValidateOpTransition(from, to OpState) error {
	if !from.CanTransitionTo(to) {
		return fmt.Errorf("invalid operation transition %q -> %q", from, to)
	}
	return nil
}

// HostState is the canonical 7-state Host CMDB lifecycle (D2 §1.3).
type HostState string

const (
	HostPending      HostState = "pending"
	HostProbing      HostState = "probing"
	HostReady        HostState = "ready"
	HostInitializing HostState = "initializing"
	HostActive       HostState = "active"
	HostDraining     HostState = "draining"
	HostRetired      HostState = "retired"
)

var hostTransitions = map[HostState][]HostState{
	HostPending:      {HostProbing, HostRetired},
	HostProbing:      {HostReady, HostPending, HostRetired},
	HostReady:        {HostInitializing, HostActive, HostProbing, HostRetired},
	HostInitializing: {HostActive, HostReady, HostRetired},
	HostActive:       {HostProbing, HostInitializing, HostDraining, HostRetired},
	HostDraining:     {HostActive, HostRetired},
	HostRetired:      {HostPending},
}

func (s HostState) CanTransitionTo(next HostState) bool {
	for _, allowed := range hostTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// TaskState is the canonical 6-state TaskRun lifecycle (D2 §1.3).
type TaskState string

const (
	TaskPending    TaskState = "pending"
	TaskRunning    TaskState = "running"
	TaskSkipped    TaskState = "skipped"
	TaskSucceeded  TaskState = "succeeded"
	TaskFailed     TaskState = "failed"
	TaskRolledBack TaskState = "rolled_back"
)

var taskTransitions = map[TaskState][]TaskState{
	TaskPending: {TaskRunning, TaskSkipped},
	TaskRunning: {TaskRunning, TaskSkipped, TaskSucceeded, TaskFailed},
	TaskFailed:  {TaskRunning, TaskRolledBack},
}

func (s TaskState) CanTransitionTo(next TaskState) bool {
	for _, allowed := range taskTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// ApprovalStatus represents the four-eyes approval record state (G1 §4.2).
type ApprovalStatus string

const (
	ApprovalPending   ApprovalStatus = "pending"
	ApprovalApproved  ApprovalStatus = "approved"
	ApprovalRejected  ApprovalStatus = "rejected"
	ApprovalExpired   ApprovalStatus = "expired"
	ApprovalCancelled ApprovalStatus = "cancelled"
)

// CredentialState represents credential rotation lifecycle (D1 §3.3).
type CredentialState string

const (
	CredentialActive   CredentialState = "active"
	CredentialRotating CredentialState = "rotating"
	CredentialRevoked  CredentialState = "revoked"
)
