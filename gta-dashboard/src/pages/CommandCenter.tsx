import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ArrowRight } from "lucide-react";
import { useAuth } from "../context/AuthContext";
import { visibleNav } from "../config/nav";
import { fetchStats, fetchAuditLog } from "../services/mockData";
import type { Stats, AuditEntry } from "../services/mockData";

function timeAgo(ts: string | number | Date): string {
  const secs = Math.max(0, (Date.now() - new Date(ts).getTime()) / 1000);
  if (secs < 60) return "just now";
  if (secs < 3600) return `${Math.floor(secs / 60)} min ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)} h ago`;
  return `${Math.floor(secs / 86400)} d ago`;
}

const VERBS: Record<string, string> = {
  POST: "Created",
  PATCH: "Updated",
  PUT: "Updated",
  DELETE: "Deleted",
};

function describe(method: string, path: string): string {
  const resource = path.split("/").filter(Boolean)[1] ?? "record";
  return `${VERBS[method] ?? method} ${resource}`;
}

export default function CommandCenter() {
  const { role, can } = useAuth();
  const [stats, setStats] = useState<Stats | null>(null);
  const [statsFailed, setStatsFailed] = useState(false);
  // The audit endpoint is admin-only on the backend, so only ask when admin.
  const [activity, setActivity] = useState<AuditEntry[] | null>(null);

  useEffect(() => {
    const loadStats = () =>
      fetchStats()
        .then((s) => {
          setStats(s);
          setStatsFailed(false);
        })
        .catch(() => setStatsFailed(true));

    loadStats();
    const interval = setInterval(loadStats, 10000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (!can("audit.read")) return;
    fetchAuditLog()
      .then((d) => setActivity(d.entries.slice(0, 8)))
      .catch(() => setActivity([]));
  }, [can]);

  const cards = [
    {
      label: "Players online",
      value: stats?.players.online,
      sub: stats ? `${stats.players.total} tracked` : "",
      to: "/police/players",
    },
    {
      label: "Active warrants",
      value: stats?.warrants.active,
      sub: stats ? `${stats.warrants.total} total` : "",
      to: "/police/records",
    },
    {
      label: "Pending arrests",
      value: stats?.arrests.pending,
      sub: stats ? `${stats.arrests.total} total` : "",
      to: "/police/records",
    },
  ];

  const modules = visibleNav(can).filter((i) => i.to !== "/dashboard");

  return (
    <div className="max-w-5xl space-y-10">
      <header>
        <h1 className="text-3xl font-bold">Command Center</h1>
        <p className="text-[var(--text-secondary)] mt-1">
          {role === "admin"
            ? "Network status and administration overview."
            : "Your city at a glance."}
        </p>
      </header>

      {/* STATS (exact counts from GET /api/stats) */}
      <section>
        {statsFailed && !stats && (
          <p className="mb-3 text-sm text-red-400">
            Couldn't load statistics. Retrying...
          </p>
        )}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {cards.map((c) => (
            <Link
              key={c.label}
              to={c.to}
              className="p-5 rounded-lg border border-[var(--border-color)] bg-[var(--bg-surface)] hover:border-[var(--accent)] transition-colors"
            >
              <div className="text-sm text-[var(--text-muted)]">{c.label}</div>
              <div className="text-4xl font-bold mt-2">
                {c.value === undefined ? "–" : c.value}
              </div>
              <div className="text-xs text-[var(--text-muted)] mt-1 h-4">
                {c.sub}
              </div>
            </Link>
          ))}
        </div>
      </section>

      {/* MODULES */}
      <section>
        <h2 className="text-lg font-semibold mb-3">Systems</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          {modules.map(({ to, label, hint, icon: Icon }) => (
            <Link
              key={to}
              to={to}
              className="flex items-center gap-4 p-4 rounded-lg border border-[var(--border-color)] bg-[var(--bg-surface)] hover:border-[var(--accent)] transition-colors"
            >
              <Icon size={20} strokeWidth={1.5} className="text-[var(--accent)]" />
              <div className="flex-1">
                <div className="font-medium">{label}</div>
                <div className="text-sm text-[var(--text-muted)]">{hint}</div>
              </div>
              <ArrowRight size={16} className="text-[var(--text-muted)]" />
            </Link>
          ))}
        </div>
      </section>

      {/* ACTIVITY (admin only) */}
      {can("audit.read") && (
        <section>
          <div className="flex items-baseline justify-between mb-3">
            <h2 className="text-lg font-semibold">Recent activity</h2>
            <Link
              to="/admin/audit"
              className="text-sm text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
            >
              View audit log
            </Link>
          </div>

          <div className="rounded-lg border border-[var(--border-color)] divide-y divide-[var(--border-color)]">
            {activity === null && (
              <p className="p-4 text-sm text-[var(--text-muted)]">Loading...</p>
            )}
            {activity?.length === 0 && (
              <p className="p-4 text-sm text-[var(--text-muted)]">
                No activity yet. Changes to players, arrests and warrants appear here.
              </p>
            )}
            {activity?.map((e) => (
              <div key={e.id} className="flex items-center justify-between p-4 text-sm">
                <div>
                  <span className="font-medium">{describe(e.method, e.path)}</span>
                  <span className="text-[var(--text-muted)]"> by {e.role}</span>
                </div>
                <span className="text-[var(--text-muted)]">{timeAgo(e.timestamp)}</span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
