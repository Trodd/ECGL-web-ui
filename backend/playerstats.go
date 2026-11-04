package main

// getPlayerLeagueStats returns (creating if needed) a player's stats for a
// league. A new row is seeded from the legacy Player columns for the main
// league, or from the default rating for other leagues.
func getPlayerLeagueStats(playerID int64, league string) PlayerLeagueStats {
	league = normalizeLeague(league)

	var s PlayerLeagueStats
	if err := DB.Where("player_id = ? AND league = ?", playerID, league).First(&s).Error; err == nil {
		return s
	}

	var p Player
	DB.First(&p, playerID)

	s = PlayerLeagueStats{PlayerID: playerID, League: league}
	if league == LeagueMain {
		s.Rating, s.Wins, s.Losses, s.Matches = p.Rating, p.Wins, p.Losses, p.Matches
	} else {
		s.Rating = getEnvInt("DEFAULT_PLAYER_RATING", 800)
	}
	if s.Rating == 0 {
		s.Rating = getEnvInt("DEFAULT_PLAYER_RATING", 800)
	}
	DB.Create(&s)
	return s
}

// savePlayerLeagueStats persists a per-league player stats row.
func savePlayerLeagueStats(s PlayerLeagueStats) {
	DB.Save(&s)
}

// readPlayerLeagueStats returns a player's stored stats for a league without
// creating a row. ok is false when no record exists yet.
func readPlayerLeagueStats(playerID int64, league string) (PlayerLeagueStats, bool) {
	var s PlayerLeagueStats
	err := DB.Where("player_id = ? AND league = ?", playerID, normalizeLeague(league)).First(&s).Error
	return s, err == nil
}

// playerStatsForLeague returns a player's stats for a league for display
// purposes, without creating a row. Main falls back to the legacy player
// columns; other leagues fall back to a clean default record so Varsity values
// never leak across.
func playerStatsForLeague(p Player, league string) PlayerLeagueStats {
	if s, ok := readPlayerLeagueStats(p.ID, league); ok {
		return s
	}
	if normalizeLeague(league) == LeagueMain {
		return PlayerLeagueStats{
			PlayerID: p.ID,
			League:   LeagueMain,
			Rating:   p.Rating,
			Wins:     p.Wins,
			Losses:   p.Losses,
			Matches:  p.Matches,
		}
	}
	return PlayerLeagueStats{
		PlayerID: p.ID,
		League:   normalizeLeague(league),
		Rating:   getEnvInt("DEFAULT_PLAYER_RATING", 800),
	}
}

// mirrorPlayerStats keeps the legacy Player columns in sync with the main
// league stats so older code paths that still read Player.Rating etc. stay
// correct. Only used for the main league.
func mirrorPlayerStats(playerID int64, s PlayerLeagueStats) {
	DB.Model(&Player{}).Where("id = ?", playerID).Updates(map[string]any{
		"rating":  s.Rating,
		"wins":    s.Wins,
		"losses":  s.Losses,
		"matches": s.Matches,
	})
}
