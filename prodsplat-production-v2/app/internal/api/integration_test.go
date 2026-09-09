package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/demberel-temuujin/prodsplat-production/app/internal/config"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/events"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/jobs"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/storage"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/system"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/tasks"
)

func setupTestAPI(t *testing.T) (*httptest.Server, *jobs.Service) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	jr, err := jobs.NewFileRepository(store.JobsRoot())
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tasks.NewFileRepository(store.TasksRoot())
	if err != nil {
		t.Fatal(err)
	}
	broker := events.NewBroker()
	monitor := system.NewWorkerMonitor()
	service := jobs.NewService(jr, tr, store, broker, time.Minute, "balanced")
	h := NewHandler(service, store, broker, monitor, config.Config{InternalToken: "secret", MaxUploadBytes: 10 << 20, TaskLease: time.Minute, DefaultProfile: "balanced"})
	mux := http.NewServeMux()
	RegisterRoutes(mux, h)
	return httptest.NewServer(mux), service
}

func gaussianPLY() []byte {
	return []byte("ply\nformat ascii 1.0\nelement vertex 1\n" +
		"property float x\nproperty float y\nproperty float z\n" +
		"property float f_dc_0\nproperty float f_dc_1\nproperty float f_dc_2\n" +
		"property float opacity\nproperty float scale_0\nproperty float scale_1\nproperty float scale_2\n" +
		"property float rot_0\nproperty float rot_1\nproperty float rot_2\nproperty float rot_3\n" +
		"end_header\n0 0 0 0 0 0 1 -1 -1 -1 1 0 0 0\n")
}

func doJSON(t *testing.T, method, url string, body any, token string) *http.Response {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCreateJobRejectsBlankName(t *testing.T) {
	server, _ := setupTestAPI(t)
	defer server.Close()
	resp := doJSON(t, "POST", server.URL+"/api/jobs", map[string]string{"name": "   "}, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create blank-name status %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestExistingPLYToDurableRenderTask(t *testing.T) {
	server, service := setupTestAPI(t)
	defer server.Close()
	resp := doJSON(t, "POST", server.URL+"/api/jobs", map[string]string{"name": "Bottle"}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("create status %d", resp.StatusCode)
	}
	var job jobs.Job
	_ = json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "model.ply")
	_, _ = part.Write(gaussianPLY())
	_ = mw.Close()
	req, _ := http.NewRequest("POST", server.URL+"/api/jobs/"+job.ID+"/ply", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("ply status %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ = http.NewRequest("POST", server.URL+"/api/jobs/"+job.ID+"/cleaned", bytes.NewReader(gaussianPLY()))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("clean status %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", nil, "")
	if resp.StatusCode != 202 {
		t.Fatalf("render status %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doJSON(t, "POST", server.URL+"/internal/tasks/claim", map[string]string{"workerId": "w1"}, "secret")
	if resp.StatusCode != 200 {
		t.Fatalf("claim status %d", resp.StatusCode)
	}
	var task tasks.Task
	_ = json.NewDecoder(resp.Body).Decode(&task)
	resp.Body.Close()
	if task.Type != tasks.TypeRender || task.JobID != job.ID {
		t.Fatalf("wrong task %+v", task)
	}

	resp = doJSON(t, "POST", server.URL+"/internal/tasks/"+task.ID+"/heartbeat", map[string]any{"workerId": "w1", "progress": 0.5, "message": "half"}, "secret")
	if resp.StatusCode != 200 {
		t.Fatalf("heartbeat status %d", resp.StatusCode)
	}
	resp.Body.Close()

	renderDir := task.Payload["renderDir"]
	resp = doJSON(t, "POST", server.URL+"/internal/tasks/"+task.ID+"/complete", map[string]any{"workerId": "w1", "result": map[string]string{"renderDir": renderDir, "viewCount": "32"}, "message": "done"}, "secret")
	if resp.StatusCode != 200 {
		t.Fatalf("complete status %d", resp.StatusCode)
	}
	resp.Body.Close()

	got, err := service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != jobs.StateRenderReady || got.CurrentTaskID != "" {
		t.Fatalf("bad final job %+v", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(got.CleanedPath), "/cleaned.ply") {
		t.Fatalf("bad cleaned path %s", got.CleanedPath)
	}
}
