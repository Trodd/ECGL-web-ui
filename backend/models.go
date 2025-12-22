package main

import (
	"time"

	"gorm.io/datatypes"
)

// --- Player ---
type Player struct {
	ID             int64      `json:"-" gorm:"primaryKey"` // internal only
	IDStr          string     `json:"id" gorm:"-"`         // exposed as string in JSON
	Username       string     `json:"username"`
	DisplayName    string     `json:"display_name"`
	Role           string     `json:"role"`
	Timezone       string     `json:"timezone"`
	Device         string     `json:"device"`
	Rating         int        `json:"rating"`
	Wins           int        `json:"wins"`
	Losses         int        `json:"losses"`
	Matches        int        `json:"matches"`
	Registered     bool       `json:"registered" gorm:"-"`
	IsCaster       bool       `json:"is_caster" gorm:"-"`
	IsMod          bool       `json:"is_mod" gorm:"-"`
	LastLeftTeamAt *time.Time `json:"last_left_team_at"`
	OnCooldown     bool       `gorm:"-" json:"on_cooldown"`
	Division       string     `json:"division" gorm:"-"`
	Tier           string     `json:"tier" gorm:"-"`
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
	ID                   uint   `gorm:"primaryKey" json:"id"`
	Name                 string `gorm:"unique" json:"name"`
	Status               string `json:"status"`
	JoinAllowed          bool   `json:"join_allowed" gorm:"default:true"`
	Rating               int    `json:"rating"`
	Wins                 int    `json:"wins"`
	Losses               int    `json:"losses"`
	Matches              int    `json:"matches"`
	WeeklyChallengesUsed int    `json:"weekly_challenges_used" gorm:"default:0"`
	AllowChallenges      bool   `json:"allow_challenges" gorm:"default:true"`
	Locked               bool   `gorm:"default:false"`
	FinalsPlacement      int    `json:"finals_placement"`
	Division             string `json:"division"`
	Tier                 string `json:"tier"`
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
	ID                     uint           `gorm:"primaryKey" json:"id"`
	MatchCode              string         `gorm:"unique" json:"match_code"`
	TeamAID                uint           `json:"team_a_id"`
	TeamBID                uint           `json:"team_b_id"`
	ProposedDate           *time.Time     `json:"proposed_date"`
	ScheduledDate          *time.Time     `json:"scheduled_date"`
	Status                 string         `json:"status"`
	WinnerID               *uint          `json:"winner_id"`
	LoserID                *uint          `json:"loser_id"`
	ProposerID             *int64         `json:"proposer_id"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
	Season                 string         `json:"season"`
	Week                   string         `json:"week"`
	TeamAScheduleConfirmed bool           `json:"team_a_schedule_confirmed"`
	TeamBScheduleConfirmed bool           `json:"team_b_schedule_confirmed"`
	TeamAScoreConfirmed    bool           `json:"team_a_score_confirmed"`
	TeamBScoreConfirmed    bool           `json:"team_b_score_confirmed"`
	ScheduleConfirmedAt    *time.Time     `json:"schedule_confirmed_at"`
	ScoreConfirmedAt       *time.Time     `json:"score_confirmed_at"`
	TeamAScore             int            `json:"team_a_score" gorm:"default:0"`
	TeamBScore             int            `json:"team_b_score" gorm:"default:0"`
	ScoreHash              string         `gorm:"default:''"`
	MapScores              datatypes.JSON `json:"map_scores" gorm:"type:jsonb;default:'[]'"`
	LeagueSubA             *int64         `json:"league_sub_a"`
	LeagueSubB             *int64         `json:"league_sub_b"`
	CoinFlip               string         `json:"coin_flip" gorm:"default:''"`
	IsFinals               bool           `json:"is_finals" gorm:"default:false"`
	Bracket                string         `json:"bracket" gorm:"default:''"`      // "winners", "losers", "grand_final"
	BracketRound           int            `json:"bracket_round" gorm:"default:0"` // 1,2,...
	BracketSlot            int            `json:"bracket_slot" gorm:"default:0"`  // match index within the round
	Archived               bool           `json:"archived" gorm:"default:false"`
	DiscordChannelID       *string        `json:"discord_channel_id"`
	ChannelCreatedAt       *time.Time     `json:"channel_created_at"`
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

	ArchiveRating  int       `json:"archive_rating"`
	ArchiveWins    int       `json:"archive_wins"`
	ArchiveLosses  int       `json:"archive_losses"`
	ArchiveMatches int       `json:"archive_matches"`
	ArchiveTeam    string    `json:"archive_team"`
	IsTeamJoin     bool      `json:"is_team_join" gorm:"default:false"`
	CreatedAt      time.Time `json:"created_at"`
}

func (PlayerHistory) TableName() string {
	return "player_history"
}

type MyTeamResponse struct {
	Team     *Team       `json:"team"`
	Roster   interface{} `json:"roster"`
	Matches  interface{} `json:"matches"`
	Requests interface{} `json:"requests"`
	MyRole   string      `json:"myRole"`
}

type LeagueSettings struct {
	ID                   uint       `gorm:"primaryKey"`
	CurrentWeek          int        `json:"current_week"`
	WeeklyChallengeLimit int        `json:"weekly_challenge_limit"`
	ChallengesEnabled    bool       `json:"challenges_enabled" gorm:"default:true"`
	FinalsVisible        bool       `json:"finals_visible" gorm:"column:show_finals_tab;default:false"`
	LastMatchGeneration  *time.Time `json:"last_match_generation"`
}

type ChallengeRequest struct {
	ID              uint `gorm:"primaryKey"`
	RequesterTeamID uint
	TargetTeamID    uint
	Week            int
	Status          string

	CreatedAt time.Time
	UpdatedAt time.Time
}

type CastLog struct {
	ID        uint `gorm:"primaryKey"`
	MatchID   uint
	TeamAID   uint
	TeamBID   uint
	CasterID  int64
	CameraID  int64
	CreatedAt time.Time
}

type CastLogMulti struct {
	ID        uint           `gorm:"primaryKey"`
	MatchID   uint           `json:"match_id"`
	Casters   datatypes.JSON `json:"casters" gorm:"type:jsonb"`
	CameraID  int64          `json:"camera_id"`
	CreatedAt time.Time      `json:"created_at"`
	StreamURL string         `json:"stream_url"`
}

type MatchRoster struct {
	ID          uint   `gorm:"primaryKey"`
	MatchID     uint   `gorm:"index"`
	TeamID      uint   `gorm:"index"`
	PlayerID    int64  `gorm:"index"`
	DisplayName string `gorm:"size:100"`
	Username    string `gorm:"size:100"`
	Role        string `gorm:"size:50"`
	CreatedAt   time.Time
}

// --- Finals models ---

type FinalsTeam struct {
	ID     uint   `json:"id" gorm:"primaryKey"`
	Season string `json:"season"`
	TeamID uint   `json:"team_id"`
	Seed   int    `json:"seed"` // 1 = top seed
}

type TeamArchive struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TeamID    uint      `json:"team_id"`
	Season    string    `json:"season"`
	Name      string    `json:"name"`
	Rating    int       `json:"rating"`
	Wins      int       `json:"wins"`
	Losses    int       `json:"losses"`
	Matches   int       `json:"matches"`
	CreatedAt time.Time `json:"created_at"`
}

type FinalsArchive struct {
	ID        uint   `gorm:"primaryKey"`
	Season    string `gorm:"uniqueIndex"`
	Snapshot  datatypes.JSON
	CreatedAt time.Time
}
