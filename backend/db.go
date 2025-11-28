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
		&Player{}, &Team{}, &TeamMember{}, &TeamJoinRequest{},
		&Match{}, &MatchScore{}, &PlayerHistory{}, &CastLog{}, &CastLogMulti{}, &MatchRoster{}, &FinalsTeam{},
	)
	if err != nil {
		log.Fatal("❌ Migration failed:", err)
	}

	sqlDB, _ := DB.DB()
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Println("✅ Connected to Postgres & migrated schema")
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
