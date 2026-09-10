package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/demberel-temuujin/prodsplat-production/app/internal/buildinfo"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/config"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/events"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/jobs"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/storage"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/system"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/tasks"
)

type Handler struct {
	jobs    *jobs.Service
	store   *storage.Local
	broker  *events.Broker
	monitor *system.WorkerMonitor
	cfg     config.Config
}

func NewHandler(service *jobs.Service, store *storage.Local, broker *events.Broker, monitor *system.WorkerMonitor, cfg config.Config) *Handler {
	return &Handler{jobs: service, store: store, broker: broker, monitor: monitor, cfg: cfg}
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "prodsplat-app"})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	worker := h.monitor.Snapshot()
	if !worker.Online || !worker.Healthy {
		writeJSON(w, 503, map[string]any{"ok": false, "worker": worker})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "worker": worker})
}

func (h *Handler) System(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"app": map[string]string{"version": buildinfo.Version, "commit": buildinfo.Commit}, "worker": h.monitor.Snapshot(), "profiles": jobs.Profiles})
}

func (h *Handler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	job, err := h.jobs.Create(r.Context(), body.Name)
	if errors.Is(err, jobs.ErrInvalidName) {
		badRequest(w, err)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 201, job)
}
func (h *Handler) ListJobs(w http.ResponseWriter, r *http.Request) {
	v, err := h.jobs.List(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, jobs.ErrNotFound) {
		http.Error(w, "job not found", 404)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, job)
}
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	v, err := h.jobs.ListTasks(r.Context(), r.PathValue("id"))
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *Handler) UploadVideo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.jobs.Get(r.Context(), id); err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		badRequest(w, errors.New("expected multipart/form-data upload"))
		return
	}

	profile := h.cfg.DefaultProfile
	var stagedPath string
	foundFile := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			removeIfSet(stagedPath)
			badRequest(w, err)
			return
		}
		name := part.FormName()
		if name == "profile" && part.FileName() == "" {
			b, _ := io.ReadAll(io.LimitReader(part, 1024))
			if v := strings.TrimSpace(string(b)); v != "" {
				profile = v
			}
			_ = part.Close()
			continue
		}
		if name != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if foundFile {
			_ = part.Close()
			removeIfSet(stagedPath)
			badRequest(w, errors.New("only one video file is allowed"))
			return
		}
		if !storage.VideoExtensionAllowed(part.FileName()) {
			_ = part.Close()
			badRequest(w, errors.New("unsupported video extension"))
			return
		}
		ext := strings.ToLower(filepath.Ext(part.FileName()))
		stagedPath, err = h.incomingPath(id, "video", ext)
		if err != nil {
			_ = part.Close()
			serverError(w, err)
			return
		}
		if err := h.store.Save(r.Context(), stagedPath, part); err != nil {
			_ = part.Close()
			serverError(w, err)
			return
		}
		_ = part.Close()
		foundFile = true
	}
	if !foundFile {
		badRequest(w, errors.New("missing multipart field 'file'"))
		return
	}
	if _, err := jobs.Profile(profile); err != nil {
		removeIfSet(stagedPath)
		badRequest(w, err)
		return
	}
	updated, err := h.jobs.AddVideo(r.Context(), id, stagedPath, profile)
	if err != nil {
		removeIfSet(stagedPath)
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, updated)
}

func (h *Handler) UploadPLY(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.jobs.Get(r.Context(), id); err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		badRequest(w, errors.New("expected multipart/form-data upload"))
		return
	}
	var stagedPath string
	found := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			removeIfSet(stagedPath)
			badRequest(w, err)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if found {
			_ = part.Close()
			removeIfSet(stagedPath)
			badRequest(w, errors.New("only one PLY is allowed"))
			return
		}
		if strings.ToLower(filepath.Ext(part.FileName())) != ".ply" {
			_ = part.Close()
			badRequest(w, errors.New("file must use .ply extension"))
			return
		}
		stagedPath, err = h.incomingPath(id, "splat", ".ply")
		if err != nil {
			_ = part.Close()
			serverError(w, err)
			return
		}
		if err := h.store.Save(r.Context(), stagedPath, part); err != nil {
			_ = part.Close()
			serverError(w, err)
			return
		}
		_ = part.Close()
		found = true
	}
	if !found {
		badRequest(w, errors.New("missing multipart field 'file'"))
		return
	}
	if err := storage.ValidateGaussianPLY(stagedPath); err != nil {
		removeIfSet(stagedPath)
		badRequest(w, err)
		return
	}
	updated, err := h.jobs.AddExistingPLY(r.Context(), id, stagedPath)
	if err != nil {
		removeIfSet(stagedPath)
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, updated)
}

