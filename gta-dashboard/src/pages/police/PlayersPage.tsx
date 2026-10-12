import { useState, useMemo, useEffect } from "react";
import PlayerList from "../../components/PlayerList";
import PlayerListSkeleton from "../../components/PlayerListSkeleton";
import PlayerDetailPanel from "../../components/PlayerDetailPanel";
import StatsBar from "../../components/StatsBar";
import { fetchPlayers, fetchWarrants } from "../../services/mockData";
import type { Player, Warrant } from "../../services/mockData";

type StatusFilter = "all" | "online" | "idle" | "offline";

const field =
  "bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm focus:outline-none focus:border-gray-500";

export default function PlayersPage() {
  const [players, setPlayers] = useState<Player[]>([]);
  const [warrants, setWarrants] = useState<Warrant[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [sortBy, setSortBy] = useState<"name" | "ping">("name");
  const [selectedPlayer, setSelectedPlayer] = useState<Player | null>(null);

  const loadPlayers = () => {
    setLoading(true);
    setError(null);
    fetchPlayers()
      .then(setPlayers)
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    loadPlayers();
    // silent background refresh so the UI doesn't flash
    const interval = setInterval(() => {
      fetchPlayers().then(setPlayers).catch(() => {});
    }, 5000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    fetchWarrants().then(setWarrants).catch(() => {});
  }, []);

  const filteredPlayers = useMemo(() => {
    let result = players.filter((p) =>
      p.name.toLowerCase().includes(search.toLowerCase())
    );
    if (statusFilter !== "all") {
      result = result.filter((p) => p.status === statusFilter);
    }
    return [...result].sort((a, b) =>
      sortBy === "name" ? a.name.localeCompare(b.name) : a.ping - b.ping
    );
  }, [players, search, statusFilter, sortBy]);

  const activeWarrantPlayerIds = useMemo(
    () => new Set(warrants.filter((w) => w.active).map((w) => w.playerId)),
    [warrants]
  );

  return (
    <div className="space-y-4">
      {!loading && !error && <StatsBar players={players} />}

      <div className="flex flex-wrap items-center gap-3">
        <input
          type="text"
          placeholder="Search players"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className={`${field} w-64`}
        />
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
          className={field}
        >
          <option value="all">All statuses</option>
          <option value="online">Online</option>
          <option value="idle">Idle</option>
          <option value="offline">Offline</option>
        </select>
        <select
          value={sortBy}
          onChange={(e) => setSortBy(e.target.value as "name" | "ping")}
          className={field}
        >
          <option value="name">Sort by name</option>
          <option value="ping">Sort by ping</option>
        </select>
        {!loading && !error && (
          <span className="text-sm text-[var(--text-muted)] ml-auto">
            {filteredPlayers.length} of {players.length} players
          </span>
        )}
      </div>

      {loading && <PlayerListSkeleton />}

      {!loading && error && (
        <div className="text-center py-12 border border-red-900/50 bg-red-950/20 rounded-lg space-y-3">
          <p className="text-red-400">{error}</p>
          <button
            onClick={loadPlayers}
            className="px-4 py-2 text-sm bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)] rounded-md border border-[var(--border-color)]"
          >
            Retry
          </button>
        </div>
      )}

      {!loading && !error && (
        <PlayerList
          players={filteredPlayers}
          onSelect={setSelectedPlayer}
          activeWarrantPlayerIds={activeWarrantPlayerIds}
        />
      )}

      <PlayerDetailPanel
        player={selectedPlayer}
        onClose={() => setSelectedPlayer(null)}
        onDeleted={loadPlayers}
      />
    </div>
  );
}
