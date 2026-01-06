import argparse
import difflib
import re
from datetime import UTC, datetime

import gspread
import psycopg2
from oauth2client.service_account import ServiceAccountCredentials
from pytz import timezone

# === Google Sheets (lazy init) ===
scope = [
    "https://spreadsheets.google.com/feeds",
    "https://www.googleapis.com/auth/spreadsheets",
    "https://www.googleapis.com/auth/drive",
]

SHEET_NAME = "CombatLeaguePre"
client = None
spreadsheet = None


def init_sheets():
    global client, spreadsheet
    if spreadsheet is not None:
        return
    creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
    client = gspread.authorize(creds)
    spreadsheet = client.open(SHEET_NAME)

# === Postgres ===
conn = psycopg2.connect(
    host="127.0.0.1",
    database="ecgl",
    user="ecgl",
    password="supersecret",
    port=5432
)
cur = conn.cursor()

def to_int(val, default=0):
    s = re.sub(r'[^0-9-]', '', str(val or ''))
    try:
        return int(s) if s else default
    except:
        return default

# === Build player map from Players sheet ===
def build_player_map():
    ws = spreadsheet.worksheet("Players")
    rows = ws.get_all_values(value_render_option="FORMATTED_VALUE")[1:]

    player_map = {}
    for row in rows:
        if not row or not row[0].strip():
            continue

        discord_id_str = row[0].strip()
        if not discord_id_str.isdigit() or not (17 <= len(discord_id_str) <= 19):
            continue

        discord_id = int(discord_id_str)
        username   = row[1].strip() if len(row) > 1 and row[1] else "Unknown"
        role       = row[2].strip() if len(row) > 2 and row[2] else "Player"
        tz         = row[3].strip() if len(row) > 3 and row[3] else "EST"

        clean_name = username.split("(")[0].split("[")[0].strip().lower()
        player_map[clean_name] = (discord_id, username, role, tz)

    return player_map

player_map = {}

# === Helpers ===

def normalize_name(name):
    """Clean and normalize a player or team name for comparison."""
    return re.sub(r'[^a-z0-9]', '', name.lower().strip())

def resolve_player(name, team_name=""):
    """Resolve username loosely using Players sheet."""
    clean = normalize_name(name)
    normalized_map = {normalize_name(k): v for k, v in player_map.items()}

    # ✅ Exact normalized match
    if clean in normalized_map:
        return normalized_map[clean]

    # ✅ Try fuzzy match (close string)
    best = difflib.get_close_matches(clean, normalized_map.keys(), n=1, cutoff=0.85)
    if best:
        print(f"🔍 Fuzzy matched '{name}' → '{best[0]}'")
        return normalized_map[best[0]]

    print(f"⚠️ No match in Players sheet for '{name}' (team {team_name})")
    return None

# === Migrate Teams ===
def migrate_teams():
    init_sheets()
    ws = spreadsheet.worksheet("Teams")
    rows = ws.get_all_values()[1:]

    for row in rows:
        if not row[0]:
            continue

        team_name = row[0].strip()
        status = row[7].strip() if len(row) > 7 and row[7] else "Active"

        cur.execute("""
            INSERT INTO teams (name, status)
            VALUES (%s, %s)
            ON CONFLICT (name) DO UPDATE SET status = EXCLUDED.status
        """, (team_name, status))

    conn.commit()
    print("✅ Migrated Teams")

