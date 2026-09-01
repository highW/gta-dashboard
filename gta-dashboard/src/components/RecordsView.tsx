import { useState, useEffect } from "react";
import { clearWarrant, fetchArrests, fetchWarrants, updateArrestStatus, mockPlayers } from "../services/mockData";
import type { ArrestRecord, Warrant } from "../services/mockData";
import NewWarrantForm from "./NewWarrantForm";
import NewArrestForm from "./NewArrestForm";
import { useAuth } from "../context/AuthContext";

function playerName(playerId: number): string {
    return mockPlayers.find((p) => p.id === playerId)?.name ?? "Unknown";
}

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
    const [search, setSearch] = useState("");
    const { role } = useAuth();

    const loadRecords = () => {
        setLoading(true);
        setError(null);
        Promise.all([fetchArrests(), fetchWarrants()])
            .then(([arrestsData, warrantsData]) => {
                setArrests(arrestsData);
                setWarrants(warrantsData);
            })
            .catch((err) => setError(err.message))
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        loadRecords();
    }, []);

    const filteredArrests = arrests.filter((a) =>
        playerName(a.playerId).toLowerCase().includes(search.toLowerCase())
    );
    const filteredWarrants = warrants.filter((w) =>
        playerName(w.playerId).toLowerCase().includes(search.toLowerCase())
    );

    const activeWarrantCount = warrants.filter((w) => w.active).length;

    if (loading) {
        return <div className="text-[var(--text-muted)] py-12 text-center">Loading records...</div>;
    }

    if (error) {
        return (
            <div className="text-center py-12 border border-red-900/50 bg-red-950/20 rounded-lg space-y-3">
                <p className="text-red-400">{error}</p>
                <button
                    onClick={loadRecords}
                    className="px-4 py-2 text-sm bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)] rounded-md border border-[var(--border-color)]"
                >
                    Retry
                </button>
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <input
                type="text"
                placeholder="Search by player name..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm w-64 focus:outline-none focus:border-gray-500"
            />

            {/* Active warrants */}
            <div>
                {role == "admin" && <NewWarrantForm onCreated = {loadRecords}/>}
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
                            {filteredWarrants.map((w) => (
                                <tr key={w.id} className="border-t border-[var(--border-color)]">
                                    <td className="px-4 py-3 font-medium">{playerName(w.playerId)}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{w.reason}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{w.issuedBy}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{w.dateIssued}</td>
                                    <td className="px-4 py-3">
                                        <span
                                            className={`px-2 py-1 rounded text-xs font-medium ${w.active ? "bg-red-500/20 text-red-400" : "bg-gray-500/20 text-gray-400"
                                                }`}
                                        >
                                            {w.active ? "ACTIVE" : "CLEARED"}
                                        </span>
                                        {role == "admin" && w.active && (<button
                                            onClick={()=>clearWarrant(w.id).then(loadRecords)}
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
                {role == "admin" && <NewArrestForm onCreated={loadRecords}/>}
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
                            {filteredArrests.map((a) => (
                                <tr key={a.id} className="border-t border-[var(--border-color)]">
                                    <td className="px-4 py-3 font-medium">{playerName(a.playerId)}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.charge}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.officer}</td>
                                    <td className="px-4 py-3 text-[var(--text-secondary)]">{a.date}</td>
                                    <td className="px-4 py-3">
                                        {role === "admin" ? (
                                            <select
                                                value={a.status}
                                                onChange={(e) => updateArrestStatus(a.id, e.target.value as ArrestRecord["status"]).then(loadRecords)}
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
