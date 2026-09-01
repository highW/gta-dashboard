import { useState, useMemo, useEffect } from "react";
import PlayerList from "../components/PlayerList";
import PlayerListSkeleton from "../components/PlayerListSkeleton";
import PlayerDetailPanel from "../components/PlayerDetailPanel";
import StatsBar from "../components/StatsBar";
import { fetchMockPlayers } from "../services/mockData";
import type { Player } from "../services/mockData";
import RecordsView from "../components/RecordsView";
import {fetchPlayers} from "../services/mockData";
import { fetchWarrants } from "../services/mockData";
import type { Warrant } from "../services/mockData";
import {useAuth} from "../context/AuthContext";

type StatusFilter = "all" | "online" | "idle" | "offline";
type Theme = "dark" | "light";

export default function Dashboard() {
  const { role, logout } = useAuth();
  const [players, setPlayers] = useState<Player[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [sortBy, setSortBy] = useState<"name" | "ping">("name");
  const [selectedPlayer, setSelectedPlayer] = useState<Player | null>(null);
  const [theme, setTheme] = useState<Theme>("dark");
  const [activeTab, setActiveTab] = useState<"players" | "records">("players");
  const [warrants, setWarrants] = useState<Warrant[]>([]);

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

    const interval = setInterval(()=>{
      fetchPlayers()
      .then(setPlayers)
      .catch(()=>{}); //silent refrersh on background without UI updating constantly
    }, 5000);
    return ()=>clearInterval(interval);
  }, []);

  useEffect(()=>{
    fetchWarrants().then(setWarrants).catch(()=>{});
  }, []);

  const filteredPlayers = useMemo(() => {
    let result = players.filter((p) =>
      p.name.toLowerCase().includes(search.toLowerCase())
    );

    if (statusFilter !== "all") {
      result = result.filter((p) => p.status === statusFilter);
    }

    result = [...result].sort((a, b) =>
      sortBy === "name" ? a.name.localeCompare(b.name) : a.ping - b.ping
    );

    return result;
  }, [players, search, statusFilter, sortBy]);

  return (
    <>
      <div
        className={`min-h-screen bg-[var(--bg-page)] text-[var(--text-primary)] ${
          theme === "light" ? "light" : ""
        }`}
      >
        {/* HEADER */}
        <header className="p-4 border-b border-[var(--border-color)] flex items-center justify-between">
          <h1 className="text-2xl font-bold">GTA Server Dashboard</h1>

          <nav className="flex gap-2">
            <button
              onClick={() => setActiveTab("players")}
              className={`px-3 py-1.5 text-sm rounded-md ${
                activeTab === "players"
                  ? "bg-blue-600 text-white"
                  : "text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)]"
              }`}
            >
              Players
            </button>

            <button
              onClick={() => setActiveTab("records")}
              className={`px-3 py-1.5 text-sm rounded-md ${
                activeTab === "records"
                  ? "bg-blue-600 text-white"
                  : "text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)]"
              }`}
            >
              Records
            </button>
          </nav>
           
          <div className="flex items-center gap-3">
            <span className="text-xs px-2 py-1 rounded bg-[var(--bg-surface)] border border-[var(--border-color)] text-[var(--text-muted)]">
              {role === "admin" ? "Admin" : "Viewer"}
            </span>
            <button
              onClick={logout}
              className="px-3 py-2 text-sm rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)]"
            >
              Logout
            </button>
          </div>


          <button
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            className="px-3 py-2 text-sm rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)]"
          >
            {theme === "dark" ? "☀️ Light mode" : "🌙 Dark mode"}
          </button>
        </header>

        {/* MAIN CONTENT */}
        <main className="p-6 space-y-4">
          {/* PLAYERS TAB */}
          {activeTab === "players" && (
            <>
              {!loading && !error && <StatsBar players={players} />}

              {/* SEARCH + FILTER BAR */}
              <div className="flex flex-wrap items-center gap-3">
                <input
                  type="text"
                  placeholder="Search players..."
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm w-64 focus:outline-none focus:border-gray-500"
                />

                <select
                  value={statusFilter}
                  onChange={(e) =>
                    setStatusFilter(e.target.value as StatusFilter)
                  }
                  className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm focus:outline-none focus:border-gray-500"
                >
                  <option value="all">All statuses</option>
                  <option value="online">Online</option>
                  <option value="idle">Idle</option>
                  <option value="offline">Offline</option>
                </select>

                <select
                  value={sortBy}
                  onChange={(e) =>
                    setSortBy(e.target.value as "name" | "ping")
                  }
                  className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm focus:outline-none focus:border-gray-500"
                >
                  <option value="name">Sort: Name</option>
                  <option value="ping">Sort: Ping</option>
                </select>

                {!loading && !error && (
                  <span className="text-sm text-[var(--text-muted)] ml-auto">
                    {filteredPlayers.length} of {players.length} players
                  </span>
                )}
              </div>

              {/* LOADING */}
              {loading && <PlayerListSkeleton />}

              {/* ERROR */}
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

              {/* PLAYER LIST */}
              {!loading && !error && (
                <PlayerList
                  players={filteredPlayers}
                  onSelect={setSelectedPlayer}
                  activeWarrantPlayerIds={new Set(warrants.filter((w)=>w.active).map((w)=>w.playerId))}
                />
              )}
            </>
          )}

          {/* RECORDS TAB */}
          {activeTab === "records" && <RecordsView />}
        </main>
      </div>

      {/* PLAYER DETAIL PANEL */}
      <PlayerDetailPanel
        player={selectedPlayer}
        onClose={() => setSelectedPlayer(null)}
        onDeleted={loadPlayers}
      />
    </>
  );
}