# === Migrate Players (stats only) ===
def migrate_players_leaderboard():
    init_sheets()
    ws = spreadsheet.worksheet("Player Leaderboard")
    rows = ws.get_all_values()[1:]  # skip header row

    updated = 0
    skipped = 0

    for row in rows:
        if not row or len(row) < 2:
            continue

        discord_id_str = row[1].strip()  # User ID is in col 2
        if not discord_id_str.isdigit() or not (17 <= len(discord_id_str) <= 19):
            print(f"⚠️ Skipping invalid Discord ID in leaderboard: {discord_id_str}")
            skipped += 1
            continue

        discord_id = int(discord_id_str)
        rating   = int(row[2]) if len(row) > 2 and row[2] else 800
        wins     = int(row[3]) if len(row) > 3 and row[3] else 0
        losses   = int(row[4]) if len(row) > 4 and row[4] else 0
        matches  = int(row[5]) if len(row) > 5 and row[5] else wins + losses

        # Only update if that player ID exists (from Players sheet)
        cur.execute("""
            UPDATE players
               SET rating=%s, wins=%s, losses=%s, matches=%s
             WHERE id=%s
        """, (rating, wins, losses, matches, discord_id))

        if cur.rowcount > 0:
            updated += 1
        else:
            print(f"⚠️ No match in Players sheet for ID {discord_id}")
            skipped += 1

    conn.commit()
    print(f"✅ Updated Player Leaderboard: updated={updated}, skipped={skipped}")

# === Migrate Players sheet (authoritative for IDs) ===
def migrate_players_sheet():
    init_sheets()
    ws = spreadsheet.worksheet("Players")
    rows = ws.get_all_values(value_render_option="FORMATTED_VALUE")[1:]

    inserted, updated = 0, 0
    for row in rows:
        if not row or not row[0].strip():
            continue

        discord_id_str = row[0].strip()
        if not discord_id_str.isdigit() or not (17 <= len(discord_id_str) <= 19):
            continue

        discord_id = int(discord_id_str)
        username   = row[1].strip() if len(row) > 1 and row[1] else "Unknown"
        role       = row[2].strip() if len(row) > 2 and row[2] else "Player"
        tz         = row[3].strip() if len(row) > 3 and row[3] else "US/Eastern"

        # ✅ Keep full IANA string (e.g., "US/Eastern", "Europe/London", etc.)
        valid_prefixes = (
            "US/", "Canada/", "Europe/", "Australia/",
            "Asia/", "Africa/", "America/", "Pacific/"
        )
        if not tz.startswith(valid_prefixes):
            print(f"⚠️ Invalid timezone format '{tz}', defaulting to US/Eastern")
            tz = "US/Eastern"

        cur.execute("""
            INSERT INTO players (id, username, display_name, role, timezone)
            VALUES (%s, %s, %s, %s, %s)
            ON CONFLICT (id) DO UPDATE
            SET username=EXCLUDED.username,
                display_name=EXCLUDED.username,
                role=EXCLUDED.role,
                timezone=EXCLUDED.timezone
        """, (discord_id, username, username, role, tz))

        if cur.rowcount == 1:
            inserted += 1
        else:
            updated += 1

    conn.commit()
    print(f"✅ Migrated Players sheet: inserted={inserted}, updated={updated}")

# === Migrate Team Leaderboard ===
def migrate_team_leaderboard():
    init_sheets()
    try:
        ws = spreadsheet.worksheet("Leaderboard")
    except gspread.WorksheetNotFound:
        print("⚠️ Team leaderboard sheet not found. Skipping.")
        return

    rows = ws.get_all_values()[1:]

    for row in rows:
        if not row or not row[0].strip():
            continue

        name    = row[0].strip()
        rating  = to_int(row[1], 0) if len(row) > 1 else 0
        wins    = to_int(row[2], 0) if len(row) > 2 else 0
        losses  = to_int(row[3], 0) if len(row) > 3 else 0
        matches = to_int(row[4], wins + losses) if len(row) > 4 else (wins + losses)

        cur.execute("""
            UPDATE teams
               SET rating=%s, wins=%s, losses=%s, matches=%s
             WHERE name=%s
        """, (rating, wins, losses, matches, name))

    conn.commit()
    print("✅ Migrated Team Leaderboard")

# === Migrate Team Members ===
def extract_discord_id(text):
    """Extract the last 17–19 digit number inside parentheses."""
    matches = re.findall(r"\((\d{17,19})\)", str(text))
    if matches:
        return int(matches[-1])
    return None

