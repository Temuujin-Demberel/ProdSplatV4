package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("task not found")
var ErrLeaseOwner = errors.New("task lease is owned by another worker")

type Repository interface {
	Create(context.Context, *Task) error
	Delete(context.Context, string) error
	Find(context.Context, string) (*Task, error)
	List(context.Context) ([]Task, error)
	ListByJob(context.Context, string) ([]Task, error)
	Claim(context.Context, string, time.Duration) (*Task, []Task, error)
	Heartbeat(context.Context, string, string, time.Duration, float64, string) (*Task, error)
	Complete(context.Context, string, string, map[string]string, string) (*Task, error)
	Fail(context.Context, string, string, string, string) (*Task, error)
	AcknowledgeCancel(context.Context, string, string, string) (*Task, error)
	CancelByJob(context.Context, string) error
}

type FileRepository struct {
	root string
	mu   sync.Mutex
}

func NewFileRepository(root string) (*FileRepository, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileRepository{root: root}, nil
}

func (r *FileRepository) path(id string) string { return filepath.Join(r.root, id+".json") }

func (r *FileRepository) Create(ctx context.Context, task *Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := os.Stat(r.path(task.ID)); err == nil {
		return errors.New("task already exists")
	}
	return r.saveLocked(task)
}

func (r *FileRepository) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	err := os.Remove(r.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (r *FileRepository) Find(ctx context.Context, id string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findLocked(id)
}

func (r *FileRepository) List(ctx context.Context) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listLocked()
}

func (r *FileRepository) ListByJob(ctx context.Context, jobID string) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.listLocked()
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for _, t := range all {
		if t.JobID == jobID {
			out = append(out, t)
		}
	}
	return out, nil
}

// Claim provides durable at-least-once delivery. Expired running tasks are
// reclaimable until MaxAttempts is exhausted. terminal contains tasks that
// became permanently failed because their leases expired too many times.
func (r *FileRepository) Claim(ctx context.Context, workerID string, lease time.Duration) (*Task, []Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.listLocked()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	terminal := []Task{}

	for i := range all {
		t := &all[i]
		expired := t.State == StateRunning && !t.LeaseUntil.IsZero() && now.After(t.LeaseUntil)
		if t.CancelRequested && (t.State == StateReady || expired) {
			t.State = StateCancelled
			t.Error = ""
			t.Message = "cancelled"
			t.LeaseUntil = time.Time{}
			t.UpdatedAt = now
			t.FinishedAt = ptrTime(now)
			_ = r.saveLocked(t)
			terminal = append(terminal, *t)
			continue
		}
		if expired && t.AttemptCount >= t.MaxAttempts {
			t.State = StateFailed
			t.Error = "worker lease expired after maximum delivery attempts"
			t.Message = "task exhausted retries"
			t.UpdatedAt = now
			t.FinishedAt = ptrTime(now)
			_ = r.saveLocked(t)
			terminal = append(terminal, *t)
		}
	}

	for i := range all {
		t := &all[i]
		claimable := t.State == StateReady || (t.State == StateRunning && !t.LeaseUntil.IsZero() && now.After(t.LeaseUntil) && t.AttemptCount < t.MaxAttempts)
		if !claimable || t.CancelRequested {
			continue
		}
		t.State = StateRunning
		t.AttemptCount++
		t.LeaseOwner = workerID
		t.LeaseUntil = now.Add(lease)
		t.UpdatedAt = now
		if t.StartedAt == nil {
			t.StartedAt = ptrTime(now)
		}
		if err := r.saveLocked(t); err != nil {
			return nil, terminal, err
		}
		copy := *t
		return &copy, terminal, nil
	}
	return nil, terminal, nil
}

func (r *FileRepository) Heartbeat(ctx context.Context, id, workerID string, lease time.Duration, progress float64, message string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.findLocked(id)
	if err != nil {
		return nil, err
	}
	if t.State != StateRunning || t.LeaseOwner != workerID {
		return nil, ErrLeaseOwner
	}
	now := time.Now().UTC()
	t.LeaseUntil = now.Add(lease)
	if progress >= 0 && progress <= 1 {
		t.Progress = progress
	}
	if message != "" {
		t.Message = message
	}
	t.UpdatedAt = now
	if err := r.saveLocked(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *FileRepository) Complete(ctx context.Context, id, workerID string, result map[string]string, message string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.findLocked(id)
	if err != nil {
		return nil, err
	}
	if t.State != StateRunning || t.LeaseOwner != workerID {
		return nil, ErrLeaseOwner
	}
	now := time.Now().UTC()
	t.State = StateSucceeded
	t.Progress = 1
	t.Result = result
	t.Message = message
	t.LeaseUntil = time.Time{}
	t.UpdatedAt = now
	t.FinishedAt = ptrTime(now)
	if err := r.saveLocked(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *FileRepository) Fail(ctx context.Context, id, workerID, errText, message string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.findLocked(id)
	if err != nil {
		return nil, err
	}
	if t.State != StateRunning || t.LeaseOwner != workerID {
		return nil, ErrLeaseOwner
	}
	now := time.Now().UTC()
	t.State = StateFailed
	t.Error = errText
	t.Message = message
	t.LeaseUntil = time.Time{}
	t.UpdatedAt = now
	t.FinishedAt = ptrTime(now)
	if err := r.saveLocked(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *FileRepository) AcknowledgeCancel(ctx context.Context, id, workerID, message string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t, err := r.findLocked(id)
	if err != nil {
		return nil, err
	}
	if t.State != StateRunning || t.LeaseOwner != workerID {
		return nil, ErrLeaseOwner
	}
	now := time.Now().UTC()
	t.State = StateCancelled
	t.Message = message
	t.Error = ""
	t.LeaseUntil = time.Time{}
	t.UpdatedAt = now
	t.FinishedAt = ptrTime(now)
	if err := r.saveLocked(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *FileRepository) CancelByJob(ctx context.Context, jobID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.listLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for i := range all {
		t := &all[i]
		if t.JobID != jobID || t.State == StateSucceeded || t.State == StateFailed || t.State == StateCancelled {
			continue
		}
		t.CancelRequested = true
		if t.State == StateReady {
			t.State = StateCancelled
			t.FinishedAt = ptrTime(now)
		}
		t.UpdatedAt = now
		if err := r.saveLocked(t); err != nil {
			return err
		}
	}
	return nil
}

func (r *FileRepository) listLocked() ([]Task, error) {
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.root, entry.Name()))
		if err != nil {
			continue
		}
		var t Task
		if json.Unmarshal(data, &t) == nil {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *FileRepository) findLocked(id string) (*Task, error) {
	data, err := os.ReadFile(r.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *FileRepository) saveLocked(task *Task) error {
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path(task.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path(task.ID))
}

func ptrTime(t time.Time) *time.Time { return &t }
