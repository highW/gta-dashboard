import { useState } from "react";
import { apiErrorMessage, createArrest } from "../services/mockData";
import type { ArrestRecord } from "../services/mockData";
import { useAuth } from "../context/AuthContext";
import PlayerSelect from "./PlayerSelect";

type Decidable = Exclude<ArrestRecord["status"], "voided">;

export default function NewArrestForm({ onCreated }: { onCreated: () => void }) {
    const { can } = useAuth();
    // Logging an arrest is police work; deciding the case is the prosecutor's.
    // Only someone who may decide sees the status choice (the server enforces it).
    const canDecide = can("arrests.decide");

    const [playerId, setPlayerId] = useState<number | null>(null);
    const [charge, setCharge] = useState("");
    const [officer, setOfficer] = useState("");
    const [status, setStatus] = useState<Decidable>("pending");
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (playerId === null || !charge.trim() || !officer.trim()) return;

        setSubmitting(true);
        setError(null);

        createArrest({
            playerId,
            charge,
            officer,
            status: canDecide ? status : "pending",
            date: new Date().toISOString().split("T")[0],
        })
            .then(() => {
                setCharge("");
                setOfficer("");
                setStatus("pending");
                onCreated();
            })
            .catch((err) => setError(apiErrorMessage(err)))
            .finally(() => setSubmitting(false));
    };

    return (
        <form
            onSubmit={handleSubmit}
            className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-lg p-4 space-y-3"
        >
            <h3 className="text-sm font-bold uppercase text-[var(--text-muted)]">Log New Arrest</h3>

            <div className="flex flex-wrap gap-3">
                <PlayerSelect value={playerId} onChange={setPlayerId} />

                <input
                    type="text"
                    placeholder="Charge"
                    aria-label="Charge"
                    value={charge}
                    onChange={(e) => setCharge(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm flex-1 min-w-[160px]"
                />

                <input
                    type="text"
                    placeholder="Officer"
                    aria-label="Officer"
                    value={officer}
                    onChange={(e) => setOfficer(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm w-40"
                />

                {canDecide && (
                    <select
                        aria-label="Status"
                        value={status}
                        onChange={(e) => setStatus(e.target.value as Decidable)}
                        className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm"
                    >
                        <option value="pending">Pending</option>
                        <option value="convicted">Convicted</option>
                        <option value="released">Released</option>
                    </select>
                )}

                <button
                    type="submit"
                    disabled={submitting || playerId === null}
                    className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-md"
                >
                    {submitting ? "Logging..." : "Log Arrest"}
                </button>
            </div>

            {!canDecide && (
                <p className="text-xs text-[var(--text-muted)]">
                    New arrests start as pending. A prosecutor decides the case.
                </p>
            )}
            {error && <p className="text-red-400 text-sm">{error}</p>}
        </form>
    );
}
