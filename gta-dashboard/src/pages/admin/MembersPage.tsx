import { useEffect, useState } from "react";
import axios from "axios";
import { fetchMembers, setMemberRole } from "../../services/mockData";
import type { Member } from "../../services/mockData";

const ROLE_OPTIONS = [
  { value: "none", label: "No access" },
  { value: "viewer", label: "Viewer" },
  { value: "police", label: "Police" },
  { value: "judge", label: "Judge" },
  { value: "admin", label: "Admin" },
];

function formatLogin(ts: string | null): string {
  return ts ? ts.slice(0, 16).replace("T", " ") : "never";
}

export default function MembersPage() {
  const [members, setMembers] = useState<Member[] | null>(null);
  const [error, setError] = useState("");
  const [savingId, setSavingId] = useState<number | null>(null);

  const load = () =>
    fetchMembers()
      .then(setMembers)
      .catch(() => setError("Couldn't load members."));

  useEffect(() => {
    load();
  }, []);

  const change = async (m: Member, role: string) => {
    setError("");
    setSavingId(m.userId);
    try {
      await setMemberRole(m.userId, role);
      await load();
    } catch (err) {
      // the server's message, e.g. "Can't remove the last admin on this server"
      if (axios.isAxiosError(err) && typeof err.response?.data === "string") {
        setError(err.response.data.trim());
      } else {
        setError("Couldn't change the role.");
      }
    } finally {
      setSavingId(null);
    }
  };

  const waiting = members?.filter((m) => m.role === null).length ?? 0;

  return (
    <div className="max-w-4xl space-y-6">
      <header>
        <h1 className="text-3xl font-bold">Members</h1>
        <p className="text-[var(--text-secondary)] mt-1">
          People who have signed in with Discord, and what they can do here.
          Changes apply on their next click.
        </p>
      </header>

      {waiting > 0 && (
        <p className="text-sm px-4 py-3 rounded-md border border-[var(--accent)] bg-[var(--accent-soft)]">
          {waiting} {waiting === 1 ? "person is" : "people are"} waiting for access.
        </p>
      )}
      {error && <p className="text-sm text-red-400">{error}</p>}

      <div className="rounded-lg border border-[var(--border-color)] divide-y divide-[var(--border-color)]">
        {members === null && !error && (
          <p className="p-4 text-sm text-[var(--text-muted)]">Loading...</p>
        )}
        {members?.length === 0 && (
          <p className="p-4 text-sm text-[var(--text-muted)]">
            Nobody has signed in yet.
          </p>
        )}
        {members?.map((m) => (
          <div
            key={m.userId}
            className="flex items-center justify-between gap-4 p-4"
          >
            <div className="min-w-0">
              <div className="font-medium truncate">{m.displayName}</div>
              <div className="text-xs text-[var(--text-muted)] truncate">
                @{m.username} · last sign-in {formatLogin(m.lastLogin)}
              </div>
            </div>
            <select
              value={m.role ?? "none"}
              disabled={savingId === m.userId}
              onChange={(e) => change(m, e.target.value)}
              className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm focus:outline-none focus:border-gray-500 disabled:opacity-50"
            >
              {ROLE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>
        ))}
      </div>
    </div>
  );
}
