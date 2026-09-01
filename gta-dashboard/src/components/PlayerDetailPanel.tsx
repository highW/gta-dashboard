import type { Player } from "../services/mockData";
import { deletePlayer } from "../services/mockData";
import { useAuth } from "../context/AuthContext";

const statusColor: Record<Player["status"], string> = {
    online: "bg-green-500",
    idle: "bg-yellow-500",
    offline: "bg-gray-500",
};

export default function PlayerDetailPanel({
    player,
    onClose,
    onDeleted,
}: {
    player: Player | null;
    onClose: () => void;
    onDeleted: () => void;
}) {
    const { role } = useAuth();
    if (!player) return null;

    const handleDelete = () => {
        if (!confirm(`Remove ${player.name} from tracking?`)) return;
        deletePlayer(player.id).then(() => {
            onClose();
            onDeleted();
        });
    };

    return (
        <>
            <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} />

            <div className="fixed right-0 top-0 h-full w-80 bg-[var(--bg-surface)] text-[var(--text-primary)] border-l border-[var(--border-color)] z-50 p-6 space-y-6 overflow-y-auto">
                <div className="flex items-start justify-between">
                    <div>
                        <h2 className="text-xl font-bold">{player.name}</h2>
                        <span className="inline-flex items-center gap-2 text-sm text-[var(--text-secondary)] mt-1">
                            <span className={`w-2 h-2 rounded-full ${statusColor[player.status]}`} />
                            {player.status}
                        </span>
                    </div>
                    <button
                        onClick={onClose}
                        className="text-[var(--text-muted)] hover:text-[var(--text-primary)] text-xl leading-none"
                    >
                        ×
                    </button>
                </div>

                <div className="space-y-4">
                    <DetailRow label="Ping" value={`${player.ping} ms`} />
                    <DetailRow label="Playtime" value={`${player.playtimeHours} hrs`} />
                    <DetailRow label="Current vehicle" value={player.vehicle} />
                    <DetailRow label="Last seen" value={player.lastSeen} />
                </div>
            {role == "admin" && (
                <button
                    onClick={handleDelete}
                    className="w-full px-4 py-2 text-sm bg-red-600/20 text-red-400 hover:bg-red-600/30 rounded-md border border-red-600/30"
                >
                    Remove Player
                </button>
                )}
            </div>
        </>
    );
}

function DetailRow({ label, value }: { label: string; value: string }) {
    return (
        <div className="border-b border-[var(--border-color)] pb-3">
            <div className="text-xs text-[var(--text-muted)] uppercase tracking-wide">{label}</div>
            <div className="text-base mt-1">{value}</div>
        </div>
    );
}