def migrate_team_members():
    ws = spreadsheet.worksheet("Teams")
    rows = ws.get_all_values()[1:]

    for row in rows:
        if not row[0]:
            continue

        team_name = row[0].strip()
        cur.execute("SELECT id FROM teams WHERE name=%s", (team_name,))
        team = cur.fetchone()
        if not team:
            continue
        team_id = team[0]

        co_captain_ref = row[8].strip() if len(row) > 8 and row[8] else None

        for i in range(1, 7):  # up to 6 players
            if i >= len(row) or not row[i]:
                continue

            # 🧠 Extract the ID inside parentheses
            discord_id = extract_discord_id(row[i])
            if not discord_id:
                print(f"⚠️ Could not find Discord ID in '{row[i]}' for team {team_name}")
                continue

            # Check if player exists in DB
            cur.execute("SELECT username, role, timezone FROM players WHERE id=%s", (discord_id,))
            p = cur.fetchone()
            if not p:
                print(f"⚠️ Player ID {discord_id} not found in players table (team {team_name})")
                continue

            username, player_role, tz = p

            role = "Captain" if i == 1 else "Member"
            if co_captain_ref and str(discord_id) in co_captain_ref:
                role = "Co-Captain"

            cur.execute("""
                INSERT INTO team_members (team_id, player_id, role)
                VALUES (%s, %s, %s)
                ON CONFLICT (team_id, player_id) DO UPDATE
                   SET role=EXCLUDED.role
            """, (team_id, discord_id, role))

    conn.commit()
    print("✅ Migrated Team Members (matched strictly by Discord ID)")

from datetime import datetime, timezone

def parse_date(val):
    """Parse a scheduled/proposed date (supports human, UNIX, or Discord timestamp)."""
    if not val:
        return None
    try:
        # Handle Discord-style timestamp <t:1728172800:F>
        if "<t:" in val:
            unix = int(re.sub(r"[^\d]", "", val))
            return datetime.fromtimestamp(unix, tz=timezone.utc)

        # Try common date formats
        for fmt in ("%Y-%m-%d %H:%M", "%m/%d/%Y %I:%M %p", "%m/%d/%Y", "%Y-%m-%d"):
            try:
                return datetime.strptime(val, fmt).replace(tzinfo=timezone.utc)
            except ValueError:
                continue
        return None
    except Exception:
        return None


