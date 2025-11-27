export default function Home() {
  return (
    <div className="rules text-start">
      <h1>📢 Welcome to the Echo Combat George League!</h1>
      <p>Your guide to format, rules, and expectations for ECGL.</p>

      <hr />
      <h2>⚠️ Platform Requirement Notice</h2>
      <blockquote>
        Echo Combat is available only through PCVR (SteamVR, Oculus PC, or Quest via Link/Air Link).
        This league is for <b>PCVR players only</b>. ❌ Quest-native players are not eligible.
      </blockquote>

      <hr />
      <h2>🏁 Signups</h2>
      <ul>
        <li>✅ Sign up as a <b>Player</b> or <b>League Sub</b></li>
        <li>👥 Teams must roster <b>3–5 players</b></li>
        <li>📝 Log in with Discord and register on the website</li>
      </ul>

      <hr />
      <h2>📅 Match Types</h2>
      <ul>
        <li>🔄 <b>Assigned Matches</b> – matches automatically generated each week by the league.</li>
        <li>⚔️ <b>Challenge Matches</b> – optional extra matches that teams may choose to schedule (limit: 1 per team per week).</li>
        <li>📅 Each team receives <b>2 scheduled matchups per week</b></li>
        <li>⚠️ Postponed matches must be played before the next week</li>
        <li>🚫 Matches not completed on time are auto-forfeited (ELO loss)</li>
      </ul>

      <hr />
      <h2>👥 Team Size & Match Format</h2>
      <ul>
        <li>🟦 Matches are played as <b>3v3 by default</b></li>
        <li>🟩 <b>4v4 is allowed</b> if—and only if—<b>both teams have 4 eligible players</b> ready to play</li>
        <li>🟧 If either team has only 3 players, the match is automatically <b>3v3</b></li>
        <li>🤝 Minimum 3 players required to avoid forfeit</li>
        <li>🔄 Both formats follow the same ruleset (best-of-3 maps)</li>
      </ul>

      <hr />
      <h2>🎮 Match Flow</h2>
      <ul>
        <li>🗺️ <b>Best-of-3 Maps</b> (first to 2 wins)</li>
        <li>🎲 Coin flip or agreement determines Map 1</li>
        <li>🔁 Loser picks next map and side</li>
        <li>🚫 No repeat maps</li>
        <li>📊 Scoring is done inside My Team tab under Active Matches</li>
        <li>🛡️ Opponent must confirm scores</li>
        <li>📈 Leaderboard updates automatically</li>
      </ul>

      <hr />
      <h2>🛠️ Gamemodes</h2>

      <h4>⚙️ Payload</h4>
      <ul>
        <li>Teams alternate attacking and defending</li>
        <li>Team that pushes the payload farther wins</li>
        <li>If both finish, overtime uses remaining time</li>
      </ul>

      <h4>🎯 Capture Point</h4>
      <ul>
        <li>Best-of-3 rounds</li>
        <li>First team to win 2 rounds wins the map</li>
      </ul>

      <h4>🔫 Loadout Rules</h4>
      <ul>
        <li>🚫 Combat chassis only — no exceptions</li>

        <li>📦 <b>Loadout limits scale with match size:</b>
          <ul>
            <li>🔹 <b>3v3:</b> Max <b>1</b> weapon / tac mod / ordnance per team
              <span className="text-light"></span></li>
            <li>🔹 <b>4v4:</b> Max <b>2</b> weapons / tac mods / ordnances per team
              <span className="text-light"></span></li>
          </ul>
        </li>

        <li>⚠️ Experimentals allowed (majority vote), but risky:
          <ul>
            <li>Some may break loadouts or cause conflicts</li>
            <li>Using exploits, leaving the map, or entering enemy spawn = can result in penalties or match forfeit.</li>
          </ul>
        </li>
      </ul>

      <hr />
      <h2>🏆 ELO Rankings</h2>
      <ul>
        <li>ELO gained or lost each match</li>
        <li>Tracked for both teams and players</li>
      </ul>

      <hr />
      <h2>🔄 Subs & Rosters</h2>
      <ul>
        <li>🔍 Use <b>Find Subs</b> to ping eligible substitutes</li>
        <li>⚖️ All subs allowed </li>
        <li>1 league sub allowed per match</li>
        <li>👥 Team roster minimum: 3 players</li>
        <li>🌐 Players must stay under 200ms ping</li>
      </ul>

      <hr />
      <h2>🏷️ Team Rules</h2>
      <ul>
        <li>Team name must match registration</li>
        <li>No slurs, offensive content, or impersonation</li>
        <li>Violations may block match actions</li>
      </ul>

      <hr />
      <h2>⏱️ Match Timing</h2>
      <ul>
        <li>⌛ 10+ minutes late → possible forfeit</li>
        <li>⏸️ 10-minute break allowed between maps</li>
        <li>🤝 <b>3 players minimum</b> required to start</li>
        <li>🟦 4v4 allowed if both teams agree and have 4 players</li>
      </ul>

      <hr />
      <h2>🚫 Conduct</h2>
      <ul>
        <li>Respect players and staff</li>
        <li>No toxicity, threats, hacking, cheating, or trolling</li>
        <li>Evidence required for disputes</li>
        <li>Rule violations may result in map loss, match loss, or bans</li>
      </ul>

      <hr />
      <h2>📋 Eligibility</h2>

      <h4>⚠️ Platform</h4>
      <ul>
        <li>Must play on PCVR using SteamVR, Oculus PC, or Link/Air Link</li>
        <li>❌ Quest-native unsupported</li>
      </ul>

      <h4>👥 Team Size</h4>
      <ul>
        <li>Teams must roster 3–5 players</li>
        <li>Minimum 3 registered to compete</li>
        <li>Must use the same Discord account in-game</li>
      </ul>

      <h4>🎖️ League Subs</h4>
      <ul>
        <li>Must sign up as a Sub</li>
        <li>Sub can play for any team</li>
        <li>Max 1 sub per match for each team</li>
        <li>Cannot join roster</li>
      </ul>

      <h4>🌍 Network</h4>
      <ul>
        <li>Must stay under 200ms ping</li>
        <li>Wired strongly recommended</li>
      </ul>

      <hr />
      <h2>❌ Forfeits & Inactivity</h2>

      <h4>📅 Scheduling Expectations</h4>
      <p>
        Weekly matchups must be completed before the next cycle.
        Teams must propose times promptly or risk forfeits.
      </p>

      <h4>❌ Forfeit Conditions</h4>
      <ul>
        <li>Cancelling last-minute without reschedule</li>
        <li>No-show at agreed time</li>
        <li>Failure to attempt scheduling before deadline</li>
      </ul>

      <h4>⚖️ Forfeit Outcomes</h4>
      <ul>
        <li>One-team forfeit → win for opponent</li>
        <li>Double forfeit → both lose</li>
        <li>Affects ELO and standings</li>
      </ul>

      <h4>🚫 Postponement Policy</h4>
      <p>
        Only valid if Echo VR servers are offline or widespread technical issues occur.
        Mods must be contacted immediately with context.
        Otherwise → double forfeit.
      </p>
    </div>
  );
}
