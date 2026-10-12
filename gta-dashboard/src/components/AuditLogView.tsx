import { useState, useEffect } from "react";
import { fetchAuditLog } from "../services/mockData";
import type { AuditEntry } from "../services/mockData";

const methodColor: Record<string, string> = {
    POST: "bg-green-500/20 text-green-400",
    PATCH: "bg-yellow-500/20 text-yellow-400",
    DELETE: "bg-red-500/20 text-red-400",
};

export default function AuditLogView() {
    const [entries, setEntries] = useState<AuditEntry[]>([]);
    const [cursor, setCursor] = useState<number | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadingMore, setLoadingMore] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const loadLog = () => {
        setLoading(true);
        setError(null);
        fetchAuditLog()
            .then((res) => {
                setEntries(res.entries);
                setCursor(res.next_cursor);
            })
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    };

    const loadMore = () => {
        if (cursor === null) return;
        setLoadingMore(true);
        fetchAuditLog(cursor)
            .then((res) => {
                setEntries((prev) => [...prev, ...res.entries]);
                setCursor(res.next_cursor);
            })
            .catch((err) => setError(err.message))
            .finally(() => setLoadingMore(false));
    };

    useEffect(() => {
        loadLog();
    }, []);

    if (loading) {
        return <div className="text-[var(--text-muted)] py-12 text-center">Loading audit log...</div>;
    }

    if (error) {
        return (
            <div className="text-center py-12 border border-red-900/50 bg-red-950/20 rounded-lg space-y-3">
                <p className="text-red-400">{error}</p>
                <button
                    onClick={loadLog}
                    className="px-4 py-2 text-sm bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)] rounded-md border border-[var(--border-color)]"
                >
                    Retry
                </button>
            </div>
        );
    }

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between">
                <h2 className="text-lg font-bold">Audit Log</h2>
                <button
                    onClick={loadLog}
                    className="px-3 py-1.5 text-xs rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)]"
                >
                    Refresh
                </button>
            </div>

            {entries.length === 0 ? (
                <div className="text-center text-[var(--text-muted)] py-12 border border-[var(--border-color)] rounded-lg">
                    No actions logged yet.
                </div>
            ) : (
                <>
                    <div className="rounded-lg border border-[var(--border-color)] overflow-hidden">
                        <table className="w-full text-left text-sm">
                            <thead className="bg-[var(--bg-surface)] text-[var(--text-muted)] uppercase text-xs">
                                <tr>
                                    <th className="px-4 py-3">Timestamp</th>
                                    <th className="px-4 py-3">Actor</th>
                                    <th className="px-4 py-3">Role</th>
                                    <th className="px-4 py-3">Method</th>
                                    <th className="px-4 py-3">Path</th>
                                    <th className="px-4 py-3">IP</th>
                                </tr>
                            </thead>
                            <tbody>
                                {entries.map((e) => (
                                    <tr key={e.id} className="border-t border-[var(--border-color)]">
                                        <td className="px-4 py-3 text-[var(--text-secondary)]">
                                            {new Date(e.timestamp).toLocaleString()}
                                        </td>
                                        <td className="px-4 py-3">{e.actor}</td>
                                        <td className="px-4 py-3 font-medium">{e.role}</td>
                                        <td className="px-4 py-3">
                                            <span className={`px-2 py-1 rounded text-xs font-medium ${methodColor[e.method] ?? "bg-gray-500/20 text-gray-400"}`}>
                                                {e.method}
                                            </span>
                                        </td>
                                        <td className="px-4 py-3 text-[var(--text-secondary)] font-mono text-xs">{e.path}</td>
                                        <td className="px-4 py-3 text-[var(--text-secondary)] font-mono text-xs">{e.ip}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>

                    {cursor !== null && (
                        <div className="flex justify-center">
                            <button
                                onClick={loadMore}
                                disabled={loadingMore}
                                className="px-4 py-2 text-sm rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
                            >
                                {loadingMore ? "Loading..." : "Load more"}
                            </button>
                        </div>
                    )}
                </>
            )}
        </div>
    );
}