package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicSaveAndTraversal(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.JobDir("j"), "x.txt")
	if err := store.Save(context.Background(), path, bytes.NewBufferString("hello")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "hello" {
		t.Fatalf("bad save: %q %v", b, err)
	}
	if store.Allowed(filepath.Join(store.Root(), "..", "escape")) {
		t.Fatal("traversal accepted")
	}
}

func TestGaussianPLYValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.ply")
	header := "ply\nformat ascii 1.0\nelement vertex 1\n" +
		"property float x\nproperty float y\nproperty float z\n" +
		"property float f_dc_0\nproperty float f_dc_1\nproperty float f_dc_2\n" +
		"property float opacity\nproperty float scale_0\nproperty float scale_1\nproperty float scale_2\n" +
		"property float rot_0\nproperty float rot_1\nproperty float rot_2\nproperty float rot_3\nend_header\n" +
		"0 0 0 0 0 0 1 -1 -1 -1 1 0 0 0\n"
	if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGaussianPLY(path); err != nil {
		t.Fatal(err)
	}
}
