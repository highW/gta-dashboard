import type { Player } from "../services/mockData";

function StatCard({
    label,
    value,
    colorClass,
}: {
    label: string;
    value: string | number;
    colorClass?: string;
}) {
    return (
        <div className="flex-1 min-w-[120px] bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-lg p-4">
            <div className="flex items-center gap-2 text-[var(--text-muted)] text-xs uppercase tracking-wide">
                {colorClass && <span className={`w-2 h-2 rounded-full ${colorClass}`} />}
                {label}
            </div>
            <div className="text-3xl font-bold mt-1">{value}</div>
        </div>
    );
}

export default function StatsBar({ players }: { players: Player[] }) {
    const online = players.filter((p) => p.status === "online").length;
    const idle = players.filter((p) => p.status === "idle").length;
    const offline = players.filter((p) => p.status === "offline").length;

    const avgPing = players.length
        ? Math.round(players.reduce((sum, p) => sum + p.ping, 0) / players.length)
        : 0;

    const topPlayer = players.length
        ? players.reduce((top, p) => (p.playtimeHours > top.playtimeHours ? p : top))
        : null;

    return (
        <div className="flex flex-wrap gap-3">
            <StatCard label="Total" value={players.length} colorClass="bg-blue-500" />
            <StatCard label="Online" value={online} colorClass="bg-green-500" />
            <StatCard label="Idle" value={idle} colorClass="bg-yellow-500" />
            <StatCard label="Offline" value={offline} colorClass="bg-gray-500" />
            <StatCard label="Avg Ping" value={`${avgPing} ms`} />
            <StatCard
                label="Most Played"
                value={topPlayer ? `${topPlayer.name} (${topPlayer.playtimeHours}h)` : "—"}
            />
        </div>
    );
}
