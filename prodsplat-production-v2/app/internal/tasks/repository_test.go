package tasks

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimHeartbeatComplete(t *testing.T) {
	repo, err := NewFileRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := &Task{ID: "t1", JobID: "j1", Type: TypeRender, State: StateReady, Payload: map[string]string{}, MaxAttempts: 2, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	claimed, terminal, err := repo.Claim(context.Background(), "w1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(terminal) != 0 || claimed == nil || claimed.State != StateRunning || claimed.AttemptCount != 1 {
		t.Fatalf("bad claim: %+v terminal=%+v", claimed, terminal)
	}
	hb, err := repo.Heartbeat(context.Background(), "t1", "w1", time.Minute, .5, "half")
	if err != nil {
		t.Fatal(err)
	}
	if hb.Progress != .5 || hb.Message != "half" {
		t.Fatalf("bad heartbeat: %+v", hb)
	}
	done, err := repo.Complete(context.Background(), "t1", "w1", map[string]string{"x": "y"}, "done")
	if err != nil {
		t.Fatal(err)
	}
	if done.State != StateSucceeded || done.Result["x"] != "y" {
		t.Fatalf("bad completion: %+v", done)
	}
}

func TestExpiredLeaseIsReclaimed(t *testing.T) {
	repo, err := NewFileRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := &Task{ID: "t1", JobID: "j1", Type: TypeRender, State: StateRunning, Payload: map[string]string{}, MaxAttempts: 2, AttemptCount: 1, LeaseOwner: "dead", LeaseUntil: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)}
	if err := repo.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := repo.Claim(context.Background(), "w2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.LeaseOwner != "w2" || claimed.AttemptCount != 2 {
		t.Fatalf("not reclaimed: %+v", claimed)
	}
}
