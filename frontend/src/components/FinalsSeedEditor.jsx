export default function FinalsSeedEditor({ teams, setTeams, onSave }) {
    const MAX = 10;

    const updateSeed = (teamId, newSeed) => {
        const updated = teams.map((t) =>
            t.team_id === teamId ? { ...t, seed: Number(newSeed) } : t
        );
        setTeams(updated);
    };

    const sorted = [...teams].sort((a, b) => (a.seed ?? 99) - (b.seed ?? 99));

    return (
        <div className="p-2 rounded mb-3" style={{ backgroundColor: "#111", border: "1px solid #333" }}>
            <h6 className="text-light mb-2">🎯 Assign Seeds (1–{MAX})</h6>

            <ul className="list-group">
                {sorted.map((t) => (
                    <li key={t.team_id} className="list-group-item bg-dark text-light d-flex justify-content-between">
                        <strong>{t.name}</strong>
                        <select
                            className="form-select bg-black text-light"
                            style={{ width: 80 }}
                            value={t.seed || ""}
                            onChange={(e) => updateSeed(t.team_id, e.target.value)}
                        >
                            <option value="">—</option>
                            {Array.from({ length: MAX }, (_, i) => i + 1).map((n) =>
                                <option key={n} value={n}>{n}</option>
                            )}
                        </select>
                    </li>
                ))}
            </ul>

            <button
                className="btn btn-outline-success btn-sm mt-3"
                onClick={() => onSave([...teams])}
            >
                💾 Save Seeds
            </button>
        </div>
    );
}
