package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type ModAuditEntry struct {
	Time          time.Time `json:"time"`
	Action        string    `json:"action"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	RequestURI    string    `json:"request_uri"`
	Target        string    `json:"target,omitempty"`
	ActorID       string    `json:"actor_id"`
	ActorUsername string    `json:"actor_username,omitempty"`
}

const modAuditMaxEntries = 500

var (
	modAuditMu      sync.Mutex
	modAuditEntries []ModAuditEntry
)

func RecordModAudit(entry ModAuditEntry) {
	modAuditMu.Lock()
	defer modAuditMu.Unlock()

	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}

	modAuditEntries = append(modAuditEntries, entry)
	if len(modAuditEntries) > modAuditMaxEntries {
		modAuditEntries = modAuditEntries[len(modAuditEntries)-modAuditMaxEntries:]
	}
}

func snapshotModAuditEntriesNewestFirst(limit int) []ModAuditEntry {
	modAuditMu.Lock()
	defer modAuditMu.Unlock()

	if limit <= 0 {
		limit = 200
	}
	if limit > modAuditMaxEntries {
		limit = modAuditMaxEntries
	}

	n := len(modAuditEntries)
	if n == 0 {
		return []ModAuditEntry{}
	}

	// Copy newest-first
	start := n - limit
	if start < 0 {
		start = 0
	}
	out := make([]ModAuditEntry, 0, n-start)
	for i := n - 1; i >= start; i-- {
		out = append(out, modAuditEntries[i])
	}
	return out
}

// GET /api/mod/audit-logs?limit=200
func HandleModAuditLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireLeagueMod(w, r); !ok {
		return
	}

	limit := 200
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			limit = v
		}
	}

	entries := snapshotModAuditEntriesNewestFirst(limit)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"count":   len(entries),
		"limit":   limit,
	})
}
