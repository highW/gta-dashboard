import { useState } from "react";
import { createWarrant, mockPlayers } from "../services/mockData";

export default function NewWarrantForm({ onCreated }: { onCreated: () => void }) {
    const [playerId, setPlayerId] = useState(mockPlayers[0]?.id ?? 1);
    const [reason, setReason] = useState("");
    const [issuedBy, setIssuedBy] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (!reason.trim() || !issuedBy.trim()) return;

        setSubmitting(true);
        setError(null);

        createWarrant({
            playerId,
            reason,
            issuedBy,
            dateIssued: new Date().toISOString().split("T")[0],
        })
            .then(() => {
                setReason("");
                setIssuedBy("");
                onCreated();
            })
            .catch((err) => setError(err.message))
            .finally(() => setSubmitting(false));
    };

    return (
        <form
            onSubmit={handleSubmit}
            className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-lg p-4 space-y-3"
        >
            <h3 className="text-sm font-bold uppercase text-[var(--text-muted)]">Issue New Warrant</h3>

            <div className="flex flex-wrap gap-3">
                <select
                    value={playerId}
                    onChange={(e) => setPlayerId(Number(e.target.value))}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm"
                >
                    {mockPlayers.map((p) => (
                        <option key={p.id} value={p.id}>
                            {p.name}
                        </option>
                    ))}
                </select>

                <input
                    type="text"
                    placeholder="Reason"
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm flex-1 min-w-[160px]"
                />

                <input
                    type="text"
                    placeholder="Issued by"
                    value={issuedBy}
                    onChange={(e) => setIssuedBy(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm w-40"
                />

                <button
                    type="submit"
                    disabled={submitting}
                    className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-md"
                >
                    {submitting ? "Issuing..." : "Issue Warrant"}
                </button>
            </div>

            {error && <p className="text-red-400 text-sm">{error}</p>}
        </form>
    );
}
