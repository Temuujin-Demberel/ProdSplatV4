package system

import (
	"sync"
	"time"
)

type WorkerInfo struct {
	WorkerID      string            `json:"workerId"`
	LastSeen      time.Time         `json:"lastSeen"`
	Online        bool              `json:"online"`
	Healthy       bool              `json:"healthy"`
	Error         string            `json:"error,omitempty"`
	GPU           map[string]string `json:"gpu,omitempty"`
	Versions      map[string]string `json:"versions,omitempty"`
	CurrentTaskID string            `json:"currentTaskId,omitempty"`
}

type WorkerMonitor struct {
	mu   sync.RWMutex
	info WorkerInfo
}

func NewWorkerMonitor() *WorkerMonitor { return &WorkerMonitor{} }

func (m *WorkerMonitor) Update(info WorkerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info.LastSeen = time.Now().UTC()
	info.Online = true
	m.info = info
}

func (m *WorkerMonitor) Snapshot() WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info := m.info
	info.Online = !info.LastSeen.IsZero() && time.Since(info.LastSeen) < 20*time.Second
	return info
}