def migrate_matches(include_orphans=True):
    """Migrate all matches from the Google Sheet into DB (even if teams are missing)."""
    print("📦 Migrating matches (auto-creating placeholder teams for missing ones)...")

    init_sheets()

    try:
        ws = spreadsheet.worksheet("Matches")
    except gspread.WorksheetNotFound:
        print("⚠️ 'Matches' sheet not found. Skipping.")
        return

    rows = ws.get_all_values()
    if not rows or len(rows) < 2:
        print("⚠️ No data in Matches sheet.")
        return

    inserted, updated, skipped, created_placeholders = 0, 0, 0, 0
    rows = rows[1:]  # Skip header row

    # --- Helper: auto-create teams if missing ---
    def ensure_team(name):
        if not name or not name.strip():
            return None
        name = name.strip()
        cur.execute("SELECT id FROM teams WHERE LOWER(name)=LOWER(%s)", (name.lower(),))
        r = cur.fetchone()
        if r:
            return r[0]
        # 🔧 Create placeholder legacy team
        cur.execute(
            "INSERT INTO teams (name, status) VALUES (%s, %s) RETURNING id",
            (name, "Legacy")
        )
        tid = cur.fetchone()[0]
        print(f"🆕 Created placeholder team '{name}' (id {tid})")
        nonlocal created_placeholders
        created_placeholders += 1
        return tid

    # --- Parse & insert matches ---
    for row in rows:
        if not row or not row[0].strip():
            continue

        match_code = row[0].strip()
        team_a = row[1].strip() if len(row) > 1 else ""
        team_b = row[2].strip() if len(row) > 2 else ""
        proposed_str = row[3].strip() if len(row) > 3 else ""
        scheduled_str = row[4].strip() if len(row) > 4 else ""
        status_raw = row[5].strip() if len(row) > 5 else ""
        winner_name = row[6].strip() if len(row) > 6 else None
        loser_name = row[7].strip() if len(row) > 7 else None

        # Temporary place-holder, real logic below
        final_status = "Finished"

        # Determine winner/loser IDs BEFORE deciding final status
        winner_id = None
        loser_id = None

        if winner_name:
            # Try team name first
            winner_id = ensure_team(winner_name)
            # If not a team, try resolving the player's team
            if winner_id is None:
                winner_id = resolve_team_from_player(winner_name)

        if loser_name:
            loser_id = ensure_team(loser_name)
            if loser_id is None:
                loser_id = resolve_team_from_player(loser_name)

        # Count score rows NOW (we check after scoring migration too)
        cur.execute("SELECT COUNT(*) FROM match_scores WHERE match_id = (SELECT id FROM matches WHERE match_code=%s)", (match_code,))
        score_count = cur.fetchone()[0] if cur.rowcount else 0

        # ====== FINAL STATUS LOGIC ======
        # 1) Double Forfeit → no winner, no loser
        if not winner_id and not loser_id:
            final_status = "Double Forfeit"

        # 2) Forfeit → winner & loser exist, but NO map scores
        elif winner_id and loser_id and score_count == 0:
            final_status = "Forfeit"

        # 3) Finished → winner & loser AND map scores exist
        elif winner_id and loser_id and score_count > 0:
            final_status = "Completed"

        # 4) Pending / Proposed cases
        elif status_raw.lower() in ["pending", "tbd", "proposed", ""]:
            final_status = "Proposed"

        # 5) Scheduled
        elif status_raw.lower() == "scheduled":
            final_status = "Scheduled"

        # 6) Fallback
        else:
            final_status = "Finished"

        def safe_parse_date(s):
            if not s or s.upper() == "TBD":
                return None

            s = s.strip()

            try:
                # Discord timestamp: <t:1728172800:f>
                if s.startswith("<t:") and s.endswith(":f>"):
                    ts = int(s.split(":")[1])
                    return datetime.fromtimestamp(ts, UTC)

                # Plain UNIX timestamp
                if s.isdigit():
                    return datetime.fromtimestamp(int(s), UTC)

                # Fallback to your regular date parser
                return parse_date(s)

            except Exception:
                return None

        scheduled_date = safe_parse_date(scheduled_str) or safe_parse_date(proposed_str)

        # --- Auto-create teams if not found ---
        team_a_id = ensure_team(team_a)
        team_b_id = ensure_team(team_b)

        try:
            cur.execute("""
                INSERT INTO matches
                    (match_code, team_a_id, team_b_id, scheduled_date, status, winner_id, loser_id)
                VALUES (%s, %s, %s, %s, %s, %s, %s)
                ON CONFLICT (match_code) DO UPDATE
                    SET team_a_id = COALESCE(EXCLUDED.team_a_id, matches.team_a_id),
                        team_b_id = COALESCE(EXCLUDED.team_b_id, matches.team_b_id),
                        scheduled_date = COALESCE(EXCLUDED.scheduled_date, matches.scheduled_date),
                        status = EXCLUDED.status,
                        winner_id = EXCLUDED.winner_id,
                        loser_id = EXCLUDED.loser_id;
            """, (match_code, team_a_id, team_b_id, scheduled_date, final_status, winner_id, loser_id))

            if cur.rowcount == 1:
                inserted += 1
            else:
                updated += 1
        except Exception as e:
            skipped += 1
            print(f"❌ Failed to insert {match_code}: {e}")

    conn.commit()
    print(f"✅ Migrated Matches: inserted={inserted}, updated={updated}, skipped={skipped}")
    if created_placeholders:
        print(f"🆕 Created {created_placeholders} placeholder 'Legacy' teams for orphaned matches.")