func (h *Handler) ActivateAttempt(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		badRequest(w, errors.New("invalid attempt number"))
		return
	}
	job, err := h.jobs.ActivateAttempt(r.Context(), r.PathValue("id"), number)
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) GetSplat(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, "job not found", 404)
		return
	}
	a := job.ActiveAttemptRef()
	if a == nil || a.SplatPath == "" {
		http.Error(w, "splat not ready", 404)
		return
	}
	serveAsset(w, r, a.SplatPath, "model.ply")
}
func (h *Handler) GetCleaned(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job.CleanedPath == "" {
		http.Error(w, "cleaned splat not found", 404)
		return
	}
	serveAsset(w, r, job.CleanedPath, "cleaned.ply")
}

func (h *Handler) SaveCleaned(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.jobs.Get(r.Context(), id); err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadBytes)
	stagedPath, err := h.incomingPath(id, "cleaned", ".ply")
	if err != nil {
		serverError(w, err)
		return
	}
	if err := h.store.Save(r.Context(), stagedPath, r.Body); err != nil {
		serverError(w, err)
		return
	}
	if err := storage.ValidateGaussianPLY(stagedPath); err != nil {
		removeIfSet(stagedPath)
		badRequest(w, err)
		return
	}
	job, err := h.jobs.SaveCleaned(r.Context(), id, stagedPath)
	if err != nil {
		removeIfSet(stagedPath)
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) StartRender(w http.ResponseWriter, r *http.Request) {
	var body jobs.RenderOptions
	var requested *jobs.RenderOptions
	switch err := decodeJSON(r, &body); {
	case errors.Is(err, io.EOF):
	case err != nil:
		badRequest(w, err)
		return
	default:
		requested = &body
	}
	job, err := h.jobs.StartRender(r.Context(), r.PathValue("id"), requested)
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, 202, job)
}

