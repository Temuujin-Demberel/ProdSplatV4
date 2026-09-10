package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

var ErrNotFound = errors.New("job not found")
var ErrInvalidName = errors.New("job name is required")

type Repository interface {
	Save(context.Context, *Job) error
	FindByID(context.Context, string) (*Job, error)
	List(context.Context) ([]Job, error)
}

type FileRepository struct {
	root string
	mu   sync.RWMutex
}

func NewFileRepository(root string) (*FileRepository, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileRepository{root: root}, nil
}

func (r *FileRepository) path(id string) string {
	return filepath.Join(r.root, id, "job.json")
}

func (r *FileRepository) Save(ctx context.Context, job *Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked(job)
}

func (r *FileRepository) saveLocked(job *Job) error {
	if err := os.MkdirAll(filepath.Dir(r.path(job.ID)), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path(job.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path(job.ID))
}

func (r *FileRepository) FindByID(ctx context.Context, id string) (*Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.findLocked(id)
}

func (r *FileRepository) findLocked(id string) (*Job, error) {
	data, err := os.ReadFile(r.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *FileRepository) List(ctx context.Context) ([]Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries, err := os.ReadDir(r.root)
	if err != nil {
		return nil, err
	}
	result := make([]Job, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		job, err := r.findLocked(entry.Name())
		if err == nil {
			result = append(result, *job)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}
