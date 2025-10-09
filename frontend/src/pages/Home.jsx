export default function Home() {
  return (
    <div className="rules text-start">
      <h1>📢 Welcome to the Echo Combat George League!</h1>
      <p>Here’s everything you need to know to get started:</p>

      <hr />
      <h2>⚠️ Platform Requirement Notice</h2>
      <blockquote>
        Echo Combat is only available on PCVR — either through SteamVR or Oculus PC, or on Quest using Air Link or Oculus Link cable.  
        This league is for PCVR players only. ❌ Quest-native players are not eligible.
      </blockquote>

      <hr />
      <h2>🏁 Signups</h2>
      <ul>
        <li>✅ Join as a <b>Player</b> or <b>League Sub</b></li>
        <li>👥 Teams must have <b>3–5 players</b></li>
        <li>👑 Captains manage scheduling & scores</li>
      </ul>

      <hr />
      <h2>📅 Match Types</h2>
      <ul>
        <li>🔄 <b>Assigned Matches</b> – Auto-posted weekly</li>
        <li>⚔️ <b>Challenge Matches</b> – 1 per team per week</li>
        <li>✅ Captains must accept before the match is official</li>
        <li>📅 Each team is assigned <b>2 matchups per week</b></li>
        <li>⚠️ <b>Postponed Matches</b> – Must be played before the next week starts</li>
        <li>🚫 Matches not completed on time are auto-forfeited (no elo loss)</li>
        <li>⚠️ Extensions are not guaranteed — schedule early!</li>
      </ul>

      <hr />
      <h2>🎮 Match Flow</h2>
      <ul>
        <li>3v3 format</li>
        <li>🗺️ Best-of-3 maps (first to 2 wins)</li>
        <li>🎲 Coin flip or agreement picks Map 1</li>
        <li>🔁 Loser picks the next map and side</li>
        <li>🚫 No repeat maps</li>
        <li>✅ All official maps allowed</li>
        <li>📊 Use Propose Score to submit results</li>
        <li>🛡️ Opponent must confirm scores</li>
        <li>📈 Leaderboard updates automatically</li>
      </ul>

      <hr />
      <h2>🛠️ Gamemodes</h2>
      <h4>⚙️ Payload</h4>
      <ul>
        <li>Each team takes turns pushing the payload. The team that pushes it the furthest wins.</li>
        <li>If both finish, they each attack again with remaining time.</li>
        <li>This continues until one pushes further or successfully defends.</li>
      </ul>

      <h4>🎯 Capture Point</h4>
      <ul>
        <li>Best-of-3 rounds per map</li>
        <li>First team to win 2 rounds takes the map</li>
      </ul>

      <h4>🔫 Loadout Rules</h4>
      <ul>
        <li>🚫 Combat chassis only — no exceptions</li>
        <li>🔒 Max 2 weapons/tac mods/ordnances per team</li>
        <li>⚠️ Experimentals allowed (majority vote), but risky:
          <ul>
            <li>Some may break loadouts</li>
            <li>No glitching outside map, enemy spawn, or exploits → penalties</li>
          </ul>
        </li>
      </ul>

      <hr />
      <h2>🏆 ELO Rankings</h2>
      <ul>
        <li>You gain or lose ELO after each match</li>
        <li>Tracked for both teams and players</li>
        <li>Updates hourly</li>
      </ul>

      <hr />
      <h2>🔄 Subs & Rosters</h2>
      <ul>
        <li>👥 Matches are 3v3</li>
        <li>🔍 Use Find Subs to ping eligible subs</li>
        <li>⚖️ Subs must have equal or lower ELO</li>
        <li>1 sub per match</li>
        <li>🌐 Players must stay under 200ms ping</li>
      </ul>

      <hr />
      <h2>🏷️ Team Rules</h2>
      <ul>
        <li>✅ Name must match registration</li>
        <li>🚫 No slurs, profanity, or offensive content</li>
        <li>🛑 Violations may block match actions</li>
      </ul>

      <hr />
      <h2>⏱️ Match Timing</h2>
      <ul>
        <li>⌛ 10+ minutes late = possible forfeit</li>
        <li>⏸️ 10-minute break allowed between maps</li>
        <li>🤝 3v3 required</li>
      </ul>

      <hr />
      <h2>🚫 Conduct</h2>
      <ul>
        <li>✅ Respect all players and staff</li>
        <li>❌ Toxicity, threats, hacking/cheating, trolling = suspension/ban</li>
        <li>📸 Evidence required for disputes</li>
        <li>⚠️ Rule violations = map loss, forfeit, or ban</li>
      </ul>

      <hr />
      <h2>📋 Eligibility</h2>
      <h4>⚠️ Platform</h4>
      <ul>
        <li>Must play on PCVR (SteamVR, Oculus PC, or Quest Link/Air Link)</li>
        <li>❌ Quest-native not eligible</li>
      </ul>
      <h4>👥 Team Size</h4>
      <ul>
        <li>Teams must have 3–5 players</li>
        <li>Minimum 3 registered to compete</li>
        <li>Must use same Discord account in-game</li>
      </ul>
      <h4>🎖️ League Subs</h4>
      <ul>
        <li>Must sign up as Sub</li>
        <li>Can only sub if ELO ≤ team they sub for</li>
        <li>Max 1 sub per match</li>
        <li>Cannot join team roster</li>
      </ul>
      <h4>🌍 Network</h4>
      <ul>
        <li>Must stay under 200ms ping</li>
        <li>Wired recommended</li>
      </ul>

      <hr />
      <h2>❌ Forfeits & Inactivity</h2>
      <h4>📅 Scheduling Expectations</h4>
      <p>Weekly matchups must be played before the next cycle. Teams must propose times promptly.</p>
      <h4>❌ Forfeit Conditions</h4>
      <ul>
        <li>Cancel last-minute without reschedule</li>
        <li>No-show at agreed time</li>
        <li>No effort to schedule before deadline</li>
      </ul>
      <h4>⚖️ Forfeit Outcomes</h4>
      <ul>
        <li>One-team forfeit → loss for that team, win for opponent</li>
        <li>Double forfeit → both lose</li>
        <li>Forfeits affect ELO, standings, eligibility</li>
      </ul>
      <h4>🚫 Postponement Policy</h4>
      <p>
        Only valid reasons: game servers offline or widespread verified technical issues.  
        Must contact mods immediately with context. Otherwise = double forfeit.
      </p>
    </div>
  );
}
