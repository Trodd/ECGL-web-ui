-- One-time data fix: legacy match codes like "Week5-M006" should be treated as Preseason.
-- This script:
--  1) previews affected rows
--  2) sets matches.season = 'Preseason' for match_code starting with 'Week'
--  3) sets matches.week from the WeekN portion of match_code
--
-- Run in psql:
--   \i backend/scripts/fix_week_matches_to_preseason.sql

BEGIN;

-- Preview (before)
SELECT id, season, week, match_code, is_finals
FROM matches
WHERE match_code ~* '^Week[0-9]+-M'
ORDER BY id;

-- Update season
UPDATE matches
SET season = 'Preseason'
WHERE match_code ~* '^Week[0-9]+-M'
  AND (season IS DISTINCT FROM 'Preseason');

-- Update week from match_code (Week<digits>-M...)
UPDATE matches
SET week = regexp_replace(match_code, '^Week([0-9]+)-M.*$', '\\1')
WHERE match_code ~* '^Week[0-9]+-M'
  AND (week IS DISTINCT FROM regexp_replace(match_code, '^Week([0-9]+)-M.*$', '\\1'));

-- Preview (after)
SELECT id, season, week, match_code, is_finals
FROM matches
WHERE match_code ~* '^Week[0-9]+-M'
ORDER BY id;

COMMIT;
