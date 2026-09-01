import type { Player } from "../services/mockData";

const statusColor: Record<Player["status"], string> = {
  online: "bg-green-500",
  idle: "bg-yellow-500",
  offline: "bg-gray-500",
};

export default function PlayerList({
  players,
  onSelect,
  activeWarrantPlayerIds,
}: {
  players: Player[];
  onSelect: (player: Player) => void;
  activeWarrantPlayerIds: Set<number>;
}) {
  if (players.length === 0) {
    return (
      <div className="text-center text-[var(--text-muted)] py-12 border border-[var(--border-color)] rounded-lg">
        No players match your search.
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-[var(--border-color)] overflow-hidden">
      <table className="w-full text-left text-sm">
        <thead className="bg-[var(--bg-surface)] text-[var(--text-muted)] uppercase text-xs">
          <tr>
            <th className="px-4 py-3">Player</th>
            <th className="px-4 py-3">Ping</th>
            <th className="px-4 py-3">Status</th>
          </tr>
        </thead>
        <tbody>
          {players.map((p) => (
            <tr
              key={p.id}
              onClick={() => onSelect(p)}
              className="border-t border-[var(--border-color)] hover:bg-[var(--bg-surface-hover)] cursor-pointer"
            >
              <td className="px-4 py-3 font-medium">
                <span className="inline-flex items-center gap-2">
                  {p.name}
                  {activeWarrantPlayerIds.has(p.id) && (
                    <span className="px-1.5 py-0.5 rounded text-[10px] font-bold bg-red-500/20 text-red-400 border border-red-500/30">
                      WANTED
                    </span>
                  )}
                </span>
              </td>
              <td className="px-4 py-3 text-[var(--text-secondary)]">{p.ping} ms</td>
              <td className="px-4 py-3">
                <span className="inline-flex items-center gap-2">
                  <span className={`w-2 h-2 rounded-full ${statusColor[p.status]}`} />
                  {p.status}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
