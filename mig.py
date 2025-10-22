import gspread
import re
import psycopg2
import difflib
from oauth2client.service_account import ServiceAccountCredentials
from pytz import timezone

# === Google Sheets ===
scope = [
    "https://spreadsheets.google.com/feeds",
    "https://www.googleapis.com/auth/spreadsheets",
    "https://www.googleapis.com/auth/drive"
]
creds = ServiceAccountCredentials.from_json_keyfile_name("credentials.json", scope)
client = gspread.authorize(creds)

SHEET_NAME = "CombatLeaguePre"
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

player_map = build_player_map()

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
        status = row[5].strip().capitalize() if len(row) > 5 else "Finished"
        winner_name = row[6].strip() if len(row) > 6 else None
        loser_name = row[7].strip() if len(row) > 7 else None

        valid_statuses = {
            "finished": "Finished",
            "forfeited": "Forfeited",
            "double forfeit": "Forfeited",
            "pending": "Proposed",
            "scheduled": "Scheduled",
            "tbd": "Proposed",
            "": "Proposed",
        }
        status = valid_statuses.get(status.lower(), "Finished")

        def safe_parse_date(s):
            if not s or s.upper() == "TBD":
                return None
            s = s.strip()
            try:
                if s.startswith("<t:") and s.endswith(":f>"):
                    ts = int(s.split(":")[1])
                    return datetime.utcfromtimestamp(ts)
                if s.isdigit():
                    return datetime.utcfromtimestamp(int(s))
                return parse_date(s)
            except Exception:
                return None

        scheduled_date = safe_parse_date(scheduled_str) or safe_parse_date(proposed_str)

        # --- Auto-create teams if not found ---
        team_a_id = ensure_team(team_a)
        team_b_id = ensure_team(team_b)
        winner_id = ensure_team(winner_name) if winner_name else None
        loser_id = ensure_team(loser_name) if loser_name else None

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
            """, (match_code, team_a_id, team_b_id, scheduled_date, status, winner_id, loser_id))

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

def migrate_scoring():
    """Migrate per-map scores from the Scoring sheet into match_scores (horizontal layout)."""
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
        ON CONFLICT (player_id, team_id, season) DO UPDATE
            SET team_name = EXCLUDED.team_name,
                role = EXCLUDED.role;
    """, (current_season,))

    conn.commit()
    print("✅ Player History recorded for", current_season)

# === Run Migration (order matters!) ===
migrate_teams()
migrate_players_sheet()
migrate_players_leaderboard()
migrate_team_members()
migrate_team_leaderboard()
migrate_player_history()
migrate_matches()
migrate_scoring()

cur.close()
conn.close()