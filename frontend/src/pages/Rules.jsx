import RulesContent from "../components/Rules";

export default function RulesPage() {
    return (
        <div>
            <h2 className="mb-4">📜 League Rules & Format</h2>
            <div
                className="p-3 rounded"
                style={{ background: "#121212", border: "1px solid #2a2a2a" }}
            >
                <RulesContent />
            </div>
        </div>
    );
}