def fix_legacy_week_matches_to_preseason():
        """Normalize legacy match rows like 'Week5-M006' to be Preseason.

        This is idempotent and safe to run multiple times.
        It also sets matches.week from the WeekN portion of match_code.
        """
        print("🧹 Fixing legacy Week*-M* matches to Preseason...")

        # Set season
        cur.execute(
                """
                UPDATE matches
                     SET season = 'Preseason'
                 WHERE match_code ~* '^Week[0-9]+-M'
                     AND (season IS DISTINCT FROM 'Preseason');
                """
        )
        season_fixed = cur.rowcount

        # Set week extracted from match_code
        cur.execute(
                """
                UPDATE matches
                     SET week = regexp_replace(match_code, '^Week([0-9]+)-M.*$', '\\1')
                 WHERE match_code ~* '^Week[0-9]+-M'
                     AND (week IS DISTINCT FROM regexp_replace(match_code, '^Week([0-9]+)-M.*$', '\\1'));
                """
        )
        week_fixed = cur.rowcount

        conn.commit()
        print(f"✅ Legacy match normalization complete: season_fixed={season_fixed}, week_fixed={week_fixed}")

def migrate_scoring():
    """Migrate per-map scores from the Scoring sheet into match_scores (horizontal layout)."""
    init_sheets()
    try:
        ws = spreadsheet.worksheet("Scoring")
    except gspread.WorksheetNotFound:
        print("⚠️ 'Scoring' sheet not found. Skipping.")
        return

    rows = ws.get_all_values()
    if not rows or len(rows) < 2:
        print("⚠️ No data in Scoring sheet.")
        return

    inserted, updated, skipped = 0, 0, 0
    rows = rows[1:]  # skip header

    for row in rows:
        if not row or not row[0].strip():
            continue

        match_code = row[0].strip()

        # --- Lookup the match ---
        cur.execute(
            "SELECT id, team_a_id, team_b_id FROM matches WHERE match_code = %s",
            (match_code,),
        )
        match_row = cur.fetchone()
        if not match_row:
            print(f"⚠️ No match found for {match_code}, skipping row.")
            skipped += 1
            continue

        match_id, team_a_id, team_b_id = match_row

        # --- Load DB team names ---
        cur.execute("SELECT name FROM teams WHERE id=%s", (team_a_id,))
        db_team_a = cur.fetchone()[0] if cur.rowcount else ""
        cur.execute("SELECT name FROM teams WHERE id=%s", (team_b_id,))
        db_team_b = cur.fetchone()[0] if cur.rowcount else ""

        # --- Sheet team names ---
        sheet_team_a = row[1].strip() if len(row) > 1 else ""
        sheet_team_b = row[2].strip() if len(row) > 2 else ""

        # --- Determine if flipped ---
        flipped = False
        if (
            sheet_team_a.lower() == db_team_b.lower()
            and sheet_team_b.lower() == db_team_a.lower()
        ):
            flipped = True
            print(f"🔁 Flipped scores for {match_code} (sheet A/B reversed)")

        # --- Helper for integer conversion ---
        def safe_int(v):
            try:
                return int(str(v).strip()) if str(v).strip() != "" else 0
            except:
                return 0

        # --- Loop over 3 maps per row ---
        for i in range(3):
            base = 3 + (i * 3)  # Map 1 cols start at 3, Map 2 at 6, Map 3 at 9
            if len(row) <= base + 2:
                continue

            mode = row[base].strip() if len(row) > base and row[base] else ""
            if not mode:
                continue

            score_a = safe_int(row[base + 1]) if len(row) > base + 1 else 0
            score_b = safe_int(row[base + 2]) if len(row) > base + 2 else 0

            # 🔁 Swap if flipped
            if flipped:
                score_a, score_b = score_b, score_a

            # --- Insert or update ---
            cur.execute(
                """
                INSERT INTO match_scores (match_id, map_number, gamemode, team_a_score, team_b_score)
                VALUES (%s, %s, %s, %s, %s)
                ON CONFLICT (match_id, map_number) DO UPDATE
                SET gamemode = EXCLUDED.gamemode,
                    team_a_score = EXCLUDED.team_a_score,
                    team_b_score = EXCLUDED.team_b_score
                """,
                (match_id, i + 1, mode, score_a, score_b),
            )

            if cur.rowcount == 1:
                inserted += 1
            else:
                updated += 1

    conn.commit()
    print(f"✅ Migrated Scoring: inserted={inserted}, updated={updated}, skipped={skipped}")

