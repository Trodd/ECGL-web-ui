-- ==========================================================
-- PLAYERS
-- ==========================================================
CREATE TABLE players (
    id BIGINT PRIMARY KEY,                      -- Discord ID
    username TEXT NOT NULL,
    display_name TEXT DEFAULT '',
    role TEXT CHECK (role IN ('Player','League Sub')) NOT NULL,
    timezone TEXT NOT NULL,
    device TEXT DEFAULT '',
    rating INT DEFAULT 800,
    wins INT DEFAULT 0,
    losses INT DEFAULT 0,
    matches INT DEFAULT 0,
    last_left_team_at TIMESTAMPTZ                -- nullable
);

-- ==========================================================
-- TEAMS
-- ==========================================================
CREATE TABLE teams (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    status TEXT DEFAULT 'Active',
    join_allowed BOOLEAN DEFAULT TRUE,
    rating INT DEFAULT 800,
    wins INT DEFAULT 0,
    losses INT DEFAULT 0,
    matches INT DEFAULT 0,
    weekly_challenges_used INT DEFAULT 0,
    allow_challenges BOOLEAN DEFAULT TRUE,
    locked BOOLEAN DEFAULT FALSE,
    finals_placement INT DEFAULT 0
);

-- ==========================================================
-- TEAM MEMBERS
-- ==========================================================
CREATE TABLE team_members (
    player_id BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    PRIMARY KEY (player_id, team_id)
);

-- ==========================================================
-- MATCHES
-- ==========================================================
CREATE TABLE matches (
    id SERIAL PRIMARY KEY,
    match_code TEXT UNIQUE NOT NULL,
    team_a_id INT REFERENCES teams(id),
    team_b_id INT REFERENCES teams(id),

    proposed_date TIMESTAMPTZ,
    scheduled_date TIMESTAMPTZ,

    status TEXT NOT NULL DEFAULT 'Proposed',

    winner_id INT REFERENCES teams(id),
    loser_id INT REFERENCES teams(id),

    proposer_id BIGINT REFERENCES players(id),

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),

    season TEXT DEFAULT 'Preseason',
    week TEXT DEFAULT '',

    team_a_schedule_confirmed BOOLEAN DEFAULT FALSE,
    team_b_schedule_confirmed BOOLEAN DEFAULT FALSE,
    team_a_score_confirmed BOOLEAN DEFAULT FALSE,
    team_b_score_confirmed BOOLEAN DEFAULT FALSE,

    schedule_confirmed_at TIMESTAMPTZ,
    score_confirmed_at TIMESTAMPTZ,

    team_a_score INT DEFAULT 0,
    team_b_score INT DEFAULT 0,
    score_hash TEXT DEFAULT '',

    map_scores JSONB DEFAULT '[]',

    league_sub_a BIGINT,
    league_sub_b BIGINT,

    coin_flip TEXT DEFAULT '',
    is_finals BOOLEAN DEFAULT FALSE,

    bracket TEXT DEFAULT '',
    bracket_round INT DEFAULT 0,
    bracket_slot INT DEFAULT 0
);

-- ==========================================================
-- MATCH SCORES (Legacy per-map storage)
-- ==========================================================
CREATE TABLE match_scores (
    id SERIAL PRIMARY KEY,
    match_id INT REFERENCES matches(id) ON DELETE CASCADE,
    map_number INT CHECK (map_number BETWEEN 1 AND 3),
    gamemode TEXT,
    team_a_score INT,
    team_b_score INT
);

-- ==========================================================
-- PLAYER HISTORY (FULL SNAPSHOT + TEAM)
-- ==========================================================
CREATE TABLE player_history (
    id SERIAL PRIMARY KEY,
    player_id BIGINT REFERENCES players(id),
    team_id INT REFERENCES teams(id),
    team_name TEXT,
    role TEXT DEFAULT 'Member',
    season TEXT NOT NULL,

    archive_rating INT DEFAULT 0,
    archive_wins INT DEFAULT 0,
    archive_losses INT DEFAULT 0,
    archive_matches INT DEFAULT 0,
    archive_team TEXT DEFAULT '',

    UNIQUE(player_id, season)
);

-- ==========================================================
-- TEAM RENAME HISTORY
-- ==========================================================
CREATE TABLE team_history (
    id SERIAL PRIMARY KEY,
    team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    old_name TEXT NOT NULL,
    new_name TEXT NOT NULL,
    changed_by BIGINT NOT NULL,
    changed_at TIMESTAMPTZ DEFAULT NOW()
);

-- ==========================================================
-- SETTINGS
-- ==========================================================
CREATE TABLE settings (
    id SERIAL PRIMARY KEY,
    roster_locked BOOLEAN DEFAULT FALSE
);

INSERT INTO settings (id, roster_locked)
VALUES (1, FALSE)
ON CONFLICT (id) DO NOTHING;

-- ==========================================================
-- CHALLENGE REQUESTS
-- ==========================================================
CREATE TABLE challenge_requests (
    id SERIAL PRIMARY KEY,
    requester_team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    target_team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    week INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'Pending',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- ==========================================================
-- LEAGUE SETTINGS
-- ==========================================================
CREATE TABLE league_settings (
    id SERIAL PRIMARY KEY,
    current_week INT NOT NULL DEFAULT 1,
    weekly_challenge_limit INT NOT NULL DEFAULT 1,
    challenges_enabled BOOLEAN DEFAULT TRUE,
    show_finals_tab BOOLEAN DEFAULT FALSE,
    last_match_generation TIMESTAMPTZ
);

INSERT INTO league_settings (id, current_week, weekly_challenge_limit)
VALUES (1, 1, 1)
ON CONFLICT (id) DO NOTHING;

-- ==========================================================
-- CAST LOGS
-- ==========================================================
CREATE TABLE cast_logs (
    id SERIAL PRIMARY KEY,
    match_id INT REFERENCES matches(id),
    team_a_id INT,
    team_b_id INT,
    caster_id BIGINT,
    camera_id BIGINT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE cast_log_multis (
    id SERIAL PRIMARY KEY,
    match_id INT,
    casters JSONB DEFAULT '[]',
    camera_id BIGINT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- ==========================================================
-- MATCH ROSTER
-- ==========================================================
CREATE TABLE match_rosters (
    id SERIAL PRIMARY KEY,
    match_id INT REFERENCES matches(id),
    team_id INT REFERENCES teams(id),
    player_id BIGINT REFERENCES players(id),
    display_name TEXT,
    username TEXT,
    role TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- ==========================================================
-- FINALS TEAMS
-- ==========================================================
CREATE TABLE finals_teams (
    id SERIAL PRIMARY KEY,
    season TEXT NOT NULL,
    team_id INT NOT NULL REFERENCES teams(id),
    seed INT NOT NULL
);
