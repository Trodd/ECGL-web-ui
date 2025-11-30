export default function FinalsBracket({ bracket, onWinnerSelect }) {
    if (!bracket || bracket.length === 0)
        return <p className="text-light">No bracket generated.</p>;

    const rounds = {};

    for (const match of bracket) {
        if (!rounds[match.bracket_round]) rounds[match.bracket_round] = [];
        rounds[match.bracket_round].push(match);
    }

    return (
        <div>
            {Object.keys(rounds).sort().map(round => (
                <div key={round} className="mb-4">
                    <h5 className="text-warning">Round {round}</h5>

                    {rounds[round].map(match => (
                        <div
                            key={match.id}
                            className="p-2 mb-2 bg-dark text-light border border-secondary rounded"
                        >
                            <strong>
                                {match.team_a_name} vs {match.team_b_name}
                            </strong>

                            <div className="mt-2 d-flex gap-2">
                                <button
                                    className="btn btn-success btn-sm"
                                    onClick={() =>
                                        onWinnerSelect({
                                            match_id: match.id,
                                            winner: match.team_a_id,
                                        })
                                    }
                                >
                                    {match.team_a_name} wins
                                </button>

                                <button
                                    className="btn btn-primary btn-sm"
                                    onClick={() =>
                                        onWinnerSelect({
                                            match_id: match.id,
                                            winner: match.team_b_id,
                                        })
                                    }
                                >
                                    {match.team_b_name} wins
                                </button>
                            </div>
                        </div>
                    ))}
                </div>
            ))}
        </div>
    );
}
