package main

import "time"

// --- Player ---
type Player struct {
	ID          int64  `json:"-" gorm:"primaryKey"` // internal only
	IDStr       string `json:"id" gorm:"-"`         // exposed as string in JSON
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Timezone    string `json:"timezone"`
	Device      string `json:"device"`
	Rating      int    `json:"rating"`
	Wins        int    `json:"wins"`
	Losses      int    `json:"losses"`
	Matches     int    `json:"matches"`
	Registered  bool   `json:"registered" gorm:"-"`
}

// --- Registered Player ---
type RegisteredPlayer struct {
	IDStr    string `json:"id" gorm:"-"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Device   string `json:"device"`
	Timezone string `json:"timezone"`
}

type TeamJoinRequest struct {
	ID       uint   `json:"id" gorm:"primaryKey"`
	PlayerID int64  `json:"player_id"`
	TeamID   uint   `json:"team_id"`
	Status   string `json:"status"` // pending / accepted / denied
	Player   Player `json:"player" gorm:"foreignKey:PlayerID"`
}

// --- Team ---
type Team struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Name    string `gorm:"unique" json:"name"`
	Status  string `json:"status"`
	Rating  int    `json:"rating"`
	Wins    int    `json:"wins"`
	Losses  int    `json:"losses"`
	Matches int    `json:"matches"`
}

// --- Team Member ---
type TeamMember struct {
	PlayerID int64 `gorm:"primaryKey;not null"`
	TeamID   uint  `gorm:"primaryKey;not null"`
	Role     string
	Player   Player `gorm:"foreignKey:PlayerID"`
}

// --- Match ---
type Match struct {
	ID                     uint       `gorm:"primaryKey" json:"id"`
	MatchCode              string     `gorm:"unique" json:"match_code"`
	TeamAID                uint       `json:"team_a_id"`
	TeamBID                uint       `json:"team_b_id"`
	ProposedDate           *time.Time `json:"proposed_date"`
	ScheduledDate          *time.Time `json:"scheduled_date"`
	Status                 string     `json:"status"`
	WinnerID               *uint      `json:"winner_id"`
	LoserID                *uint      `json:"loser_id"`
	ProposerID             *int64     `json:"proposer_id"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	Season                 string     `json:"season"`
	Week                   string     `json:"week"`
	TeamAScheduleConfirmed bool       `json:"team_a_schedule_confirmed"`
	TeamBScheduleConfirmed bool       `json:"team_b_schedule_confirmed"`
	TeamAScoreConfirmed    bool       `json:"team_a_score_confirmed"`
	TeamBScoreConfirmed    bool       `json:"team_b_score_confirmed"`
	ScheduleConfirmedAt    *time.Time `json:"schedule_confirmed_at"`
	ScoreConfirmedAt       *time.Time `json:"score_confirmed_at"`
}

// --- Match Score ---
type MatchScore struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	MatchID    uint   `json:"match_id"`
	MapNumber  int    `gorm:"column:map_number" json:"map_number"`
	Gamemode   string `json:"gamemode"`
	TeamAScore int    `gorm:"column:team_a_score" json:"team_a_score"`
	TeamBScore int    `gorm:"column:team_b_score" json:"team_b_score"`
}

// --- Player History ---
type PlayerHistory struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	PlayerID int64  `json:"player_id"`
	TeamID   uint   `json:"team_id"`
	TeamName string `json:"team_name"`
	Role     string `json:"role"`
	Season   string `json:"season"`
}

type MyTeamResponse struct {
	Team     *Team       `json:"team"`
	Roster   interface{} `json:"roster"`
	Matches  interface{} `json:"matches"`
	Requests interface{} `json:"requests"`
	MyRole   string      `json:"myRole"`
}
