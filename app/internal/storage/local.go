package storage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{"jobs", "tasks", "runtime"} {
		if err := os.MkdirAll(filepath.Join(abs, d), 0o755); err != nil {
			return nil, err
		}
	}
	return &Local{root: abs}, nil
}

func (s *Local) Root() string            { return s.root }
func (s *Local) JobsRoot() string        { return filepath.Join(s.root, "jobs") }
func (s *Local) TasksRoot() string       { return filepath.Join(s.root, "tasks") }
func (s *Local) JobDir(id string) string { return filepath.Join(s.JobsRoot(), id) }
func (s *Local) AttemptDir(id string, n int) string {
	return filepath.Join(s.JobDir(id), "attempts", fmt.Sprintf("%03d", n))
}
func (s *Local) TaskLogPath(jobID, taskID string) string {
	return filepath.Join(s.JobDir(jobID), "logs", taskID+".log")
}

func (s *Local) Allowed(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(s.root, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Local) Save(ctx context.Context, path string, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.Allowed(path) {
		return errors.New("refusing to write outside workspace")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, path)
}

func ValidateGaussianPLY(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Gaussian PLY headers are small; cap malicious headers.
	scanner.Buffer(make([]byte, 4096), 1<<20)
	props := map[string]bool{}
	formatOK := false
	vertexCount := 0
	foundEnd := false
	lineCount := 0

	for scanner.Scan() {
		lineCount++
		if lineCount > 4096 {
			return errors.New("PLY header is unreasonably large")
		}
		line := strings.TrimSpace(scanner.Text())
		if lineCount == 1 && line != "ply" {
			return errors.New("not a PLY file")
		}
		if strings.HasPrefix(line, "format ") {
			formatOK = strings.Contains(line, "binary_little_endian") || strings.Contains(line, "ascii")
		}
		if strings.HasPrefix(line, "element vertex ") {
			_, _ = fmt.Sscanf(line, "element vertex %d", &vertexCount)
		}
		if strings.HasPrefix(line, "property ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 && parts[1] != "list" {
				props[parts[len(parts)-1]] = true
			}
		}
		if line == "end_header" {
			foundEnd = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !foundEnd || !formatOK || vertexCount <= 0 {
		return errors.New("invalid or unsupported PLY header")
	}

	required := []string{"x", "y", "z", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"}
	for _, p := range required {
		if !props[p] {
			return fmt.Errorf("not a Gaussian-splat PLY: missing %s", p)
		}
	}
	hasSH := props["f_dc_0"] && props["f_dc_1"] && props["f_dc_2"]
	hasRGB := props["red"] && props["green"] && props["blue"]
	if !hasSH && !hasRGB {
		return errors.New("not a Gaussian-splat PLY: missing SH DC or RGB color fields")
	}
	return nil
}

func VideoExtensionAllowed(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mov", ".m4v", ".mkv", ".webm":
		return true
	default:
		return false
	}
}

func ImageExtensionAllowed(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	default:
		return false
	}
}
