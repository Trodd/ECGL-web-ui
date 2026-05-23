import { useEffect, useState } from "react";
import axios from "axios";
import MatchCard from "../components/MatchCard";

export default function MyTeam() {
  const [data, setData] = useState({
    team: {},
    roster: [],
    matches: [],
    requests: [],
    myRole: "",
  });
  const [loading, setLoading] = useState(true);
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [msg, setMsg] = useState("");
  const [selectedSeason, setSelectedSeason] = useState("All");
  const [currentSeason, setCurrentSeason] = useState("Preseason");
  const [newTeamName, setNewTeamName] = useState("");
  const [teamSettings, setTeamSettings] = useState({});
  const [challengeRequests, setChallengeRequests] = useState([]);
  const [allowChallenges, setAllowChallenges] = useState(true);
  const [globalChallengesEnabled, setGlobalChallengesEnabled] = useState(true);
  const [arenaModeEnabled, setArenaModeEnabled] = useState(false);

  const [logoFile, setLogoFile] = useState(null);
  const [logoPreviewUrl, setLogoPreviewUrl] = useState("");
  const [logoUploading, setLogoUploading] = useState(false);
  const [logoMsg, setLogoMsg] = useState("");
  const [logoVersion, setLogoVersion] = useState("");
  const [logoLoadFailed, setLogoLoadFailed] = useState(false);

  const urlBase = import.meta.env.VITE_API_URL;
  const sectionStyle = {
    backgroundColor: "#1a1a1a",
    border: "1px solid #333",
    borderRadius: "0.5rem",
    padding: "1rem",
    width: "100%",
    maxWidth: "800px",
    margin: "0 auto 1.5rem auto",
  };

  const [accordionOpen, setAccordionOpen] = useState(
    localStorage.getItem("accordion_team_settings_open") === "true"
  );

  useEffect(() => {
    const saved = localStorage.getItem("accordion_team_settings_open");
    if (saved === "true") setAccordionOpen(true);
  }, []);

  const [rosterLocked, setRosterLocked] = useState(false);

  useEffect(() => {
    async function loadRosterLockStatus() {
      try {
        const res = await axios.get(`${urlBase}/api/mod/roster/status`, {
          withCredentials: true,
        });
        setRosterLocked(!!res.data.locked);
      } catch {
        setRosterLocked(false);
      }
    }
    loadRosterLockStatus();
  }, []);

  // 🔄 Load global challenge setting
  useEffect(() => {
    axios
      .get(`${urlBase}/api/settings`, { withCredentials: true })
      .then(res => {
        if (res.data?.challenges_enabled !== undefined) {
          setGlobalChallengesEnabled(res.data.challenges_enabled);
        }
        if (res.data?.arena_mode_enabled !== undefined) {
          setArenaModeEnabled(res.data.arena_mode_enabled);
        }
      })
      .catch(() => {
        setGlobalChallengesEnabled(true);
        setArenaModeEnabled(false);
      });
  }, []);

  useEffect(() => {
    axios.get(`${urlBase}/api/myteam`, { withCredentials: true })
      .then(res => {
        setTeamSettings(res.data.team);
        setAllowChallenges(!!res.data.team?.allow_challenges);
        setChallengeRequests(res.data.challenge_requests || []);
      })
      .catch(() => {
        setTeamSettings({});
        setChallengeRequests([]);
      });
  }, []);

  async function loadTeam() {
    try {
      setLoading(true);
      // 🧩 Support ?as=<discord_id> override in DEV_MODE
      const query = new URLSearchParams(window.location.search);
      const asParam = query.get("as");
      const url = asParam
        ? `${urlBase}/api/myteam?as=${asParam}`
        : `${urlBase}/api/myteam`;

      const res = await axios.get(url, { withCredentials: true });

      setData({
        team: res.data?.team || {},
        roster: Array.isArray(res.data?.roster) ? res.data.roster : [],
        matches: Array.isArray(res.data?.matches) ? res.data.matches : [],
        requests: Array.isArray(res.data?.requests) ? res.data.requests : [],
        myRole: res.data?.myRole || "",
      });

      setTeamSettings(res.data.team || {});
      setAllowChallenges(res.data.team?.allow_challenges ?? true);
      setChallengeRequests(res.data.challenge_requests || []);

    } catch (err) {
      console.error("❌ Failed to load MyTeam:", err);
      setData({
        team: {},
        roster: [],
        matches: [],
        requests: [],
        myRole: "",
      });
      setMsg("⚠️ Failed to load team data.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadTeam();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const { team, roster, matches, requests, myRole } = data;
  const isLockedByMods = Boolean(team?.locked);
  const joinDisabled = rosterLocked || isLockedByMods;
  const isCaptain = myRole === "Captain" || myRole === "Co-Captain";

  const effectiveLogoUrl = team?.logo_url || teamSettings?.logo_url || (team?.id ? `/api/team/logo/${team.id}` : "");

  function buildLogoSrc(logoUrl) {
    if (!logoUrl) return "";
    const base = String(logoUrl);
    const absolute = base.startsWith("http://") || base.startsWith("https://");
    const src = absolute ? base : `${urlBase}${base}`;
    return logoVersion ? `${src}${src.includes("?") ? "&" : "?"}v=${encodeURIComponent(String(logoVersion))}` : src;
  }

  useEffect(() => {
    return () => {
      if (logoPreviewUrl) URL.revokeObjectURL(logoPreviewUrl);
    };
  }, [logoPreviewUrl]);

  // 🔄 Load current season label (from backend)
  useEffect(() => {
    axios
      .get(`${urlBase}/api/season`)
      .then((res) => {
        if (res.data?.season) {
          let s = res.data.season.toString().trim();
          if (/^\d+$/.test(s)) s = `Season ${s}`;
          setCurrentSeason(s);
        }
      })
      .catch(() => setCurrentSeason("Preseason"));
  }, []);


  if (loading) return <p>⏳ Fetching team data…</p>;
  if (!team?.id) {
    return (
      <div
        className="d-flex justify-content-center align-items-center text-light"
        style={{ minHeight: "60vh" }}
      >
        <div
          className="text-center p-4 rounded-4 shadow"
          style={{
            background: "linear-gradient(145deg, #121212, #1a1a1a)",
            border: "1px solid rgba(255,255,255,0.08)",
            maxWidth: 420,
            width: "100%",
          }}
        >
          <div
            className="mb-3"
            style={{ fontSize: "3rem", opacity: 0.85 }}
          >
            👥
          </div>

          <h4 className="fw-semibold mb-2">
            You’re not on a team yet
          </h4>

          <p className="text-secondary small mb-4">
            Join an existing team or create your own to start competing in ECGL.
          </p>

          <div className="d-flex justify-content-center gap-2 flex-wrap">
            <a
              href="/teams"
              className="btn btn-outline-info btn-sm px-4"
            >
              Browse Teams
            </a>

            <a
              href="/register"
              className="btn btn-outline-success btn-sm px-4"
            >
              Create / Join team
            </a>
          </div>
        </div>
      </div>
    );
  }

  // Build list of season options
  const allSeasons = ["All", "Preseason"];
  if (currentSeason && !allSeasons.includes(currentSeason))
    allSeasons.push(currentSeason);

  // Extract finished past matches OR previous-season matches
  const filteredPastMatches = (matches ?? []).filter((m) => {
    // Normalize backend season
    let seasonNum = String(m.season ?? "").trim();

    // If backend season missing → derive from match_code
    if (!seasonNum) {
      const prefix = (m.match_code ?? "").split("-")[0];
      seasonNum = /^\d+$/.test(prefix) ? prefix : "0";  // 0 = preseason
    }

    // Normalize current season for comparison
    const currentSeasonNum = currentSeason.replace("Season ", "").trim();

    // Determine finished match
    const isFinished = ["Finished", "Completed", "Forfeit", "Cancelled"]
      .includes((m.status ?? "").trim());

    // A match is PAST if:
    // - finished, OR
    // - season does not match current season
    const isPast = isFinished || seasonNum !== currentSeasonNum;
    if (!isPast) return false;

    // Apply dropdown filter
    if (selectedSeason === "All") return true;
    if (selectedSeason === "Preseason") return seasonNum === "0";

    const selectedNum = selectedSeason.replace("Season ", "").trim();
    return seasonNum === selectedNum;
  });


  // --- Actions (with basic crash prevention) ---

  async function handleDecision(requestID, action) {
    if (!requestID || !action) return;
    try {
      await axios.post(
        `${urlBase}/api/team/join/decision`,
        { request_id: requestID, action },
        { withCredentials: true }
      );
      await loadTeam();
    } catch (err) {
      console.error("❌ Failed to update request:", err);
      alert("Failed to update request");
    }
  }

  async function handleKick(playerId) {
    if (!playerId || !team?.id) return;
    try {
      await axios.post(
        `${urlBase}/api/team/kick`,
        { team_id: team.id, player_id: String(playerId) }, // ✅ string
        { withCredentials: true }
      );
      await loadTeam();
    } catch (err) {
      console.error("Kick failed:", err);
      alert("❌ Failed to kick player");
    }
  }

  async function handleLeaveTeam() {
    if (!team?.id) return;
    try {
      await axios.post(
        `${urlBase}/api/team/leave`,
        { team_id: team.id },
        { withCredentials: true }
      );
      alert("✅ You have left the team.");
      setConfirmLeave(false);
      await loadTeam();
    } catch (err) {
      console.error("Leave failed:", err);
      alert("❌ Failed to leave team");
    }
  }

  async function handlePromote(playerId, role) {
    if (!playerId || !role || !team?.id) return;
    try {
      await axios.post(
        `${urlBase}/api/team/promote`,
        { team_id: team.id, player_id: String(playerId), role }, // ✅ string
        { withCredentials: true }
      );
      await loadTeam();
    } catch (err) {
      console.error("Promote failed:", err);
      alert("❌ Failed to promote player");
    }
  }

  async function respondChallenge(id, accept) {
    try {
      await axios.post(
        `${urlBase}/api/challenge/respond`,
        { challenge_id: id, accept },
        { withCredentials: true }
      );

      loadTeam();  // reload MyTeam data
    } catch (err) {
      alert("Error updating challenge: " + err.response?.data);
    }
  }

  async function uploadTeamLogo() {
    if (!isCaptain) return;
    if (!team?.id) return;
    if (!logoFile) {
      setLogoMsg("⚠️ Choose an image file first.");
      return;
    }

    try {
      setLogoUploading(true);
      setLogoMsg("");
      const form = new FormData();
      form.append("team_id", String(team.id));
      form.append("logo", logoFile);

      const res = await axios.post(`${urlBase}/api/team/logo`, form, {
        withCredentials: true,
      });

      const newLogoUrl = res?.data?.logo_url;
      const newLogoVersion = res?.data?.logo_version;
      setLogoMsg("✅ Team logo updated.");
      setLogoVersion(newLogoVersion ? String(newLogoVersion) : String(Date.now()));
      setLogoLoadFailed(false);

      if (newLogoUrl) {
        setData((prev) => ({
          ...prev,
          team: { ...(prev?.team || {}), logo_url: newLogoUrl },
        }));
        setTeamSettings((prev) => ({ ...(prev || {}), logo_url: newLogoUrl }));
      }

      setLogoFile(null);
      if (logoPreviewUrl) URL.revokeObjectURL(logoPreviewUrl);
      setLogoPreviewUrl("");
      await loadTeam();
    } catch (err) {
      const msg = err?.response?.data || err?.message || "Upload failed";
      setLogoMsg(`❌ ${msg}`);
    } finally {
      setLogoUploading(false);
    }
  }

  return (
    <div className="container text-light py-4" style={{ maxWidth: 1100 }}>
      {/* ================= TEAM HEADER ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <div className="d-flex flex-column flex-md-row justify-content-between align-items-md-center gap-3">
          <div className="d-flex align-items-center gap-3">
            {effectiveLogoUrl && !logoLoadFailed && (
              <img
                src={buildLogoSrc(effectiveLogoUrl)}
                alt="Team logo"
                className="rounded border border-secondary"
                style={{ width: 56, height: 56, objectFit: "cover" }}
                onError={() => setLogoLoadFailed(true)}
              />
            )}

            <div>
              <h2 className="mb-1"> {team?.name}</h2>
              <span
                className={`fw-bold ${team?.status === "Active"
                  ? "text-success"
                  : team?.status === "Inactive"
                    ? "text-warning"
                    : "text-danger"
                  }`}
              >
                {team?.status || "Active"}
              </span>
            </div>
          </div>

          {team?.id && (
            !confirmLeave ? (
              <button
                className="btn btn-outline-danger btn-sm"
                onClick={() => setConfirmLeave(true)}
              >
                🚪 Leave Team
              </button>
            ) : (
              <div className="d-flex gap-2">
                <button className="btn btn-danger btn-sm" onClick={handleLeaveTeam}>
                  ✅ Confirm
                </button>
                <button
                  className="btn btn-secondary btn-sm"
                  onClick={() => setConfirmLeave(false)}
                >
                  ❌ Cancel
                </button>
              </div>
            )
          )}
        </div>

        {/* COOLDOWN BANNER (dark readable text on yellow) */}
        {roster?.some((p) => p?.on_cooldown && p?.role === myRole) && (
          <div className="alert ecgl-alert-warning small mt-3 mb-0">
            <strong>⏳ Cooldown:</strong> You recently left a team. You can’t play matches
            for your new team until the next matchup cycle.
            <br />
          </div>
        )}

        {/* STATUS MSG */}
        {msg && (
          <div
            className={`alert mt-3 mb-0 ${msg.startsWith("✅")
              ? "alert-success"
              : msg.startsWith("⚠️")
                ? "alert-warning"
                : "alert-danger"
              }`}
          >
            {msg}
          </div>
        )}
      </div>

      {/* ================= TEAM SETTINGS ================= */}
      {(myRole === "Captain" || myRole === "Co-Captain") && (
        <div className="accordion mb-4" id="teamSettingsAccordion">
          <div className="accordion-item bg-dark border-secondary rounded shadow-sm overflow-hidden">
            <h2 className="accordion-header">
              <button
                className={`accordion-button bg-dark text-light fw-semibold ${accordionOpen ? "" : "collapsed"
                  }`}
                type="button"
                onClick={() => {
                  const next = !accordionOpen;
                  setAccordionOpen(next);
                  localStorage.setItem("accordion_team_settings_open", next ? "true" : "false");
                }}
              >
                ⚙️ Team Settings
              </button>
            </h2>

            <div className={`accordion-collapse collapse ${accordionOpen ? "show" : ""}`}>
              <div className="accordion-body bg-black text-light">
                {/* TEAM LOGO */}
                <div className="mb-4 border-bottom border-secondary pb-3">
                  <h6 className="text-info mb-2">🖼️ Team Logo</h6>

                  <div className="d-flex flex-column flex-md-row gap-3 align-items-md-center">
                    <div style={{ width: 96, height: 96 }}>
                      {logoPreviewUrl ? (
                        <img
                          src={logoPreviewUrl}
                          alt="Team logo preview"
                          style={{ width: 96, height: 96, objectFit: "cover", borderRadius: 8, border: "1px solid #333" }}
                        />
                      ) : effectiveLogoUrl && !logoLoadFailed ? (
                        <img
                          src={buildLogoSrc(effectiveLogoUrl)}
                          alt="Team logo"
                          style={{ width: 96, height: 96, objectFit: "cover", borderRadius: 8, border: "1px solid #333" }}
                          onError={() => setLogoLoadFailed(true)}
                        />
                      ) : (
                        <div
                          className="d-flex justify-content-center align-items-center text-secondary"
                          style={{ width: 96, height: 96, borderRadius: 8, border: "1px dashed #444" }}
                        >
                          No logo
                        </div>
                      )}
                    </div>

                    <div className="flex-grow-1">
                      <input
                        className="form-control form-control-sm bg-dark text-light border-secondary"
                        type="file"
                        accept="image/png,image/jpeg,image/jpg,image/webp,image/gif"
                        onChange={(e) => {
                          const f = e.target.files?.[0] || null;
                          setLogoMsg("");
                          setLogoFile(f);
                          if (logoPreviewUrl) URL.revokeObjectURL(logoPreviewUrl);
                          setLogoPreviewUrl(f ? URL.createObjectURL(f) : "");
                        }}
                        disabled={logoUploading}
                      />
                      <div className="d-flex gap-2 mt-2">
                        <button
                          className="btn btn-outline-info btn-sm"
                          onClick={uploadTeamLogo}
                          disabled={!logoFile || logoUploading}
                        >
                          {logoUploading ? "Uploading…" : "Upload Logo"}
                        </button>
                        {logoFile && (
                          <button
                            className="btn btn-outline-secondary btn-sm"
                            onClick={() => {
                              setLogoFile(null);
                              if (logoPreviewUrl) URL.revokeObjectURL(logoPreviewUrl);
                              setLogoPreviewUrl("");
                              setLogoMsg("");
                            }}
                            disabled={logoUploading}
                          >
                            Clear
                          </button>
                        )}
                      </div>
                      {logoMsg && (
                        <div
                          className={`small mt-2 ${logoMsg.startsWith("✅")
                            ? "text-success"
                            : logoMsg.startsWith("⚠️")
                              ? "text-warning"
                              : "text-danger"
                            }`}
                        >
                          {logoMsg}
                        </div>
                      )}
                      <div className="text-secondary small mt-2">
                        Supported: PNG/JPG/WEBP/GIF (max ~8MB).
                      </div>
                    </div>
                  </div>
                </div>

                {/* TEAM STATUS */}
                <div className="mb-4 border-bottom border-secondary pb-3">
                  <h6 className="text-warning mb-2">🏁 Team Status</h6>
                  <div className="d-flex align-items-center gap-2">
                    <div className="form-check form-switch m-0">
                      <input
                        className="form-check-input"
                        type="checkbox"
                        checked={team?.status === "Active"}
                        onChange={async (e) => {
                          const nextStatus = e.target.checked ? "Active" : "Inactive";
                          try {
                            await axios.post(
                              `${urlBase}/api/team/toggle-status`,
                              { team_id: team.id, status: nextStatus },
                              { withCredentials: true }
                            );
                            await loadTeam();
                          } catch {
                            alert("Failed to update team status");
                          }
                        }}
                      />
                    </div>
                    <span className="small">
                      {team?.status === "Active"
                        ? "✅ Active / Match-Eligible"
                        : "⛔ Inactive / Hidden"}
                    </span>
                  </div>
                </div>

                {/* JOIN REQUEST TOGGLE */}
                <div className="mb-4 border-bottom border-secondary pb-3">
                  <h6 className="text-success mb-2">👥 Join Requests</h6>
                  <div className="d-flex align-items-center gap-2">
                    <div
                      className="form-check form-switch m-0"
                      style={joinDisabled ? { opacity: 0.5, pointerEvents: "none" } : {}}
                    >
                      <input
                        className="form-check-input"
                        type="checkbox"
                        checked={!!team?.join_allowed}
                        disabled={joinDisabled}
                        onChange={async (e) => {
                          if (joinDisabled) return;
                          const next = e.target.checked;
                          try {
                            await axios.post(
                              `${urlBase}/api/team/toggle-join`,
                              { team_id: team.id, allow: next },
                              { withCredentials: true }
                            );
                            await loadTeam();
                          } catch {
                            alert("Failed to update join settings");
                          }
                        }}
                      />
                    </div>
                    <span className="small">
                      {joinDisabled
                        ? "🔒 Roster is locked"
                        : team?.join_allowed
                          ? "✅ Allowed"
                          : "🚫 Disabled"}
                    </span>
                  </div>
                </div>

                {/* CHALLENGE TOGGLE */}
                <div>
                  <h6 className="text-info mb-2">🏆 Challenge Matches</h6>
                  <div className="d-flex align-items-center gap-2">
                    <div className="form-check form-switch m-0">
                      <input
                        className="form-check-input"
                        type="checkbox"
                        checked={!!allowChallenges}
                        disabled={team?.status !== "Active" || !globalChallengesEnabled || !isCaptain}
                        onChange={async (e) => {
                          const next = e.target.checked;

                          if (!globalChallengesEnabled) {
                            return alert("🚫 League mods have globally disabled challenge matches.");
                          }
                          if (team?.status !== "Active") {
                            return alert("🚫 Your team must be active to enable challenges.");
                          }
                          if (!isCaptain) {
                            return alert("⛔ Only the captain or co-captain can toggle this.");
                          }

                          try {
                            await axios.post(
                              `${urlBase}/api/team/toggle-challenges`,
                              { team_id: team.id, allow: next },
                              { withCredentials: true }
                            );
                            setAllowChallenges(next);
                          } catch {
                            alert("Failed to update challenge setting");
                          }
                        }}
                      />
                    </div>

                    <span className="small">
                      {!globalChallengesEnabled
                        ? "⚠️ Challenge matches disabled league-wide."
                        : allowChallenges
                          ? "✅ Accepting Challenges"
                          : "🚫 Challenges Disabled"}
                    </span>
                  </div>

                  {team?.status !== "Active" && (
                    <p className="text-warning small mt-2 mb-0">
                      ⚠️ Team is <b>Inactive</b>. Challenge matches disabled automatically.
                    </p>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ================= ROSTER ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-3">👥 Roster</h4>

        <ul className="list-group">
          {(roster ?? []).length > 0 ? (
            roster.map((m) => (
              <li
                key={m.id}
                className="list-group-item bg-dark text-light d-flex justify-content-between align-items-center mb-2 rounded"
                style={{ borderColor: "#333" }}
              >
                <div>
                  <strong className="text-info">{m.display_name || m.username || "Unknown"}</strong>{" "}
                  <span className="text-secondary small">{m.role || "-"}</span>

                  {m.on_cooldown && (
                    <span className="badge bg-warning text-dark ms-2">
                      ⏳ Cooldown
                    </span>
                  )}
                </div>

                {myRole === "Captain" && m.role !== "Captain" && (
                  <div className="d-flex gap-2 align-items-center">
                    <select
                      className="form-select form-select-sm bg-dark text-light"
                      style={{ width: 160 }}
                      defaultValue=""
                      onChange={async (e) => {
                        if (!e.target.value) return;
                        await handlePromote(m.id, e.target.value);
                        e.target.value = "";
                      }}
                    >
                      <option value="" disabled>
                        Promote…
                      </option>
                      <option value="Captain">Captain</option>
                      <option value="Co-Captain">Co-Captain</option>
                      <option value="Member">Member</option>
                    </select>

                    <button className="btn btn-danger btn-sm" onClick={() => handleKick(m.id)}>
                      Kick
                    </button>
                  </div>
                )}
              </li>
            ))
          ) : (
            <li className="list-group-item bg-dark text-light rounded" style={{ borderColor: "#333" }}>
              No members yet.
            </li>
          )}
        </ul>
      </div>

      {/* ================= JOIN & CHALLENGE REQUESTS ================= */}
      {(myRole === "Captain" || myRole === "Co-Captain") && (
        <div className="row g-4 mb-4">
          {/* JOIN REQUESTS */}
          <div className="col-12 col-lg-6">
            <div className="card bg-dark border-secondary p-4 shadow-sm h-100">
              <h4 className="mb-3">📥 Join Requests</h4>

              {requests.length === 0 ? (
                <p className="text-secondary mb-0">No join requests.</p>
              ) : (
                requests.map((req) => (
                  <div
                    key={req.id}
                    className="border rounded p-3 mb-3"
                    style={{ borderColor: "#444" }}
                  >
                    <strong className="text-info">
                      {req.display_name || req.username || "Unknown Player"}
                    </strong>

                    <div className="d-flex gap-2 mt-2">
                      <button
                        className="btn btn-success btn-sm"
                        onClick={() => handleDecision(req.id, "accept")}
                      >
                        Accept
                      </button>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={() => handleDecision(req.id, "deny")}
                      >
                        Deny
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>

          {/* CHALLENGE REQUESTS */}
          <div className="col-12 col-lg-6">
            <div className="card bg-dark border-secondary p-4 shadow-sm h-100">
              <h4 className="mb-3">⚔️ Challenge Requests</h4>

              {challengeRequests.length === 0 ? (
                <p className="text-secondary mb-0">No challenge requests.</p>
              ) : (
                challengeRequests.map((req) => (
                  <div
                    key={req.id}
                    className="border rounded p-3 mb-3"
                    style={{ borderColor: "#444" }}
                  >
                    <p className="mb-2">
                      <strong className="text-info">{req.requester_team_name}</strong>{" "}
                      challenged your team
                      <span className="text-secondary"> (Week {req.week})</span>
                    </p>

                    <div className="d-flex gap-2">
                      <button
                        className="btn btn-success btn-sm"
                        onClick={() => respondChallenge(req.id, true)}
                      >
                        Accept
                      </button>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={() => respondChallenge(req.id, false)}
                      >
                        Deny
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      )}

      {/* ================= ACTIVE MATCHES (Scheduled ONLY) ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-3">📅 Active Matches</h4>

        {(() => {
          const activeMatches = (matches ?? []).filter((m) => {
            if (!m) return false;
            const status = String(m.status ?? "").trim().toLowerCase();
            return status === "scheduled" || status === "pending schedule confirmation";
          });

          if (activeMatches.length === 0) {
            return <p className="text-secondary mb-0">No active matches.</p>;
          }

          return activeMatches.map((m) => (
            <div
              key={m.id}
              className="border rounded p-3 mb-3"
              style={{ borderColor: "#444" }}
            >
              <h6 className="mb-1">
                {m.match_code || `Match #${m.id}`}{" "}
                <span className="text-info">vs {m.opponent || "Unknown"}</span>
              </h6>

              <p className="small text-secondary mb-2">
                {m.date ? new Date(m.date).toLocaleString() : "Not scheduled yet"}
              </p>

              {(myRole === "Captain" || myRole === "Co-Captain") ? (
                <MatchCard
                  match={m}
                  team={team}
                  urlBase={urlBase}
                  loadTeam={loadTeam}
                  myRole={myRole}
                  arenaModeEnabled={arenaModeEnabled}
                />
              ) : (
                <p className="text-secondary small mb-0">Waiting for captains…</p>
              )}
            </div>
          ));
        })()}
      </div>

      {/* ================= PAST MATCHES (Card style like Active) ================= */}
      <div className="card bg-dark border-secondary p-4 mb-4 shadow-sm">
        <h4 className="mb-3">🏁 Past Matches</h4>

        {/* Season Filter */}
        <div className="d-flex justify-content-end mb-3">
          <select
            className="form-select form-select-sm bg-dark text-light"
            style={{ maxWidth: 200 }}
            value={selectedSeason}
            onChange={(e) => setSelectedSeason(e.target.value)}
          >
            {allSeasons.map((s) => (
              <option key={s}>{s}</option>
            ))}
          </select>
        </div>

        {(() => {
          const finishedStatuses = new Set([
            "finished",
            "completed",
            "forfeit",
            "double forfeit",
            "cancelled",
          ]);

          const currentSeasonNum = String(currentSeason ?? "")
            .replace("Season ", "")
            .trim()
            .toLowerCase();

          const pastMatches = (matches ?? [])
            .filter((m) => {
              if (!m) return false;

              const status = String(m.status ?? "").trim().toLowerCase();
              const isFinished = finishedStatuses.has(status);

              // Treat anything not "Scheduled" as past-ish, but keep your original idea:
              // "Past" = finished OR not current season
              let seasonNum = String(m.season ?? "").trim().toLowerCase();
              const prefix = String(m.match_code ?? "").split("-")[0];

              if (!seasonNum || seasonNum === "preseason") {
                seasonNum = /^\d+$/.test(prefix) ? prefix : "0";
              }
              if (seasonNum.startsWith("season ")) seasonNum = seasonNum.replace("season ", "");
              if (currentSeasonNum === "preseason") {
                // preseason in your UI corresponds to "0"
                return isFinished || seasonNum !== "0";
              }

              const currentNum = /^\d+$/.test(currentSeasonNum) ? currentSeasonNum : "0";
              return isFinished || seasonNum !== currentNum;
            })
            .filter((m) => {
              // Apply dropdown filter
              if (selectedSeason === "All") return true;

              if (selectedSeason === "Preseason") {
                let seasonNum = String(m.season ?? "").trim().toLowerCase();
                const prefix = String(m.match_code ?? "").split("-")[0];
                if (!seasonNum || seasonNum === "preseason") seasonNum = /^\d+$/.test(prefix) ? prefix : "0";
                if (seasonNum.startsWith("season ")) seasonNum = seasonNum.replace("season ", "");
                return seasonNum === "0";
              }

              const selectedNum = String(selectedSeason).replace("Season ", "").trim();
              let seasonNum = String(m.season ?? "").trim().toLowerCase();
              const prefix = String(m.match_code ?? "").split("-")[0];
              if (!seasonNum || seasonNum === "preseason") seasonNum = /^\d+$/.test(prefix) ? prefix : "0";
              if (seasonNum.startsWith("season ")) seasonNum = seasonNum.replace("season ", "");
              return seasonNum === selectedNum;
            });

          if (pastMatches.length === 0) {
            return <p className="text-secondary mb-0">No matches found for {selectedSeason}.</p>;
          }

          return pastMatches.map((m) => (
            <div
              key={m.id}
              className="border rounded p-3 mb-3"
              style={{ borderColor: "#444", cursor: "pointer" }}
              onClick={() => navigate(`/match/${m.id}`)}
            >
              <h6 className="mb-1">
                {m.match_code || `Match #${m.id}`}{" "}
                <span className="text-info">vs {m.opponent || "Unknown"}</span>
              </h6>

              <p className="small text-secondary mb-2">
                {m.date ? new Date(m.date).toLocaleString() : "Date unavailable"}
              </p>

              <div className="d-flex flex-wrap gap-2 align-items-center">
                <span
                  className={`fw-bold ${m.result === "Win"
                    ? "text-success"
                    : m.result === "Loss"
                      ? "text-danger"
                      : "text-warning"
                    }`}
                >
                  Result: {m.result || "Pending"}
                </span>

                <span className="text-secondary small">
                  Status: {m.status || "-"}
                </span>
              </div>
            </div>
          ));
        })()}
      </div>
    </div>
  );
}

