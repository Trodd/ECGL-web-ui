package main

import (
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

// dataVersion is a global counter that increments whenever meaningful data is
// written to the database. Clients poll GET /api/version and force a full page
// refresh when it changes, which prevents users from interacting with a stale
// frontend after the backend state has changed (e.g. a mod reset a schedule).
var dataVersion atomic.Int64

// InitDataVersion seeds the version and registers GORM write callbacks that
// bump it automatically after every successful Create/Update/Delete.
// Must be called after InitDB().
func InitDataVersion() {
	// Seed from the current unix time so a server restart (redeploy) also
	// changes the version, forcing open tabs to pick up the new bundle.
	dataVersion.Store(time.Now().Unix())

	bump := func(db *gorm.DB) {
		if db == nil || db.Error != nil {
			return
		}
		// Skip per-user / internal churn that should NOT force everyone else
		// to reload (notifications are personal; mod audit logs are internal).
		switch db.Statement.Table {
		case "notifications", "mod_audit_logs":
			return
		}
		bumpDataVersion()
	}

	DB.Callback().Create().After("gorm:create").Register("ecgl:bump-data-version", bump)
	DB.Callback().Update().After("gorm:update").Register("ecgl:bump-data-version", bump)
	DB.Callback().Delete().After("gorm:delete").Register("ecgl:bump-data-version", bump)

	log.Println("✅ Data-version callbacks registered")
}

// bumpDataVersion increments the global data version. It is called by the
// GORM write callbacks and also explicitly by handlers that write via raw SQL
// (DB.Exec), which bypasses GORM callbacks.
func bumpDataVersion() {
	dataVersion.Add(1)
}

// CurrentDataVersion returns the current data version.
func CurrentDataVersion() int64 {
	return dataVersion.Load()
}

// GET /api/version — public, returns the current data version.
func HandleGetVersion(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, map[string]any{
		"version": CurrentDataVersion(),
	})
}