func (h *Handler) UploadBackgrounds(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.jobs.Get(r.Context(), id); err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		badRequest(w, errors.New("expected multipart/form-data upload"))
		return
	}
	token, err := randomToken(8)
	if err != nil {
		serverError(w, err)
		return
	}
	stagedDir := filepath.Join(h.store.JobDir(id), "incoming", "backgrounds_"+token)
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		serverError(w, err)
		return
	}
	count := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = os.RemoveAll(stagedDir)
			badRequest(w, err)
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if !storage.ImageExtensionAllowed(part.FileName()) {
			_ = part.Close()
			_ = os.RemoveAll(stagedDir)
			badRequest(w, fmt.Errorf("unsupported image extension: %s", filepath.Ext(part.FileName())))
			return
		}
		count++
		ext := strings.ToLower(filepath.Ext(part.FileName()))
		path := filepath.Join(stagedDir, fmt.Sprintf("%03d%s", count, ext))
		if err := h.store.Save(r.Context(), path, part); err != nil {
			_ = part.Close()
			_ = os.RemoveAll(stagedDir)
			serverError(w, err)
			return
		}
		_ = part.Close()
	}
	if count == 0 {
		_ = os.RemoveAll(stagedDir)
		badRequest(w, errors.New("missing multipart field 'files'"))
		return
	}
	job, err := h.jobs.SetBackgroundDir(r.Context(), id, stagedDir)
	if err != nil {
		_ = os.RemoveAll(stagedDir)
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) StartDataset(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.StartDataset(r.Context(), r.PathValue("id"))
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, 202, job)
}
func (h *Handler) DownloadDataset(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job.DatasetZip == "" {
		http.Error(w, "dataset not ready", 404)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="dataset.zip"`)
	http.ServeFile(w, r, job.DatasetZip)
}
func (h *Handler) CancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, 202, job)
}

func (h *Handler) TaskLog(w http.ResponseWriter, r *http.Request) {
	t, err := h.jobs.Task(r.Context(), r.PathValue("taskID"))
	if err != nil {
		http.Error(w, "task not found", 404)
		return
	}
	if t.LogPath == "" {
		http.Error(w, "log unavailable", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, t.LogPath)
}

func (h *Handler) RenderAsset(w http.ResponseWriter, r *http.Request) {
	job, err := h.jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job.RenderDir == "" {
		http.Error(w, "renders unavailable", 404)
		return
	}
	name := filepath.Base(r.PathValue("name"))
	isPNG := strings.HasSuffix(strings.ToLower(name), ".png")
	if name != r.PathValue("name") || !(isPNG || name == "render_manifest.json") {
		http.Error(w, "invalid render name", 400)
		return
	}
	path := filepath.Join(job.RenderDir, name)
	if !h.store.Allowed(path) {
		http.Error(w, "invalid path", 400)
		return
	}
	serveAsset(w, r, path, name)
}

func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	id, ch := h.broker.Subscribe()
	defer h.broker.Unsubscribe(id)
	_, _ = fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case jobID, ok := <-ch:
			if !ok {
				return
			}
			payload, _ := json.Marshal(map[string]string{"jobId": jobID})
			_, _ = fmt.Fprintf(w, "event: job\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) incomingPath(jobID, prefix, ext string) (string, error) {
	token, err := randomToken(8)
	if err != nil {
		return "", err
	}
	return filepath.Join(h.store.JobDir(jobID), "incoming", prefix+"_"+token+ext), nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func removeIfSet(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// -----------------------------------------------------------------------------
// Internal worker API
// -----------------------------------------------------------------------------

type workerHeartbeat struct {
	WorkerID      string            `json:"workerId"`
	Healthy       bool              `json:"healthy"`
	Error         string            `json:"error,omitempty"`
	GPU           map[string]string `json:"gpu,omitempty"`
	Versions      map[string]string `json:"versions,omitempty"`
	CurrentTaskID string            `json:"currentTaskId,omitempty"`
}

func (h *Handler) WorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !h.internalAuthorized(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var body workerHeartbeat
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	h.monitor.Update(system.WorkerInfo{WorkerID: body.WorkerID, Healthy: body.Healthy, Error: body.Error, GPU: body.GPU, Versions: body.Versions, CurrentTaskID: body.CurrentTaskID})
	w.WriteHeader(204)
}

type claimRequest struct {
	WorkerID string `json:"workerId"`
}

func (h *Handler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	if !h.internalAuthorized(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var body claimRequest
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.WorkerID) == "" {
		badRequest(w, errors.New("workerId required"))
		return
	}
	t, err := h.jobs.ClaimTask(r.Context(), body.WorkerID)
	if err != nil {
		serverError(w, err)
		return
	}
	if t == nil {
		w.WriteHeader(204)
		return
	}
	writeJSON(w, 200, t)
}

type taskHeartbeatRequest struct {
	WorkerID string  `json:"workerId"`
	Progress float64 `json:"progress"`
	Message  string  `json:"message"`
}

func (h *Handler) TaskHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !h.internalAuthorized(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var body taskHeartbeatRequest
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	t, cancel, err := h.jobs.TaskHeartbeat(r.Context(), r.PathValue("taskID"), body.WorkerID, body.Progress, body.Message)
	if errors.Is(err, tasks.ErrLeaseOwner) {
		http.Error(w, "lease lost", 409)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"task": t, "cancelRequested": cancel})
}

type taskCompleteRequest struct {
	WorkerID string            `json:"workerId"`
	Result   map[string]string `json:"result"`
	Message  string            `json:"message"`
}

func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	if !h.internalAuthorized(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var body taskCompleteRequest
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	t, err := h.jobs.CompleteTask(r.Context(), r.PathValue("taskID"), body.WorkerID, body.Result, body.Message)
	if errors.Is(err, tasks.ErrLeaseOwner) {
		http.Error(w, "lease lost", 409)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, t)
}

type taskFailRequest struct {
	WorkerID string `json:"workerId"`
	Error    string `json:"error"`
	Message  string `json:"message"`
}

func (h *Handler) FailTask(w http.ResponseWriter, r *http.Request) {
	if !h.internalAuthorized(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var body taskFailRequest
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	t, err := h.jobs.FailTask(r.Context(), r.PathValue("taskID"), body.WorkerID, body.Error, body.Message)
	if errors.Is(err, tasks.ErrLeaseOwner) {
		http.Error(w, "lease lost", 409)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, t)
}

type taskCancelRequest struct {
	WorkerID string `json:"workerId"`
	Message  string `json:"message"`
}

func (h *Handler) CancelTask(w http.ResponseWriter, r *http.Request) {
	if !h.internalAuthorized(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	var body taskCancelRequest
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	t, err := h.jobs.CancelTask(r.Context(), r.PathValue("taskID"), body.WorkerID, body.Message)
	if errors.Is(err, tasks.ErrLeaseOwner) {
		http.Error(w, "lease lost", 409)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (h *Handler) internalAuthorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.cfg.InternalToken)) == 1
}
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func badRequest(w http.ResponseWriter, err error) { http.Error(w, err.Error(), 400) }
func serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	http.Error(w, "internal server error", 500)
}
func serveAsset(w http.ResponseWriter, r *http.Request, path, downloadName string) {
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "asset not found", 404)
		return
	}
	if strings.HasSuffix(strings.ToLower(downloadName), ".ply") {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeFile(w, r, path)
}
