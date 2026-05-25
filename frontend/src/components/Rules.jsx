import { useState, useEffect } from "react";
import axios from "axios";
import { getApiUrl } from "../config";

function renderContent(content) {
    const lines = content.split("\n");
    const elements = [];
    let currentList = [];
    let blockquoteLines = [];

    const flushList = () => {
        if (currentList.length > 0) {
            elements.push(<ul key={elements.length}>{currentList}</ul>);
            currentList = [];
        }
    };

    const flushBlockquote = () => {
        if (blockquoteLines.length > 0) {
            elements.push(
                <blockquote key={elements.length}>
                    {blockquoteLines.map((l, i) => <span key={i}>{l}<br /></span>)}
                </blockquote>
            );
            blockquoteLines = [];
        }
    };

    for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        const trimmed = line.trim();

        if (!trimmed) {
            flushList();
            flushBlockquote();
            continue;
        }

        if (trimmed.startsWith("> ")) {
            flushList();
            blockquoteLines.push(trimmed.slice(2));
            continue;
        } else if (blockquoteLines.length > 0) {
            flushBlockquote();
        }

        if (trimmed.startsWith("### ")) {
            flushList();
            elements.push(<h4 key={elements.length}>{trimmed.slice(4)}</h4>);
        } else if (trimmed.startsWith("- ")) {
            const text = trimmed.slice(2);
            // Check if next lines are indented sub-items
            const subItems = [];
            while (i + 1 < lines.length && lines[i + 1].match(/^\s{2,}-\s/)) {
                i++;
                subItems.push(lines[i].trim().slice(2));
            }
            if (subItems.length > 0) {
                currentList.push(
                    <li key={currentList.length}>
                        {text}
                        <ul>{subItems.map((s, j) => <li key={j}>{s}</li>)}</ul>
                    </li>
                );
            } else {
                currentList.push(<li key={currentList.length}>{text}</li>);
            }
        } else {
            flushList();
            elements.push(<p key={elements.length}>{trimmed}</p>);
        }
    }

    flushList();
    flushBlockquote();

    return elements;
}

export default function Rules() {
    const [sections, setSections] = useState(null);
    const [error, setError] = useState(false);

    useEffect(() => {
        axios.get(`${getApiUrl()}/api/rules`)
            .then(res => {
                if (res.data && res.data.length > 0) {
                    setSections(res.data);
                } else {
                    setError(true);
                }
            })
            .catch(() => setError(true));
    }, []);

    if (error) return <StaticRules />;
    if (sections === null) return <p className="text-secondary">Loading rules…</p>;

    return (
        <div className="rules text-start">
            {sections.map((section, idx) => (
                <div key={section.id || idx}>
                    <hr />
                    <h2>{section.title}</h2>
                    {renderContent(section.content)}
                </div>
            ))}
        </div>
    );
}

export function StaticRules() {
    return (
        <div className="rules text-start">

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
            <ul>
                <li>⚙️ Payload: Teams alternate attacking and defending. Team that pushes farther wins.</li>
                <li>🎯 Capture Point: Best-of-3 rounds. First to 2 wins the map.</li>
                <li>🔫 Combat chassis only — no exceptions</li>
                <li>📦 3v3: Max 1 weapon/tac/ordnance per team. 4v4: Max 2.</li>
                <li>⚠️ Experimentals allowed by majority vote but risky.</li>
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
                <li>🔍 Use Find Subs to ping eligible substitutes</li>
                <li>⚖️ All subs allowed</li>
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
                <li>🤝 3 players minimum required to start</li>
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
            <ul>
                <li>Must play on PCVR using SteamVR, Oculus PC, or Link/Air Link</li>
                <li>❌ Quest-native unsupported</li>
                <li>Teams must roster 3–5 players, minimum 3 to compete</li>
                <li>League Subs: max 1 per match per team, cannot join roster</li>
                <li>Must stay under 200ms ping, wired recommended</li>
            </ul>

            <hr />
            <h2>❌ Forfeits & Inactivity</h2>
            <ul>
                <li>Matches must be completed before next cycle</li>
                <li>Cancelling last-minute, no-show, or failing to schedule → forfeit</li>
                <li>One-team forfeit → win for opponent. Double forfeit → both lose.</li>
                <li>Affects ELO and standings</li>
                <li>Postponement only valid for server outages; mods must be contacted immediately</li>
            </ul>

        </div>
    );
}
