-- === Players ===
CREATE TABLE players (
    id BIGINT PRIMARY KEY,            -- Discord ID
    username TEXT NOT NULL,
    role TEXT CHECK (role IN ('Player','League Sub')) NOT NULL,
    timezone TEXT NOT NULL,
    rating INT DEFAULT 800,           -- from config
    wins INT DEFAULT 0,
    losses INT DEFAULT 0,
    matches INT DEFAULT 0
);

-- === Teams ===
CREATE TABLE teams (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    status TEXT DEFAULT 'Active',
    rating INT DEFAULT 800,
    wins INT DEFAULT 0,
    losses INT DEFAULT 0,
    matches INT DEFAULT 0
);

-- === Team Members ===
CREATE TABLE team_members (
    team_id INT REFERENCES teams(id) ON DELETE CASCADE,
    player_id BIGINT REFERENCES players(id) ON DELETE CASCADE,
    role TEXT CHECK (role IN ('Captain','Co-Captain','Member')) NOT NULL,
    PRIMARY KEY (team_id, player_id)
);

-- === Matches (Scheduled & Challenge) ===
CREATE TABLE matches (
    id SERIAL PRIMARY KEY,
    match_code TEXT UNIQUE NOT NULL,     -- e.g. Week3-NEX-IMM or Challenge3-M001
    team_a_id INT REFERENCES teams(id),
    team_b_id INT REFERENCES teams(id),
    proposed_date TIMESTAMP WITH TIME ZONE,
    scheduled_date TIMESTAMP WITH TIME ZONE,
    status TEXT CHECK (status IN ('Proposed','Scheduled','Finished','Forfeited','Cancelled')) NOT NULL DEFAULT 'Proposed',
    winner_id INT REFERENCES teams(id),
    loser_id INT REFERENCES teams(id),
    proposer_id BIGINT REFERENCES players(id),
    season TEXT DEFAULT 'Preseason'
);

-- === Match Scores (per map) ===
CREATE TABLE match_scores (
    id SERIAL PRIMARY KEY,
    match_id INT REFERENCES matches(id) ON DELETE CASCADE,
    map_number INT CHECK (map_number BETWEEN 1 AND 3),
    gamemode TEXT CHECK (gamemode IN ('Payload','Capture Point')),
    team_a_score INT,
    team_b_score INT
);

-- === Team Join Requests ===
CREATE TABLE team_join_requests (
    id SERIAL PRIMARY KEY,
    player_id BIGINT REFERENCES players(id) ON DELETE CASCADE,
    team_id INT REFERENCES teams(id) ON DELETE CASCADE,
    status TEXT CHECK (status IN ('pending','accepted','denied')) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE player_history (
    id SERIAL PRIMARY KEY,
    player_id BIGINT REFERENCES players(id),
    team_id INT REFERENCES teams(id),
    team_name TEXT,
    season TEXT NOT NULL,
    UNIQUE(player_id, season)
);

CREATE TABLE IF NOT EXISTS team_history (
    id SERIAL PRIMARY KEY,
    team_id INTEGER NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    old_name TEXT NOT NULL,
    new_name TEXT NOT NULL,
    changed_by BIGINT NOT NULL, -- Discord ID of captain/mod
    changed_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS settings (
    id SERIAL PRIMARY KEY,
    roster_locked BOOLEAN DEFAULT FALSE
);

-- Ensure one row exists
INSERT INTO settings (id, roster_locked)
VALUES (1, FALSE)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS challenge_requests (
    id SERIAL PRIMARY KEY,

    requester_team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    target_team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,

    week INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'Pending',

    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS league_settings (
    id SERIAL PRIMARY KEY,
    current_week INT NOT NULL DEFAULT 1,
    weekly_challenge_limit INT NOT NULL DEFAULT 1
);

-- Ensure row with ID = 1 exists
INSERT INTO league_settings (id, current_week, weekly_challenge_limit)
VALUES (1, 1, 1)
ON CONFLICT (id) DO NOTHING;