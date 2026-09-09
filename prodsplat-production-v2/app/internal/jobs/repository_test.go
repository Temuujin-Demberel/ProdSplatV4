package jobs

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFileRepositoryRoundTrip(t *testing.T) {
	repo, err := NewFileRepository(filepath.Join(t.TempDir(), "jobs"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	j := &Job{ID: "j1", Name: "Bottle", State: StateCreated, CreatedAt: now, UpdatedAt: now}
	if err := repo.Save(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(context.Background(), "j1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Bottle" || got.State != StateCreated {
		t.Fatalf("bad roundtrip: %+v", got)
	}
}
