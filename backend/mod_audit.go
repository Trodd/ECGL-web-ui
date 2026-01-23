package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// ModAuditEntry is used for JSON responses (matches ModAuditLog structure)
type ModAuditEntry struct {
	ID            uint      `json:"id,omitempty"`
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

// RecordModAudit persists a mod action to the database
func RecordModAudit(entry ModAuditEntry) {
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}

	// Save to database
	dbEntry := ModAuditLog{
		Time:          entry.Time,
		Action:        entry.Action,
		Method:        entry.Method,
		Path:          entry.Path,
		RequestURI:    entry.RequestURI,
		Target:        entry.Target,
		ActorID:       entry.ActorID,
		ActorUsername: entry.ActorUsername,
	}

	if err := DB.Create(&dbEntry).Error; err != nil {
		log.Printf("⚠️ Failed to save mod audit log: %v", err)
	}

	// Prune old entries if we have too many (keep last 500)
	var count int64
	DB.Model(&ModAuditLog{}).Count(&count)
	if count > modAuditMaxEntries {
		// Delete oldest entries beyond the limit
		DB.Exec(`DELETE FROM mod_audit_logs WHERE id NOT IN (
			SELECT id FROM mod_audit_logs ORDER BY time DESC LIMIT ?
		)`, modAuditMaxEntries)
	}
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
	if limit > modAuditMaxEntries {
		limit = modAuditMaxEntries
	}

	// Fetch from database, newest first
	var logs []ModAuditLog
	DB.Order("time DESC").Limit(limit).Find(&logs)

	// Convert to response format
	entries := make([]ModAuditEntry, len(logs))
	for i, l := range logs {
		entries[i] = ModAuditEntry{
			ID:            l.ID,
			Time:          l.Time,
			Action:        l.Action,
			Method:        l.Method,
			Path:          l.Path,
			RequestURI:    l.RequestURI,
			Target:        l.Target,
			ActorID:       l.ActorID,
			ActorUsername: l.ActorUsername,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"count":   len(entries),
		"limit":   limit,
	})
}
