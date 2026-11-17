import { useState, useEffect, useRef, useCallback } from "react";
import { actOnWarrant, apiErrorMessage, fetchArrests, fetchWarrants, updateArrestStatus } from "../services/mockData";
import type { ArrestRecord, Warrant, WarrantAction } from "../services/mockData";
import NewWarrantForm from "./NewWarrantForm";
import NewArrestForm from "./NewArrestForm";
import { useAuth } from "../context/AuthContext";
import { ArrestStatus, WarrantBadge } from "./StatusBadge";

// How long to wait after the last keystroke before asking the server.
const SEARCH_DELAY_MS = 300;

export default function RecordsView() {
    const [arrests, setArrests] = useState<ArrestRecord[]>([]);
    const [warrants, setWarrants] = useState<Warrant[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    // The search runs on the server, over every record, not only the page
    // loaded here. Three values keep typing smooth:
    const [search, setSearch] = useState(""); // what is in the box right now
    const [query, setQuery] = useState(""); // what the server is being asked for
    const [shownQuery, setShownQuery] = useState(""); // what the tables currently show
    const { can } = useAuth();

    // Every request gets a number and only the newest may update the page,
    // so a slow answer to an old search can't overwrite a newer one.
    const latestRequest = useRef(0);

    const loadRecords = useCallback((q: string) => {
        const request = ++latestRequest.current;
        return Promise.all([fetchArrests(q), fetchWarrants(q)])
            .then(([arrestsData, warrantsData]) => {
                if (request !== latestRequest.current) return;
                setArrests(arrestsData);
                setWarrants(warrantsData);
                setShownQuery(q);
                setError(null);
            })
            .catch((err) => {
                if (request === latestRequest.current) setError(err.message);
            })
            .finally(() => {
                if (request === latestRequest.current) setLoading(false);
            });
    }, []);

    // Ask the server once typing pauses, not on every keystroke.
    useEffect(() => {
        const timer = setTimeout(() => setQuery(search.trim()), SEARCH_DELAY_MS);
        return () => clearTimeout(timer);
    }, [search]);

    useEffect(() => {
        loadRecords(query);
    }, [query, loadRecords]);

    // After a change (new record, cleared warrant...) show the same search again.
    const reload = () => loadRecords(query);

    const retry = () => {
        setLoading(true);
        setError(null);
        loadRecords(query);
    };

    const searching = search.trim() !== shownQuery;
    const nothingFound = shownQuery !== "" && arrests.length === 0 && warrants.length === 0;

    const activeWarrantCount = warrants.filter((w) => w.status === "active").length;
    const requestedCount = warrants.filter((w) => w.status === "requested").length;

    // A failed action (e.g. another judge already decided) shows the server's reason.
    const [actionError, setActionError] = useState<string | null>(null);
    const act = (p: Promise<void>) => {
        setActionError(null);
        return p.then(reload).catch((err) => {
            setActionError(apiErrorMessage(err));
            reload();
        });
    };
    const warrantAct = (w: Warrant, action: WarrantAction) => act(actOnWarrant(w.id, action));
    const voidArrest = (a: ArrestRecord) => {
        if (window.confirm(`Void the arrest of ${a.playerName || "this player"} for "${a.charge}"? It stays on record as voided and can't be changed again.`)) {
            act(updateArrestStatus(a.id, "voided"));
        }
    };

    if (loading) {
        return <div className="text-[var(--text-muted)] py-12 text-center">Loading records...</div>;
    }

    if (error) {
        return (
            <div className="text-center py-12 border border-red-900/50 bg-red-950/20 rounded-lg space-y-3">
                <p className="text-red-400">{error}</p>
                <button
                    onClick={retry}
                    className="px-4 py-2 text-sm bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)] rounded-md border border-[var(--border-color)]"
                >
                    Retry
                </button>
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <div className="flex flex-wrap items-center gap-3">
                <input
                    type="text"
                    placeholder="Search name, charge, officer"
                    aria-label="Search records by player name, charge, reason, officer or judge"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm w-64 focus:outline-none focus:border-gray-500"
                />
                {searching && <span className="text-sm text-[var(--text-muted)]">Searching...</span>}
                {!searching && nothingFound && (
                    <span className="text-sm text-[var(--text-muted)]">No records match "{shownQuery}".</span>
                )}
            </div>

            {/* Active warrants */}
            <div>
                {(can("warrants.issue") || can("warrants.request")) && <NewWarrantForm onCreated = {reload}/>}
                {actionError && <p role="alert" className="text-red-400 text-sm mt-3">{actionError}</p>}
                <h2 className="text-lg font-bold mb-3 mt-6">
                    Warrants
                    <span className="ml-2 text-sm font-normal text-red-400">
                        ({activeWarrantCount} active)
                    </span>
                    {requestedCount > 0 && (
                        <span className="ml-2 text-sm font-normal text-amber-400">
                            {requestedCount} waiting for a judge
                        </span>
                    )}
                </h2>
                <div className="rounded-lg border border-[var(--border-color)] overflow-hidden">
                    <table className="w-full text-left text-sm">
                        <thead className="bg-[var(--bg-surface)] text-[var(--text-muted)] uppercase text-xs">
                            <tr>
                                <th className="px-4 py-3">Player</th>
                                <th className="px-4 py-3">Reason</th>
                                <th className="px-4 py-3">Issued By</th>
                                <th className="px-4 py-3">Date</th>
                                <th className="px-4 py-3">Status</th>
                            </tr>
                        </thead>
                        <tbody>
                            {warrants.map((w) => (
                                <tr key={w.id} className="border-t border-[var(--border-color)]">
                                    <td className="px-4 py-3 font-medium">{w.playerName || "Unknown"}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{w.reason}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{w.issuedBy}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{w.dateIssued || "—"}</td>
                                    <td className="px-4 py-3">
                                        <span title={w.decidedBy ? `${w.status === "denied" ? "Denied" : "Approved"} by ${w.decidedBy}${w.decisionNote ? `: ${w.decisionNote}` : ""}` : undefined}>
                                            <WarrantBadge status={w.status} />
                                        </span>
                                        {can("warrants.decide") && w.status === "requested" && (
                                            <>
                                                <button onClick={() => warrantAct(w, "approve")}
                                                    className="ml-2 text-xs text-green-400 hover:text-green-300 underline">
                                                    Approve
                                                </button>
                                                <button onClick={() => warrantAct(w, "deny")}
                                                    className="ml-2 text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)] underline">
                                                    Deny
                                                </button>
                                            </>
                                        )}
                                        {can("warrants.clear") && w.status === "active" && (
                                            <button onClick={() => warrantAct(w, "clear")}
                                                className="ml-2 text-xs text-[var(--text-muted)] hover:text-[var(--text-primary)] underline">
                                                Clear
                                            </button>
                                        )}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </div>

            {/* Arrest history */}
            <div>
                {can("arrests.create") && <NewArrestForm onCreated={reload}/>}
                <h2 className="text-lg font-bold mb-3 mt-6">Arrest History</h2>
                <div className="rounded-lg border border-[var(--border-color)] overflow-hidden">
                    <table className="w-full text-left text-sm">
                        <thead className="bg-[var(--bg-surface)] text-[var(--text-muted)] uppercase text-xs">
                            <tr>
                                <th className="px-4 py-3">Player</th>
                                <th className="px-4 py-3">Charge</th>
                                <th className="px-4 py-3">Officer</th>
                                <th className="px-4 py-3">Date</th>
                                <th className="px-4 py-3">Status</th>
                            </tr>
                        </thead>
                        <tbody>
                            {arrests.map((a) => (
                                <tr key={a.id} className={`border-t border-[var(--border-color)] ${a.status === "voided" ? "opacity-50 line-through decoration-[var(--text-muted)]" : ""}`}>
                                    <td className="px-4 py-3 font-medium">{a.playerName || "Unknown"}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.charge}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.officer}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.date || "—"}</td>
                                    <td className="px-4 py-3">
                                        {can("arrests.decide") && a.status !== "voided" ? (
                                            <select
                                                aria-label={`Status of ${a.playerName}'s arrest`}
                                                value={a.status}
                                                onChange={(e) => act(updateArrestStatus(a.id, e.target.value as ArrestRecord["status"]))}
                                                className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded px-2 py-1 text-xs"
                                            >
                                                <option value="pending">pending</option>
                                                <option value="convicted">convicted</option>
                                                <option value="released">released</option>
                                            </select>
                                        ) : (
                                            <ArrestStatus status={a.status} />
                                        )}
                                        {can("arrests.void") && a.status !== "voided" && (
                                            <button onClick={() => voidArrest(a)}
                                                className="ml-3 text-xs text-[var(--text-muted)] hover:text-red-400 underline">
                                                Void
                                            </button>
                                        )}
                                    </td>


                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    );
}
