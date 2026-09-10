package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/demberel-temuujin/prodsplat-production/app/internal/events"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/storage"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/tasks"
)

type Service struct {
	mu             sync.Mutex
	jobs           Repository
	tasks          tasks.Repository
	store          *storage.Local
	broker         *events.Broker
	lease          time.Duration
	defaultProfile string
}

func NewService(jobRepo Repository, taskRepo tasks.Repository, store *storage.Local, broker *events.Broker, lease time.Duration, defaultProfile string) *Service {
	return &Service{jobs: jobRepo, tasks: taskRepo, store: store, broker: broker, lease: lease, defaultProfile: defaultProfile}
}

func (s *Service) Create(ctx context.Context, name string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	now := time.Now().UTC()
	job := &Job{ID: newID("job"), Name: name, State: StateCreated, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := os.MkdirAll(s.store.JobDir(job.ID), 0o755); err != nil {
		return nil, err
	}
	if err := s.jobs.Save(ctx, job); err != nil {
		return nil, err
	}
	s.broker.Publish(job.ID)
	return job, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Job, error) { return s.jobs.FindByID(ctx, id) }
func (s *Service) List(ctx context.Context) ([]Job, error)          { return s.jobs.List(ctx) }
func (s *Service) ListTasks(ctx context.Context, jobID string) ([]tasks.Task, error) {
	return s.tasks.ListByJob(ctx, jobID)
}
func (s *Service) Task(ctx context.Context, id string) (*tasks.Task, error) {
	return s.tasks.Find(ctx, id)
}

func (s *Service) NextAttemptNumber(job *Job) int {
	n := 1
	for _, a := range job.Attempts {
		if a.Number >= n {
			n = a.Number + 1
		}
	}
	return n
}

func (s *Service) AddVideo(ctx context.Context, id, videoPath, profileName string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.CurrentTaskID != "" && (job.State == StateReconstructing || job.State == StateRendering || job.State == StateDatasetBuilding) {
		return nil, errors.New("job already has an active processing task")
	}
	if profileName == "" {
		profileName = s.defaultProfile
	}
	profile, err := Profile(profileName)
	if err != nil {
		return nil, err
	}
	n := s.NextAttemptNumber(job)
	ext := strings.ToLower(filepath.Ext(videoPath))
	stableVideoPath := filepath.Join(s.store.AttemptDir(id, n), "input"+ext)
	if err := os.MkdirAll(filepath.Dir(stableVideoPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(videoPath, stableVideoPath); err != nil {
		return nil, fmt.Errorf("commit uploaded video: %w", err)
	}
	now := time.Now().UTC()
	taskID := newID("task")
	logPath := s.store.TaskLogPath(id, taskID)
	task := &tasks.Task{
		ID: taskID, JobID: id, Attempt: n, Type: tasks.TypeReconstruct, State: tasks.StateReady,
		Payload: map[string]string{
			"videoPath":          stableVideoPath,
			"attemptDir":         s.store.AttemptDir(id, n),
			"profile":            profile.Name,
			"frames":             fmt.Sprint(profile.Frames),
			"iterations":         fmt.Sprint(profile.Iterations),
			"dataDownscale":      fmt.Sprint(profile.DataDownscale),
			"modelDownscales":    fmt.Sprint(profile.ModelDownscales),
			"resolutionSchedule": fmt.Sprint(profile.ResolutionSchedule),
		},
		MaxAttempts: 2, LogPath: logPath, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		_ = os.RemoveAll(s.store.AttemptDir(id, n))
		return nil, err
	}
	job.ActiveAttempt = n
	job.Attempts = append(job.Attempts, Attempt{Number: n, State: StateReconstructing, Profile: profile.Name, VideoPath: stableVideoPath, CreatedAt: now, UpdatedAt: now})
	job.State = StateReconstructing
	job.CleanedPath = ""
	job.RenderDir = ""
	job.DatasetZip = ""
	job.Progress = 0
	job.Message = "reconstruction queued"
	job.Error = ""
	job.CurrentTaskID = taskID
	job.CancelRequested = false
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		_ = s.tasks.Delete(context.Background(), taskID)
		_ = os.RemoveAll(s.store.AttemptDir(id, n))
		return nil, err
	}
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) AddExistingPLY(ctx context.Context, id, splatPath string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.CurrentTaskID != "" && (job.State == StateReconstructing || job.State == StateRendering || job.State == StateDatasetBuilding) {
		return nil, errors.New("job has an active task")
	}
	n := s.NextAttemptNumber(job)
	stableSplatPath := filepath.Join(s.store.AttemptDir(id, n), "splat.ply")
	if err := os.MkdirAll(filepath.Dir(stableSplatPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(splatPath, stableSplatPath); err != nil {
		return nil, fmt.Errorf("commit uploaded PLY: %w", err)
	}
	now := time.Now().UTC()
	job.ActiveAttempt = n
	job.Attempts = append(job.Attempts, Attempt{Number: n, State: StateReviewReady, SplatPath: stableSplatPath, CreatedAt: now, UpdatedAt: now})
	job.State = StateReviewReady
	job.CleanedPath = ""
	job.RenderDir = ""
	job.DatasetZip = ""
	job.Progress = 1
	job.Message = "existing Gaussian PLY ready for review"
	job.Error = ""
	job.CurrentTaskID = ""
	job.CancelRequested = false
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		_ = os.RemoveAll(s.store.AttemptDir(id, n))
		return nil, err
	}
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) ActivateAttempt(ctx context.Context, id string, number int) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.CurrentTaskID != "" {
		return nil, errors.New("cannot switch attempts while a task is active")
	}
	var selected *Attempt
	for i := range job.Attempts {
		if job.Attempts[i].Number == number {
			selected = &job.Attempts[i]
			break
		}
	}
	if selected == nil {
		return nil, errors.New("attempt not found")
	}
	if selected.SplatPath == "" || (selected.State != StateReviewReady && selected.State != StateEditing && selected.State != StateRenderReady && selected.State != StateCompleted) {
		return nil, errors.New("attempt does not have a reviewable Gaussian asset")
	}
	job.ActiveAttempt = number
	job.CleanedPath = selected.CleanedPath
	job.RenderDir = selected.RenderDir
	job.DatasetZip = selected.DatasetZip
	if selected.RenderOptions != nil {
		options := *selected.RenderOptions
		job.RenderOptions = &options
	}
	job.Error = ""
	job.Progress = 1
	if selected.DatasetZip != "" {
		job.State = StateCompleted
		job.Message = "previous completed attempt activated"
	} else if selected.RenderDir != "" {
		job.State = StateRenderReady
		job.Message = "previous rendered attempt activated"
	} else if selected.CleanedPath != "" {
		job.State = StateEditing
		job.Message = "previous edited attempt activated"
	} else {
		job.State = StateReviewReady
		job.Message = "previous reconstruction activated for review"
	}
	s.touch(job, time.Now().UTC())
	if err := s.jobs.Save(ctx, job); err != nil {
		return nil, err
	}
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) SaveCleaned(ctx context.Context, id, stagedPath string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.ActiveAttempt <= 0 || job.ActiveAttemptRef() == nil {
		return nil, errors.New("no active attempt")
	}
	if job.CurrentTaskID != "" {
		return nil, errors.New("cannot replace the edited asset while a processing task is active")
	}
	stablePath := filepath.Join(s.store.AttemptDir(id, job.ActiveAttempt), "cleaned.ply")
	backupPath, err := replaceWithBackup(stagedPath, stablePath)
	if err != nil {
		return nil, fmt.Errorf("commit edited PLY: %w", err)
	}
	now := time.Now().UTC()
	oldCleaned := job.CleanedPath
	job.CleanedPath = stablePath
	if a := job.ActiveAttemptRef(); a != nil {
		a.CleanedPath = stablePath
		a.RenderDir = ""
		a.DatasetZip = ""
		a.UpdatedAt = now
	}
	job.RenderDir = ""
	job.DatasetZip = ""
	job.State = StateEditing
	job.Progress = 1
	job.Message = "edited Gaussian asset saved"
	job.Error = ""
	job.CancelRequested = false
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		_ = rollbackReplace(stablePath, backupPath)
		job.CleanedPath = oldCleaned
		return nil, err
	}
	removeBackup(backupPath)
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) StartRender(ctx context.Context, id string, requested *RenderOptions) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.CleanedPath == "" {
		return nil, errors.New("save the edited/cleaned PLY before rendering")
	}
	if job.CurrentTaskID != "" && (job.State == StateRendering || job.State == StateDatasetBuilding || job.State == StateReconstructing) {
		return nil, errors.New("job already has an active processing task")
	}
	effective := RenderOptions{}
	if requested != nil {
		effective = *requested
	} else if job.RenderOptions != nil {
		effective = *job.RenderOptions
	}
	resolved, err := ResolveRenderOptions(job.Name, effective)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	taskID := newID("task")
	renderDir := filepath.Join(s.store.AttemptDir(id, job.ActiveAttempt), "renders")
	payload := map[string]string{
		"splatPath":           job.CleanedPath,
		"renderDir":           renderDir,
		"assetName":           resolved.AssetName,
		"upAxis":              resolved.UpAxis,
		"frontAzimuthDegrees": strconv.FormatFloat(resolved.FrontAzimuthDegrees, 'f', -1, 64),
	}
	task := &tasks.Task{ID: taskID, JobID: id, Attempt: job.ActiveAttempt, Type: tasks.TypeRender, State: tasks.StateReady, Payload: payload, MaxAttempts: 2, LogPath: s.store.TaskLogPath(id, taskID), CreatedAt: now, UpdatedAt: now}
	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, err
	}
	job.RenderDir = renderDir
	job.DatasetZip = ""
	jobOptions := resolved
	job.RenderOptions = &jobOptions
	if a := job.ActiveAttemptRef(); a != nil {
		a.DatasetZip = ""
		attemptOptions := resolved
		a.RenderOptions = &attemptOptions
	}
	job.State = StateRendering
	job.Progress = 0
	job.Message = fmt.Sprintf("transparent rendering queued (%s, up %s, front %g°)", resolved.AssetName, resolved.UpAxis, resolved.FrontAzimuthDegrees)
	job.Error = ""
	job.CurrentTaskID = taskID
	job.CancelRequested = false
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		_ = s.tasks.Delete(context.Background(), taskID)
		return nil, err
	}
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) SetBackgroundDir(ctx context.Context, id, stagedDir string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.CurrentTaskID != "" && job.State == StateDatasetBuilding {
		return nil, errors.New("cannot replace backgrounds while dataset generation is active")
	}
	stableDir := filepath.Join(s.store.JobDir(id), "backgrounds", "current")
	backupDir, err := replaceWithBackup(stagedDir, stableDir)
	if err != nil {
		return nil, fmt.Errorf("commit backgrounds: %w", err)
	}
	oldDir := job.BackgroundDir
	job.BackgroundDir = stableDir
	job.DatasetZip = ""
	if a := job.ActiveAttemptRef(); a != nil {
		a.DatasetZip = ""
	}
	if job.RenderDir != "" {
		job.State = StateRenderReady
		job.Message = "backgrounds updated; dataset can be regenerated"
	}
	s.touch(job, time.Now().UTC())
	if err := s.jobs.Save(ctx, job); err != nil {
		_ = rollbackReplace(stableDir, backupDir)
		job.BackgroundDir = oldDir
		return nil, err
	}
	removeBackup(backupDir)
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) StartDataset(ctx context.Context, id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.RenderDir == "" || job.BackgroundDir == "" {
		return nil, errors.New("rendered RGBA views and background images are required")
	}
	if job.CurrentTaskID != "" && (job.State == StateRendering || job.State == StateDatasetBuilding || job.State == StateReconstructing) {
		return nil, errors.New("job already has an active processing task")
	}
	now := time.Now().UTC()
	taskID := newID("task")
	datasetDir := filepath.Join(s.store.AttemptDir(id, job.ActiveAttempt), "dataset")
	task := &tasks.Task{ID: taskID, JobID: id, Attempt: job.ActiveAttempt, Type: tasks.TypeDataset, State: tasks.StateReady, Payload: map[string]string{"renderDir": job.RenderDir, "backgroundDir": job.BackgroundDir, "datasetDir": datasetDir}, MaxAttempts: 2, LogPath: s.store.TaskLogPath(id, taskID), CreatedAt: now, UpdatedAt: now}
	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, err
	}
	job.State = StateDatasetBuilding
	job.Progress = 0
	job.Message = "synthetic dataset generation queued"
	job.Error = ""
	job.CurrentTaskID = taskID
	job.CancelRequested = false
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		_ = s.tasks.Delete(context.Background(), taskID)
		return nil, err
	}
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) Cancel(ctx context.Context, id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	job.CancelRequested = true
	job.Message = "cancellation requested"
	s.touch(job, time.Now().UTC())
	if err := s.tasks.CancelByJob(ctx, id); err != nil {
		return nil, err
	}
	if job.CurrentTaskID == "" {
		job.State = StateCancelled
	}
	if err := s.jobs.Save(ctx, job); err != nil {
		return nil, err
	}
	s.broker.Publish(id)
	return job, nil
}

