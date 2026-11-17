import { useState } from "react";
import { apiErrorMessage, createWarrant } from "../services/mockData";
import { useAuth } from "../context/AuthContext";
import PlayerSelect from "./PlayerSelect";

/**
 * A judge issues a warrant that is active at once. Police, supervisors and
 * prosecutors request one instead, and it waits for a judge. The server
 * decides which happens; this only words the form to match.
 */
export default function NewWarrantForm({ onCreated }: { onCreated: () => void }) {
    const { can } = useAuth();
    const issues = can("warrants.issue");

    const [playerId, setPlayerId] = useState<number | null>(null);
    const [reason, setReason] = useState("");
    const [issuedBy, setIssuedBy] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [sent, setSent] = useState(false);

    const handleSubmit = (e: React.FormEvent) => {
        e.preventDefault();
        if (playerId === null || !reason.trim() || !issuedBy.trim()) return;

        setSubmitting(true);
        setError(null);
        setSent(false);

        createWarrant({
            playerId,
            reason,
            issuedBy,
            dateIssued: new Date().toISOString().split("T")[0],
        })
            .then(() => {
                setReason("");
                setIssuedBy("");
                setSent(!issues);
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
            <h3 className="text-sm font-bold uppercase text-[var(--text-muted)]">
                {issues ? "Issue New Warrant" : "Request a Warrant"}
            </h3>

            <div className="flex flex-wrap gap-3">
                <PlayerSelect value={playerId} onChange={setPlayerId} />

                <input
                    type="text"
                    placeholder="Reason"
                    aria-label="Reason"
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm flex-1 min-w-[160px]"
                />

                <input
                    type="text"
                    placeholder={issues ? "Issued by" : "Requested by"}
                    aria-label={issues ? "Issued by" : "Requested by"}
                    value={issuedBy}
                    onChange={(e) => setIssuedBy(e.target.value)}
                    className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm w-40"
                />

                <button
                    type="submit"
                    disabled={submitting || playerId === null}
                    className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-md"
                >
                    {submitting
                        ? issues ? "Issuing..." : "Sending..."
                        : issues ? "Issue Warrant" : "Send to a Judge"}
                </button>
            </div>

            {!issues && !sent && (
                <p className="text-xs text-[var(--text-muted)]">
                    A judge reviews the request before the warrant becomes active.
                </p>
            )}
            {sent && <p className="text-xs text-amber-400">Request sent. It shows as REQUESTED until a judge decides.</p>}
            {error && <p className="text-red-400 text-sm">{error}</p>}
        </form>
    );
}
