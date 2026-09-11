package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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

func doGET(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func cleanedJob(t *testing.T, server *httptest.Server, name string) jobs.Job {
	t.Helper()
	resp := doJSON(t, "POST", server.URL+"/api/jobs", map[string]string{"name": name}, "")
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
	return job
}

func claimTask(t *testing.T, server *httptest.Server) tasks.Task {
	t.Helper()
	resp := doJSON(t, "POST", server.URL+"/internal/tasks/claim", map[string]string{"workerId": "w1"}, "secret")
	if resp.StatusCode != 200 {
		t.Fatalf("claim status %d", resp.StatusCode)
	}
	var task tasks.Task
	_ = json.NewDecoder(resp.Body).Decode(&task)
	resp.Body.Close()
	return task
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
	job := cleanedJob(t, server, "Bottle")

	resp := doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", map[string]any{"assetName": "Bottle Test", "upAxis": "+y", "frontAzimuthDegrees": 45}, "")
	if resp.StatusCode != 202 {
		t.Fatalf("render status %d", resp.StatusCode)
	}
	resp.Body.Close()

	task := claimTask(t, server)
	if task.Type != tasks.TypeRender || task.JobID != job.ID {
		t.Fatalf("wrong task %+v", task)
	}
	if task.Payload["assetName"] != "Bottle_Test" || task.Payload["upAxis"] != "+y" || task.Payload["frontAzimuthDegrees"] != "45" {
		t.Fatalf("bad render payload %+v", task.Payload)
	}

	resp = doJSON(t, "POST", server.URL+"/internal/tasks/"+task.ID+"/heartbeat", map[string]any{"workerId": "w1", "progress": 0.5, "message": "half"}, "secret")
	if resp.StatusCode != 200 {
		t.Fatalf("heartbeat status %d", resp.StatusCode)
	}
	resp.Body.Close()

	renderDir := task.Payload["renderDir"]
	resp = doJSON(t, "POST", server.URL+"/internal/tasks/"+task.ID+"/complete", map[string]any{"workerId": "w1", "result": map[string]string{"renderDir": renderDir, "viewCount": "48", "assetName": "Bottle_Test"}, "message": "done"}, "secret")
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
	if got.RenderOptions == nil || got.RenderOptions.AssetName != "Bottle_Test" || got.RenderOptions.FrontAzimuthDegrees != 45 {
		t.Fatalf("job render options not persisted: %+v", got.RenderOptions)
	}
	attempt := got.ActiveAttemptRef()
	if attempt == nil || attempt.RenderOptions == nil || attempt.RenderOptions.UpAxis != "+y" {
		t.Fatalf("attempt render options not persisted: %+v", attempt)
	}
}

func TestStartRenderWithoutBodyUsesDefaults(t *testing.T) {
	server, _ := setupTestAPI(t)
	defer server.Close()
	job := cleanedJob(t, server, "Bottle")

	resp := doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", nil, "")
	if resp.StatusCode != 202 {
		t.Fatalf("render status %d", resp.StatusCode)
	}
	resp.Body.Close()

	task := claimTask(t, server)
	if task.Payload["assetName"] != "Bottle" || task.Payload["upAxis"] != "+z" || task.Payload["frontAzimuthDegrees"] != "0" {
		t.Fatalf("bad default payload %+v", task.Payload)
	}
}

func TestStartRenderRejectsInvalidOptions(t *testing.T) {
	server, _ := setupTestAPI(t)
	defer server.Close()
	job := cleanedJob(t, server, "Bottle")

	invalid := []map[string]any{
		{"upAxis": "+w"},
		{"frontAzimuthDegrees": 10},
		{"assetName": "---"},
		{"bogus": 1},
	}
	for _, body := range invalid {
		resp := doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", body, "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %v: status %d, want 400", body, resp.StatusCode)
		}
	}
}

func TestPlyRoutesAndManifestName(t *testing.T) {
	server, _ := setupTestAPI(t)
	defer server.Close()
	job := cleanedJob(t, server, "Bottle")

	for _, path := range []string{"/splat.ply", "/cleaned.ply"} {
		resp := doGET(t, server.URL+"/api/jobs/"+job.ID+path)
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("%s: status %d content-type %q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}

	resp := doGET(t, server.URL+"/api/jobs/"+job.ID+"/renders/render_manifest.json")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("manifest before render: status %d, want 404", resp.StatusCode)
	}

	resp = doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", nil, "")
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("render status %d", resp.StatusCode)
	}

	resp = doGET(t, server.URL+"/api/jobs/"+job.ID+"/renders/notes.txt")
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("notes.txt: status %d, want 400", resp.StatusCode)
	}
	resp = doGET(t, server.URL+"/api/jobs/"+job.ID+"/renders/render_manifest.json")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("missing manifest: status %d, want 404", resp.StatusCode)
	}
}