func (s *Service) ClaimTask(ctx context.Context, workerID string) (*tasks.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	claimed, terminal, err := s.tasks.Claim(ctx, workerID, s.lease)
	if err != nil {
		return nil, err
	}
	for i := range terminal {
		if terminal[i].State == tasks.StateCancelled {
			_ = s.applyTaskCancellation(ctx, &terminal[i], "cancelled")
		} else {
			_ = s.applyTaskFailure(ctx, &terminal[i])
		}
	}
	return claimed, nil
}

func (s *Service) TaskHeartbeat(ctx context.Context, id, workerID string, progress float64, message string) (*tasks.Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.tasks.Heartbeat(ctx, id, workerID, s.lease, progress, message)
	if err != nil {
		return nil, false, err
	}
	job, err := s.jobs.FindByID(ctx, t.JobID)
	if err == nil {
		job.Progress = t.Progress
		job.Message = t.Message
		s.touch(job, time.Now().UTC())
		if err := s.jobs.Save(ctx, job); err != nil {
			return t, job.CancelRequested || t.CancelRequested, err
		}
		s.broker.Publish(job.ID)
		return t, job.CancelRequested || t.CancelRequested, nil
	}
	return t, t.CancelRequested, nil
}

func (s *Service) CompleteTask(ctx context.Context, id, workerID string, result map[string]string, message string) (*tasks.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.tasks.Complete(ctx, id, workerID, result, message)
	if err != nil {
		return nil, err
	}
	if err := s.applyTaskSuccess(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) FailTask(ctx context.Context, id, workerID, errText, message string) (*tasks.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.tasks.Fail(ctx, id, workerID, errText, message)
	if err != nil {
		return nil, err
	}
	if err := s.applyTaskFailure(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) CancelTask(ctx context.Context, id, workerID, message string) (*tasks.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.tasks.AcknowledgeCancel(ctx, id, workerID, message)
	if err != nil {
		return nil, err
	}
	if err := s.applyTaskCancellation(ctx, t, message); err != nil {
		return nil, err
	}
	return t, nil
}

// Reconcile repairs the small cross-file consistency window between terminal
// task metadata and job metadata. It is safe to run repeatedly and is used at
// startup and periodically by the app process.
func (s *Service) Reconcile(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.tasks.List(ctx)
	if err != nil {
		return err
	}
	for i := range all {
		t := &all[i]
		job, err := s.jobs.FindByID(ctx, t.JobID)
		if err != nil || job.CurrentTaskID != t.ID {
			continue
		}
		switch t.State {
		case tasks.StateSucceeded:
			if err := s.applyTaskSuccess(ctx, t); err != nil {
				return err
			}
		case tasks.StateFailed:
			if err := s.applyTaskFailure(ctx, t); err != nil {
				return err
			}
		case tasks.StateCancelled:
			if err := s.applyTaskCancellation(ctx, t, coalesce(t.Message, "cancelled")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) applyTaskSuccess(ctx context.Context, t *tasks.Task) error {
	job, err := s.jobs.FindByID(ctx, t.JobID)
	if err != nil {
		return err
	}
	if job.CurrentTaskID != t.ID {
		return nil // stale terminal task; never overwrite a newer job transition
	}
	now := time.Now().UTC()
	job.CurrentTaskID = ""
	job.Progress = 1
	job.Message = t.Message
	job.Error = ""
	job.CancelRequested = false
	switch t.Type {
	case tasks.TypeReconstruct:
		job.State = StateReviewReady
		if a := job.ActiveAttemptRef(); a != nil && a.Number == t.Attempt {
			a.State = StateReviewReady
			a.SplatPath = t.Result["splatPath"]
			a.ConfigPath = t.Result["configPath"]
			a.QualityPath = t.Result["qualityPath"]
			_, _ = fmt.Sscanf(t.Result["gaussianCount"], "%d", &a.GaussianCount)
			_, _ = fmt.Sscanf(t.Result["registrationRatio"], "%f", &a.RegistrationRatio)
			a.Error = ""
			a.UpdatedAt = now
		}
	case tasks.TypeRender:
		job.State = StateRenderReady
		job.RenderDir = t.Result["renderDir"]
		if a := job.ActiveAttemptRef(); a != nil && (t.Attempt == 0 || a.Number == t.Attempt) {
			a.RenderDir = job.RenderDir
			a.UpdatedAt = now
		}
	case tasks.TypeDataset:
		job.State = StateCompleted
		job.DatasetZip = t.Result["datasetZip"]
		if a := job.ActiveAttemptRef(); a != nil && (t.Attempt == 0 || a.Number == t.Attempt) {
			a.DatasetZip = job.DatasetZip
			a.UpdatedAt = now
		}
	default:
		return fmt.Errorf("unsupported successful task type %q", t.Type)
	}
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		return err
	}
	s.broker.Publish(job.ID)
	return nil
}

func (s *Service) applyTaskCancellation(ctx context.Context, t *tasks.Task, message string) error {
	job, err := s.jobs.FindByID(ctx, t.JobID)
	if err != nil {
		return err
	}
	if job.CurrentTaskID != t.ID {
		return nil
	}
	now := time.Now().UTC()
	job.State = StateCancelled
	job.Progress = t.Progress
	job.Message = message
	job.Error = ""
	job.CurrentTaskID = ""
	job.CancelRequested = false
	if t.Type == tasks.TypeReconstruct {
		if a := job.ActiveAttemptRef(); a != nil && a.Number == t.Attempt {
			a.State = StateCancelled
			a.Error = ""
			a.UpdatedAt = now
		}
	}
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		return err
	}
	s.broker.Publish(job.ID)
	return nil
}

func (s *Service) applyTaskFailure(ctx context.Context, t *tasks.Task) error {
	job, err := s.jobs.FindByID(ctx, t.JobID)
	if err != nil {
		return err
	}
	if job.CurrentTaskID != t.ID {
		return nil
	}
	now := time.Now().UTC()
	job.State = StateFailed
	job.Progress = t.Progress
	job.Message = t.Message
	job.Error = t.Error
	job.CurrentTaskID = ""
	if t.Type == tasks.TypeReconstruct {
		if a := job.ActiveAttemptRef(); a != nil && a.Number == t.Attempt {
			a.State = StateFailed
			a.Error = t.Error
			a.UpdatedAt = now
		}
	}
	s.touch(job, now)
	if err := s.jobs.Save(ctx, job); err != nil {
		return err
	}
	s.broker.Publish(job.ID)
	return nil
}

func coalesce(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// replaceWithBackup atomically commits a staged path on the same filesystem.
// If a previous destination exists it is moved aside so callers can roll back
// when the metadata commit fails. The returned backup path is empty when no
// prior destination existed.
func replaceWithBackup(staged, destination string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	backup := ""
	if _, err := os.Stat(destination); err == nil {
		backup = destination + ".backup-" + fmt.Sprint(time.Now().UnixNano())
		if err := os.Rename(destination, backup); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(staged, destination); err != nil {
		if backup != "" {
			_ = os.Rename(backup, destination)
		}
		return "", err
	}
	return backup, nil
}

func rollbackReplace(destination, backup string) error {
	_ = os.RemoveAll(destination)
	if backup == "" {
		return nil
	}
	return os.Rename(backup, destination)
}

func removeBackup(path string) {
	if path != "" {
		_ = os.RemoveAll(path)
	}
}

func (s *Service) touch(job *Job, now time.Time) { job.Revision++; job.UpdatedAt = now }
func newID(prefix string) string {
	now := time.Now().UTC()
	return fmt.Sprintf("%s_%s_%06d", prefix, now.Format("20060102T150405"), now.Nanosecond()%1000000)
}
