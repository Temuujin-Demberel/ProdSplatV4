package jobs

import "time"

type State string

const (
	StateCreated         State = "CREATED"
	StateReconstructing  State = "RECONSTRUCTING"
	StateReviewReady     State = "REVIEW_READY"
	StateEditing         State = "EDITING"
	StateRendering       State = "RENDERING"
	StateRenderReady     State = "RENDER_READY"
	StateDatasetBuilding State = "DATASET_BUILDING"
	StateCompleted       State = "COMPLETED"
	StateFailed          State = "FAILED"
	StateCancelled       State = "CANCELLED"
)

type Attempt struct {
	Number            int       `json:"number"`
	State             State     `json:"state"`
	Profile           string    `json:"profile,omitempty"`
	VideoPath         string    `json:"videoPath,omitempty"`
	SplatPath         string    `json:"splatPath,omitempty"`
	ConfigPath        string    `json:"configPath,omitempty"`
	QualityPath       string    `json:"qualityPath,omitempty"`
	GaussianCount     int       `json:"gaussianCount,omitempty"`
	RegistrationRatio float64   `json:"registrationRatio,omitempty"`
	CleanedPath       string    `json:"cleanedPath,omitempty"`
	RenderDir         string    `json:"renderDir,omitempty"`
	DatasetZip        string    `json:"datasetZip,omitempty"`
	Error             string    `json:"error,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type Job struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	State           State     `json:"state"`
	Progress        float64   `json:"progress"`
	Message         string    `json:"message,omitempty"`
	Error           string    `json:"error,omitempty"`
	ActiveAttempt   int       `json:"activeAttempt"`
	Attempts        []Attempt `json:"attempts"`
	CleanedPath     string    `json:"cleanedPath,omitempty"`
	RenderDir       string    `json:"renderDir,omitempty"`
	BackgroundDir   string    `json:"backgroundDir,omitempty"`
	DatasetZip      string    `json:"datasetZip,omitempty"`
	CurrentTaskID   string    `json:"currentTaskId,omitempty"`
	CancelRequested bool      `json:"cancelRequested,omitempty"`
	Revision        int64     `json:"revision"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (j *Job) ActiveAttemptRef() *Attempt {
	for i := range j.Attempts {
		if j.Attempts[i].Number == j.ActiveAttempt {
			return &j.Attempts[i]
		}
	}
	return nil
}
