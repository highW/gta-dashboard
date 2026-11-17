import { useEffect, useState } from "react";
import { fetchPlayers } from "../services/mockData";
import type { Player } from "../services/mockData";

/** Picks one of the server's real players (the forms used a hardcoded list before). */
export default function PlayerSelect({
  value,
  onChange,
}: {
  value: number | null;
  onChange: (id: number) => void;
}) {
  const [players, setPlayers] = useState<Player[] | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    fetchPlayers()
      .then((list) => {
        const sorted = [...list].sort((a, b) => a.name.localeCompare(b.name));
        setPlayers(sorted);
        if (value === null && sorted.length > 0) onChange(sorted[0].id);
      })
      .catch(() => setFailed(true));
    // Load once; the parent owns the selection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const cls =
    "bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm min-w-[160px]";

  if (failed) return <span className="text-sm text-red-400 self-center">Couldn't load players</span>;
  if (!players) return <select disabled className={cls}><option>Loading players...</option></select>;
  if (players.length === 0) return <select disabled className={cls}><option>No players yet</option></select>;

  return (
    <select
      aria-label="Player"
      value={value ?? ""}
      onChange={(e) => onChange(Number(e.target.value))}
      className={cls}
    >
      {players.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </select>
  );
}