def migrate_player_history():
    """Snapshot all team rosters into player_history (by current season)."""
    print("📜 Building Player History...")

    current_season = "Preseason"

    # Clear this season's records
    cur.execute("DELETE FROM player_history WHERE season=%s", (current_season,))

    # Insert all members with their team roles
    cur.execute("""
        INSERT INTO player_history (player_id, team_id, team_name, role, season)
        SELECT tm.player_id, tm.team_id, t.name, tm.role, %s
        FROM team_members tm
        JOIN teams t ON t.id = tm.team_id
    """, (current_season,))

    conn.commit()
    print("✅ Player History recorded for", current_season)

def resolve_team_from_player(player_name):
    """Return the player's team_id if the sheet names the PLAYER, not the TEAM."""
    clean = normalize_name(player_name)
    normalized_map = {normalize_name(k): v for k, v in player_map.items()}

    # If player exists, find their team
    if clean in normalized_map:
        discord_id = normalized_map[clean][0]
        cur.execute("SELECT team_id FROM team_members WHERE player_id=%s", (discord_id,))
        r = cur.fetchone()
        if r:
            return r[0]  # team_id

def migrate_match_rosters():
    print("📦 Migrating roster snapshots for legacy + completed matches...")

    # Get ALL completed matches (legacy + modern)
    cur.execute("""
        SELECT id, team_a_id, team_b_id
        FROM matches
        WHERE status IN ('Completed', 'Finished')
    """)
    matches = cur.fetchall()

    created = 0
    skipped = 0

    for match_id, team_a_id, team_b_id in matches:

        # Skip if snapshot already exists
        cur.execute(
            "SELECT 1 FROM match_rosters WHERE match_id=%s LIMIT 1",
            (match_id,)
        )
        if cur.fetchone():
            skipped += 1
            continue

        def load_team(team_id):
            # 🔑 LEGACY-SAFE SOURCE
            cur.execute("""
                SELECT p.id, p.display_name, p.username, tm.role
                FROM team_members tm
                JOIN players p ON p.id = tm.player_id
                WHERE tm.team_id = %s
            """, (team_id,))
            return cur.fetchall()

        roster_a = load_team(team_a_id)
        roster_b = load_team(team_b_id)

        if not roster_a or not roster_b:
            print(f"⚠️ Match {match_id}: missing roster data, skipping")
            continue

        for team_id, roster in [(team_a_id, roster_a), (team_b_id, roster_b)]:
            for pid, display, user, role in roster:
                cur.execute("""
                    INSERT INTO match_rosters
                        (match_id, team_id, player_id, display_name, username, role)
                    VALUES (%s, %s, %s, %s, %s, %s)
                """, (match_id, team_id, pid, display, user, role))

        created += 1

    conn.commit()
    print(f"✅ Roster snapshots complete: created={created}, skipped={skipped}")

    return None

def run_full_migration():
    global player_map
    init_sheets()
    player_map = build_player_map()

    # order matters
    migrate_teams()
    migrate_players_sheet()
    migrate_players_leaderboard()
    migrate_team_members()
    migrate_team_leaderboard()
    migrate_player_history()
    migrate_scoring()
    migrate_matches()
    fix_legacy_week_matches_to_preseason()
    migrate_match_rosters()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--fix-week-preseason",
        action="store_true",
        help="Only normalize legacy Week*-M* matches to season=Preseason and set week from match_code.",
    )
    args = parser.parse_args()

    try:
        if args.fix_week_preseason:
            fix_legacy_week_matches_to_preseason()
        else:
            run_full_migration()
    finally:
        try:
            cur.close()
        finally:
            conn.close()


if __name__ == "__main__":
    main()