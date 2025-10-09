package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

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
		Logger: logger.Default.LogMode(logger.Error), // ✅ only show errors, not "record not found"
	})
	if err != nil {
		log.Fatal("❌ Failed to connect to Postgres:", err)
	}

	// Auto-migrate schema
	err = DB.AutoMigrate(&Player{}, &Team{}, &TeamMember{}, &TeamJoinRequest{}, &Match{}, &MatchScore{}, &PlayerHistory{})
	if err != nil {
		log.Fatal("❌ Migration failed:", err)
	}

	// Configure connection pool
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
