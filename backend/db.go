package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// --- Custom logger to silence ONLY ErrRecordNotFound ---
type SilentRecordNotFoundLogger struct {
	logger.Interface
}

func (l SilentRecordNotFoundLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	// Skip noisy “record not found” logs
	if len(data) > 0 {
		if err, ok := data[0].(error); ok && err == gorm.ErrRecordNotFound {
			return
		}
	}
	l.Interface.Error(ctx, msg, data...)
}

func InitDB() {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		getEnv("DB_HOST", "127.0.0.1"),
		getEnv("DB_USER", "ecgl"),
		getEnv("DB_PASS", "supersecret"),
		getEnv("DB_NAME", "ecgl"),
		getEnv("DB_PORT", "5432"),
	)

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), // 🔇 disables ALL SQL logging
	})
	if err != nil {
		log.Fatal("❌ Failed to connect to Postgres:", err)
	}

	// Auto-migrate schema
	err = DB.AutoMigrate(
		&Player{}, &Team{}, &TeamLogo{}, &TeamMember{}, &TeamJoinRequest{},
		&Match{}, &MatchScore{}, &PlayerHistory{}, &CastLog{}, &CastLogMulti{}, &MatchRoster{}, &FinalsTeam{}, &TeamArchive{}, &FinalsArchive{},
		&ModAuditLog{}, &LeagueSettings{}, &RuleSection{}, &Clip{}, &Notification{}, &PlayerAvailability{}, &PlayerLeagueStats{},
		&DraftSignup{}, &CastAssignment{}, &PlayerSettings{}, &PlayerLeagueRole{},
	)
	if err != nil {
		log.Fatal("❌ Migration failed:", err)
	}

	ensureSettingsSchema()
	ensureLeagueSchema()

	sqlDB, _ := DB.DB()
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Register data-version callbacks so the frontend can detect stale state.
	InitDataVersion()

	log.Println("✅ Connected to Postgres & migrated schema")
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// ensureSettingsSchema makes sure the singleton `settings` table used by the
// roster-lock controls exists and has every column the handlers rely on.
// AutoMigrate does not cover it (there is no GORM model), and the table was
// previously only created by running data.sql by hand.
func ensureSettingsSchema() {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS settings (
			id INTEGER PRIMARY KEY,
			roster_locked BOOLEAN DEFAULT FALSE
		)`,
		`ALTER TABLE settings ADD COLUMN IF NOT EXISTS roster_lock_override BOOLEAN DEFAULT FALSE`,
		`INSERT INTO settings (id, roster_locked) VALUES (1, FALSE) ON CONFLICT (id) DO NOTHING`,
	}
	for _, s := range stmts {
		if err := DB.Exec(s).Error; err != nil {
			log.Printf("⚠️ ensureSettingsSchema: %v", err)
		}
	}
}
