import type { ArrestRecord, WarrantStatus } from "../services/mockData";

const WARRANT: Record<WarrantStatus, { label: string; cls: string }> = {
  requested: { label: "REQUESTED", cls: "bg-amber-500/15 text-amber-400" },
  active: { label: "ACTIVE", cls: "bg-red-500/20 text-red-400" },
  denied: { label: "DENIED", cls: "bg-gray-500/20 text-gray-400" },
  cleared: { label: "CLEARED", cls: "bg-gray-500/20 text-gray-400" },
};

const ARREST: Record<ArrestRecord["status"], { label: string; dot: string }> = {
  pending: { label: "pending", dot: "bg-yellow-500" },
  convicted: { label: "convicted", dot: "bg-red-500" },
  released: { label: "released", dot: "bg-gray-500" },
  voided: { label: "voided", dot: "bg-gray-700" },
};

export function WarrantBadge({ status }: { status: WarrantStatus }) {
  const s = WARRANT[status] ?? WARRANT.cleared;
  return <span className={`px-2 py-1 rounded text-xs font-medium ${s.cls}`}>{s.label}</span>;
}

export function ArrestStatus({ status }: { status: ArrestRecord["status"] }) {
  const s = ARREST[status] ?? ARREST.pending;
  return (
    <span className="inline-flex items-center gap-2">
      <span className={`w-2 h-2 rounded-full ${s.dot}`} />
      {s.label}
    </span>
  );
}
