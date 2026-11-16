import { useState, useEffect, useRef, useCallback } from "react";
import { clearWarrant, fetchArrests, fetchWarrants, updateArrestStatus } from "../services/mockData";
import type { ArrestRecord, Warrant } from "../services/mockData";
import NewWarrantForm from "./NewWarrantForm";
import NewArrestForm from "./NewArrestForm";
import { useAuth } from "../context/AuthContext";

// How long to wait after the last keystroke before asking the server.
const SEARCH_DELAY_MS = 300;

const arrestStatusColor: Record<ArrestRecord["status"], string> = {
    convicted: "bg-red-500",
    released: "bg-gray-500",
    pending: "bg-yellow-500",
};

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

    const activeWarrantCount = warrants.filter((w) => w.active).length;

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
                {can("warrants.issue") && <NewWarrantForm onCreated = {reload}/>}
                <h2 className="text-lg font-bold mb-3">
                    Active Warrants
                    <span className="ml-2 text-sm font-normal text-red-400">
                        ({activeWarrantCount} outstanding)
                    </span>
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
                                        <span
                                            className={`px-2 py-1 rounded text-xs font-medium ${w.active ? "bg-red-500/20 text-red-400" : "bg-gray-500/20 text-gray-400"
                                                }`}
                                        >
                                            {w.active ? "ACTIVE" : "CLEARED"}
                                        </span>
                                        {can("warrants.clear") && w.active && (<button
                                            onClick={()=>clearWarrant(w.id).then(reload)}
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
                <h2 className="text-lg font-bold mb-3">Arrest History</h2>
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
                                <tr key={a.id} className="border-t border-[var(--border-color)]">
                                    <td className="px-4 py-3 font-medium">{a.playerName || "Unknown"}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.charge}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.officer}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.date || "—"}</td>
                                    <td className="px-4 py-3">
                                        {can("arrests.update") ? (
                                            <select
                                                value={a.status}
                                                onChange={(e) => updateArrestStatus(a.id, e.target.value as ArrestRecord["status"]).then(reload)}
                                                className="bg-[var(--bg-page)] border border-[var(--border-color)] rounded px-2 py-1 text-xs"
                                            >
                                                <option value="pending">pending</option>
                                                <option value="convicted">convicted</option>
                                                <option value="released">released</option>
                                            </select>
                                        ) : (
                                            <span className="inline-flex items-center gap-2">
                                                <span className={`w-2 h-2 rounded-full ${arrestStatusColor[a.status]}`} />
                                                {a.status}
                                            </span>
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
