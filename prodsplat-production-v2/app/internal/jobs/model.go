package jobs

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

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

type RenderOptions struct {
	AssetName           string  `json:"assetName"`
	UpAxis              string  `json:"upAxis"`
	FrontAzimuthDegrees float64 `json:"frontAzimuthDegrees"`
	Isolate             bool    `json:"isolate"`
}

var UpAxes = []string{"+x", "-x", "+y", "-y", "+z", "-z"}

const (
	DefaultUpAxis    = "+z"
	FrontAzimuthStep = 22.5
	MaxFrontAzimuth  = 360 - FrontAzimuthStep
)

func SanitizeAssetName(raw string) string {
	var b strings.Builder
	pendingSeparator := false
	for _, r := range raw {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alnum {
			pendingSeparator = b.Len() > 0
			continue
		}
		if pendingSeparator {
			b.WriteByte('_')
			pendingSeparator = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func ResolveRenderOptions(jobName string, requested RenderOptions) (RenderOptions, error) {
	name := SanitizeAssetName(requested.AssetName)
	if name == "" && strings.TrimSpace(requested.AssetName) != "" {
		return RenderOptions{}, fmt.Errorf("assetName %q has no letters or digits", requested.AssetName)
	}
	if name == "" {
		name = SanitizeAssetName(jobName)
	}
	if name == "" {
		return RenderOptions{}, errors.New("assetName is required because the job name has no letters or digits")
	}
	axis := strings.TrimSpace(requested.UpAxis)
	if axis == "" {
		axis = DefaultUpAxis
	}
	if !slices.Contains(UpAxes, axis) {
		return RenderOptions{}, errors.New("upAxis must be one of +x, -x, +y, -y, +z, -z")
	}
	front := requested.FrontAzimuthDegrees
	if front < 0 || front > MaxFrontAzimuth || math.Mod(front, FrontAzimuthStep) != 0 {
		return RenderOptions{}, errors.New("frontAzimuthDegrees must be a multiple of 22.5 between 0 and 337.5")
	}
	return RenderOptions{AssetName: name, UpAxis: axis, FrontAzimuthDegrees: front, Isolate: requested.Isolate}, nil
}

type Attempt struct {
	Number            int            `json:"number"`
	State             State          `json:"state"`
	Profile           string         `json:"profile,omitempty"`
	VideoPath         string         `json:"videoPath,omitempty"`
	SplatPath         string         `json:"splatPath,omitempty"`
	ConfigPath        string         `json:"configPath,omitempty"`
	QualityPath       string         `json:"qualityPath,omitempty"`
	GaussianCount     int            `json:"gaussianCount,omitempty"`
	RegistrationRatio float64        `json:"registrationRatio,omitempty"`
	CleanedPath       string         `json:"cleanedPath,omitempty"`
	RenderDir         string         `json:"renderDir,omitempty"`
	RenderOptions     *RenderOptions `json:"renderOptions,omitempty"`
	IsolatedPath      string         `json:"isolatedPath,omitempty"`
	DatasetZip        string         `json:"datasetZip,omitempty"`
	Error             string         `json:"error,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type Job struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	State           State          `json:"state"`
	Progress        float64        `json:"progress"`
	Message         string         `json:"message,omitempty"`
	Error           string         `json:"error,omitempty"`
	ActiveAttempt   int            `json:"activeAttempt"`
	Attempts        []Attempt      `json:"attempts"`
	CleanedPath     string         `json:"cleanedPath,omitempty"`
	RenderDir       string         `json:"renderDir,omitempty"`
	RenderOptions   *RenderOptions `json:"renderOptions,omitempty"`
	IsolatedPath    string         `json:"isolatedPath,omitempty"`
	BackgroundDir   string         `json:"backgroundDir,omitempty"`
	DatasetZip      string         `json:"datasetZip,omitempty"`
	CurrentTaskID   string         `json:"currentTaskId,omitempty"`
	CancelRequested bool           `json:"cancelRequested,omitempty"`
	Revision        int64          `json:"revision"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

func (j *Job) ActiveAttemptRef() *Attempt {
	for i := range j.Attempts {
		if j.Attempts[i].Number == j.ActiveAttempt {
			return &j.Attempts[i]
		}
	}
	return nil
}
