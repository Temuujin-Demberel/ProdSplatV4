package api

import "net/http"

func RegisterRoutes(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /ready", h.Ready)
	mux.HandleFunc("GET /api/system", h.System)
	mux.HandleFunc("GET /api/events", h.Events)

	mux.HandleFunc("POST /api/jobs", h.CreateJob)
	mux.HandleFunc("GET /api/jobs", h.ListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", h.GetJob)
	mux.HandleFunc("GET /api/jobs/{id}/tasks", h.ListTasks)
	mux.HandleFunc("POST /api/jobs/{id}/video", h.UploadVideo)
	mux.HandleFunc("POST /api/jobs/{id}/ply", h.UploadPLY)
	mux.HandleFunc("POST /api/jobs/{id}/attempts/{number}/activate", h.ActivateAttempt)
	mux.HandleFunc("GET /api/jobs/{id}/splat", h.GetSplat)
	mux.HandleFunc("POST /api/jobs/{id}/cleaned", h.SaveCleaned)
	mux.HandleFunc("GET /api/jobs/{id}/cleaned", h.GetCleaned)
	mux.HandleFunc("POST /api/jobs/{id}/render", h.StartRender)
	mux.HandleFunc("GET /api/jobs/{id}/renders/{name}", h.RenderAsset)
	mux.HandleFunc("POST /api/jobs/{id}/backgrounds", h.UploadBackgrounds)
	mux.HandleFunc("POST /api/jobs/{id}/dataset", h.StartDataset)
	mux.HandleFunc("GET /api/jobs/{id}/dataset.zip", h.DownloadDataset)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", h.CancelJob)
	mux.HandleFunc("GET /api/tasks/{taskID}/log", h.TaskLog)

	mux.HandleFunc("POST /internal/worker/heartbeat", h.WorkerHeartbeat)
	mux.HandleFunc("POST /internal/tasks/claim", h.ClaimTask)
	mux.HandleFunc("POST /internal/tasks/{taskID}/heartbeat", h.TaskHeartbeat)
	mux.HandleFunc("POST /internal/tasks/{taskID}/complete", h.CompleteTask)
	mux.HandleFunc("POST /internal/tasks/{taskID}/fail", h.FailTask)
	mux.HandleFunc("POST /internal/tasks/{taskID}/cancelled", h.CancelTask)
}