func reconstructedVideoJob(t *testing.T, server *httptest.Server, name string) jobs.Job {
	t.Helper()
	resp := doJSON(t, "POST", server.URL+"/api/jobs", map[string]string{"name": name}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("create status %d", resp.StatusCode)
	}
	var job jobs.Job
	_ = json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("profile", "fast")
	part, _ := mw.CreateFormFile("file", "clip.mp4")
	_, _ = part.Write([]byte("not really a video"))
	_ = mw.Close()
	req, _ := http.NewRequest("POST", server.URL+"/api/jobs/"+job.ID+"/video", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 202 {
		t.Fatalf("video status %d", resp.StatusCode)
	}
	resp.Body.Close()

	task := claimTask(t, server)
	if task.Type != tasks.TypeReconstruct {
		t.Fatalf("expected reconstruct task, got %+v", task)
	}
	splatPath := filepath.Join(task.Payload["attemptDir"], "splat.ply")
	if err := os.WriteFile(splatPath, gaussianPLY(), 0o644); err != nil {
		t.Fatal(err)
	}
	result := map[string]string{"splatPath": splatPath, "configPath": "", "qualityPath": "", "gaussianCount": "1", "registrationRatio": "0.9"}
	resp = doJSON(t, "POST", server.URL+"/internal/tasks/"+task.ID+"/complete", map[string]any{"workerId": "w1", "result": result, "message": "done"}, "secret")
	if resp.StatusCode != 200 {
		t.Fatalf("reconstruct complete status %d", resp.StatusCode)
	}
	resp.Body.Close()
	return job
}

func TestAutoIsolateRendersWithoutCleanedAsset(t *testing.T) {
	server, service := setupTestAPI(t)
	defer server.Close()
	job := reconstructedVideoJob(t, server, "Can")

	resp := doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", map[string]any{"isolate": false}, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("render without cleaned and without isolate: status %d, want 400", resp.StatusCode)
	}

	resp = doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", map[string]any{"isolate": true, "supportColor": "#1F4FD8"}, "")
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("render with isolate: status %d", resp.StatusCode)
	}
	task := claimTask(t, server)
	if task.Type != tasks.TypeRender || task.Payload["isolate"] != "1" || task.Payload["attemptDir"] == "" || task.Payload["supportColor"] != "#1f4fd8" {
		t.Fatalf("bad render task %+v", task)
	}
	if !strings.HasSuffix(filepath.ToSlash(task.Payload["splatPath"]), "/attempts/001/splat.ply") {
		t.Fatalf("render source should be the reconstruction: %s", task.Payload["splatPath"])
	}
	isolatedPath := filepath.Join(task.Payload["attemptDir"], "isolated.ply")
	if err := os.WriteFile(isolatedPath, gaussianPLY(), 0o644); err != nil {
		t.Fatal(err)
	}
	result := map[string]string{"renderDir": task.Payload["renderDir"], "viewCount": "48", "isolatedPath": isolatedPath, "isolatedCount": "1"}
	resp = doJSON(t, "POST", server.URL+"/internal/tasks/"+task.ID+"/complete", map[string]any{"workerId": "w1", "result": result, "message": "done"}, "secret")
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("complete status %d", resp.StatusCode)
	}

	got, err := service.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != jobs.StateRenderReady || got.IsolatedPath != isolatedPath {
		t.Fatalf("isolated path not persisted: %+v", got)
	}
	if a := got.ActiveAttemptRef(); a == nil || a.IsolatedPath != isolatedPath || a.RenderOptions == nil || !a.RenderOptions.Isolate {
		t.Fatalf("attempt isolate state not persisted: %+v", a)
	}
	resp = doGET(t, server.URL+"/api/jobs/"+job.ID+"/isolated.ply")
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("isolated.ply: status %d content-type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestAutoIsolateRejectedForUploadedPLY(t *testing.T) {
	server, _ := setupTestAPI(t)
	defer server.Close()
	job := cleanedJob(t, server, "Bottle")
	resp := doJSON(t, "POST", server.URL+"/api/jobs/"+job.ID+"/render", map[string]any{"isolate": true}, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("isolate on a PLY attempt: status %d, want 400", resp.StatusCode)
	}
}
