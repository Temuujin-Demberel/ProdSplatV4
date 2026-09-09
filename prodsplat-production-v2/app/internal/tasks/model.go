package tasks

import "time"

type State string

type Type string

const (
	StateReady     State = "READY"
	StateRunning   State = "RUNNING"
	StateSucceeded State = "SUCCEEDED"
	StateFailed    State = "FAILED"
	StateCancelled State = "CANCELLED"

	TypeReconstruct Type = "RECONSTRUCT"
	TypeRender      Type = "RENDER"
	TypeDataset     Type = "DATASET"
)

type Task struct {
	ID              string            `json:"id"`
	JobID           string            `json:"jobId"`
	Attempt         int               `json:"attempt,omitempty"`
	Type            Type              `json:"type"`
	State           State             `json:"state"`
	Payload         map[string]string `json:"payload"`
	Result          map[string]string `json:"result,omitempty"`
	Progress        float64           `json:"progress"`
	Message         string            `json:"message,omitempty"`
	Error           string            `json:"error,omitempty"`
	AttemptCount    int               `json:"attemptCount"`
	MaxAttempts     int               `json:"maxAttempts"`
	LeaseOwner      string            `json:"leaseOwner,omitempty"`
	LeaseUntil      time.Time         `json:"leaseUntil,omitempty"`
	CancelRequested bool              `json:"cancelRequested,omitempty"`
	LogPath         string            `json:"logPath,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	StartedAt       *time.Time        `json:"startedAt,omitempty"`
	FinishedAt      *time.Time        `json:"finishedAt,omitempty"`
}
