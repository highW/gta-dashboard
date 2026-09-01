import { useState } from "react";
import { createArrest, mockPlayers } from "../services/mockData";
import type { ArrestRecord } from "../services/mockData";

export default function NewArrestForm({ onCreated }: { onCreated: () => void }) {
    const [playerId, setPlayerId] = useState(mockPlayers[0]?.id ?? 1);
    const [charge, setCharge] = useState("");
    const [officer, setOfficer] = useState("");
    const [status, setStatus] = useState<ArrestRecord["status"]>("pending");
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (!charge.trim() || !officer.trim()) return;

        setSubmitting(true);
        setError(null);

        createArrest({
            playerId,
            charge,
            officer,
            status,
            date: new Date().toISOString().split("T")[0],
        })
            .then(() => {
                setCharge("");
                setOfficer("");
                setStatus("pending");
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
            <h3 className="text-sm font-bold uppercase text-[var(--text-muted)]">Log New Arrest</h3>

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
                    placeholder="Charge"
                    value={charge}
                    onChange={(e) => setCharge(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm flex-1 min-w-[160px]"
                />

                <input
                    type="text"
                    placeholder="Officer"
                    value={officer}
                    onChange={(e) => setOfficer(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm w-40"
                />

                <select
                    value={status}
                    onChange={(e) => setStatus(e.target.value as ArrestRecord["status"])}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm"
                >
                    <option value="pending">Pending</option>
                    <option value="convicted">Convicted</option>
                    <option value="released">Released</option>
                </select>

                <button
                    type="submit"
                    disabled={submitting}
                    className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-md"
                >
                    {submitting ? "Logging..." : "Log Arrest"}
                </button>
            </div>

            {error && <p className="text-red-400 text-sm">{error}</p>}
        </form>
    );
}
