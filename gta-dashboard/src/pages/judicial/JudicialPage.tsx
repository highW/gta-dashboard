import { useCallback, useEffect, useState } from "react";
import { Check, Gavel, X } from "lucide-react";
import {
  actOnWarrant,
  apiErrorMessage,
  fetchArrests,
  fetchWarrants,
  updateArrestStatus,
} from "../../services/mockData";
import type { ArrestRecord, Warrant } from "../../services/mockData";
import { useAuth } from "../../context/AuthContext";

/**
 * The court's desk. Judges see warrant requests from police, supervisors and
 * prosecutors and approve or deny them. Prosecutors see arrests still
 * pending and decide them. Each part shows only to the role that can act.
 */
export default function JudicialPage() {
  const { can } = useAuth();
  const decidesWarrants = can("warrants.decide");
  const decidesCases = can("arrests.decide");

  return (
    <div className="max-w-5xl space-y-10">
      <header>
        <h1 className="text-3xl font-bold">Judicial</h1>
        <p className="text-[var(--text-secondary)] mt-1">
          {decidesWarrants && decidesCases
            ? "Warrant requests and open cases."
            : decidesWarrants
              ? "Warrant requests waiting for a judge."
              : "Arrests waiting for a decision."}
        </p>
      </header>

      {decidesWarrants && <WarrantRequests />}
      {decidesCases && <OpenCases />}
    </div>
  );
}

function Section({
  title,
  count,
  children,
}: {
  title: string;
  count: number | null;
  children: React.ReactNode;
}) {
  return (
    <section>
      <h2 className="text-lg font-bold mb-3">
        {title}
        {count !== null && (
          <span className="ml-2 text-sm font-normal text-[var(--text-muted)]">({count})</span>
        )}
      </h2>
      {children}
    </section>
  );
}

function Empty({ children }: { children: React.ReactNode }) {
  return (
    <p className="text-sm text-[var(--text-muted)] border border-dashed border-[var(--border-color)] rounded-lg px-4 py-6 text-center">
      {children}
    </p>
  );
}

function WarrantRequests() {
  const [requests, setRequests] = useState<Warrant[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<number, string>>({});
  const [busy, setBusy] = useState<number | null>(null);

  const load = useCallback(() => {
    fetchWarrants(undefined, "requested")
      .then(setRequests)
      .catch((err) => setError(apiErrorMessage(err)));
  }, []);
  useEffect(load, [load]);

  const decide = (w: Warrant, action: "approve" | "deny") => {
    setBusy(w.id);
    setError(null);
    actOnWarrant(w.id, action, notes[w.id])
      .catch((err) => setError(apiErrorMessage(err)))
      .finally(() => {
        setBusy(null);
        load();
      });
  };

  return (
    <Section title="Warrant requests" count={requests?.length ?? null}>
      {error && <p role="alert" className="text-red-400 text-sm mb-3">{error}</p>}
      {requests === null && !error && <p className="text-sm text-[var(--text-muted)]">Loading...</p>}
      {requests?.length === 0 && <Empty>No requests waiting. New ones appear here as officers send them.</Empty>}
      <div className="space-y-3">
        {requests?.map((w) => (
          <article
            key={w.id}
            className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-lg p-4"
          >
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <h3 className="font-semibold">
                {w.playerName || "Unknown player"}
                <span className="font-normal text-[var(--text-secondary)]"> · {w.reason}</span>
              </h3>
              <span className="text-xs text-[var(--text-muted)]">
                Requested by {w.issuedBy}
                {w.dateIssued && ` on ${w.dateIssued}`}
              </span>
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <input
                type="text"
                maxLength={500}
                placeholder="Note (optional): reason for the decision"
                aria-label={`Note for the request about ${w.playerName}`}
                value={notes[w.id] ?? ""}
                onChange={(e) => setNotes({ ...notes, [w.id]: e.target.value })}
                className="flex-1 min-w-[220px] bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm"
              />
              <button
                onClick={() => decide(w, "approve")}
                disabled={busy === w.id}
                className="inline-flex items-center gap-1 px-3 py-2 text-sm rounded-md bg-[var(--accent)] text-white hover:brightness-110 disabled:opacity-50"
              >
                <Check size={16} /> Approve
              </button>
              <button
                onClick={() => decide(w, "deny")}
                disabled={busy === w.id}
                className="inline-flex items-center gap-1 px-3 py-2 text-sm rounded-md border border-[var(--border-color)] hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
              >
                <X size={16} /> Deny
              </button>
            </div>
          </article>
        ))}
      </div>
    </Section>
  );
}

function OpenCases() {
  const [cases, setCases] = useState<ArrestRecord[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<number | null>(null);

  const load = useCallback(() => {
    fetchArrests(undefined, "pending")
      .then(setCases)
      .catch((err) => setError(apiErrorMessage(err)));
  }, []);
  useEffect(load, [load]);

  const decide = (a: ArrestRecord, status: "convicted" | "released") => {
    setBusy(a.id);
    setError(null);
    updateArrestStatus(a.id, status)
      .catch((err) => setError(apiErrorMessage(err)))
      .finally(() => {
        setBusy(null);
        load();
      });
  };

  return (
    <Section title="Open cases" count={cases?.length ?? null}>
      {error && <p role="alert" className="text-red-400 text-sm mb-3">{error}</p>}
      {cases === null && !error && <p className="text-sm text-[var(--text-muted)]">Loading...</p>}
      {cases?.length === 0 && <Empty>No pending arrests. Every case has a decision.</Empty>}
      {cases && cases.length > 0 && (
        <div className="rounded-lg border border-[var(--border-color)] overflow-hidden">
          <table className="w-full text-left text-sm">
            <thead className="bg-[var(--bg-surface)] text-[var(--text-muted)] uppercase text-xs">
              <tr>
                <th className="px-4 py-3">Player</th>
                <th className="px-4 py-3">Charge</th>
                <th className="px-4 py-3">Officer</th>
                <th className="px-4 py-3">Date</th>
                <th className="px-4 py-3">Decision</th>
              </tr>
            </thead>
            <tbody>
              {cases.map((a) => (
                <tr key={a.id} className="border-t border-[var(--border-color)]">
                  <td className="px-4 py-3 font-medium">{a.playerName || "Unknown"}</td>
                  <td className="px-4 py-3 text-[var(--text-secondary)]">{a.charge}</td>
                  <td className="px-4 py-3 text-[var(--text-secondary)]">{a.officer}</td>
                  <td className="px-4 py-3 text-[var(--text-secondary)]">{a.date || "—"}</td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    <button
                      onClick={() => decide(a, "convicted")}
                      disabled={busy === a.id}
                      className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded bg-[var(--accent)] text-white hover:brightness-110 disabled:opacity-50"
                    >
                      <Gavel size={14} /> Convict
                    </button>
                    <button
                      onClick={() => decide(a, "released")}
                      disabled={busy === a.id}
                      className="ml-2 px-2 py-1 text-xs rounded border border-[var(--border-color)] hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
                    >
                      Release
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  );
}
