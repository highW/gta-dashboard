import axios from "axios";
import api from "./api";

export interface Player {
  id: number;
  name: string;
  ping: number;
  status: "online" | "idle" | "offline";
  playtimeHours: number;
  vehicle: string;
  /** UTC time, e.g. "2026-11-16T21:04:05Z"; null if the player was never seen. */
  lastSeen: string | null;
}

export const mockPlayers: Player[] = [
  { id: 1, name: "ShadowRunner", ping: 34, status: "online", playtimeHours: 142, vehicle: "Adder", lastSeen: "Now" },
  { id: 2, name: "NightCrawler99", ping: 87, status: "online", playtimeHours: 58, vehicle: "Zentorno", lastSeen: "Now" },
  { id: 3, name: "GhostDriver", ping: 152, status: "idle", playtimeHours: 301, vehicle: "Sultan RS", lastSeen: "3 min ago" },
  { id: 4, name: "VendettaX", ping: 41, status: "online", playtimeHours: 89, vehicle: "Kuruma", lastSeen: "Now" },
  { id: 5, name: "RustyWheels", ping: 0, status: "offline", playtimeHours: 12, vehicle: "Blista", lastSeen: "2 hours ago" },
];

export function fetchMockPlayers(): Promise<Player[]> {
  return new Promise((resolve, reject) => {
    setTimeout(() => {
      // simulate occasional failure so you can see the error state too
      if (Math.random() < 0.15) {
        reject(new Error("Failed to load player data"));
      } else {
        resolve(mockPlayers);
      }
    }, 900);
  });
}

export interface AuditEntry {
  id: number;
  /** UTC time, e.g. "2026-11-16T21:04:05Z". */
  timestamp: string;
  role: string;
  actor: string;
  method: string;
  path: string;
  ip: string;
}

export interface AuditLogResponse {
  entries: AuditEntry[];
  next_cursor: number | null;
}

export function fetchAuditLog(beforeId?: number): Promise<AuditLogResponse> {
  const params: Record<string, number> = { limit: 25 };
  if (beforeId) params.before_id = beforeId;
  return api.get("/audit-log", { params }).then((res) => res.data);
}

export interface ArrestRecord {
  id: number;
  playerId: number;
  /** Filled in by the server. */
  playerName: string;
  charge: string;
  officer: string;
  /** Calendar day, "2026-08-14". Empty only for old records with no readable date. */
  date: string;
  /** "voided" = struck by a supervisor as made in error; it never changes again. */
  status: "pending" | "convicted" | "released" | "voided";
}

/**
 * requested: waiting for a judge. active: approved or issued by a judge.
 * denied: a judge refused the request. cleared: was active, now lifted.
 */
export type WarrantStatus = "requested" | "active" | "denied" | "cleared";

export interface Warrant {
  id: number;
  playerId: number;
  /** Filled in by the server. */
  playerName: string;
  reason: string;
  issuedBy: string;
  /** Calendar day, "2026-08-14". Empty only for old records with no readable date. */
  dateIssued: string;
  status: WarrantStatus;
  /** status === "active" */
  active: boolean;
  /** The account that approved or denied the request. */
  decidedBy: string | null;
  /** UTC time of that decision. */
  decidedAt: string | null;
  decisionNote: string | null;
}

export const mockArrestRecords: ArrestRecord[] = [
  { id: 1, playerId: 3, playerName: "GhostDriver", charge: "Grand Theft Auto", officer: "Officer Reyes", date: "2026-08-14", status: "convicted" },
  { id: 2, playerId: 3, playerName: "GhostDriver", charge: "Evading Police", officer: "Officer Reyes", date: "2026-08-14", status: "convicted" },
  { id: 3, playerId: 5, playerName: "RustyWheels", charge: "Reckless Driving", officer: "Officer Chen", date: "2026-08-20", status: "released" },
  { id: 4, playerId: 1, playerName: "ShadowRunner", charge: "Assault", officer: "Officer Chen", date: "2026-08-25", status: "pending" },
];

const undecided = { decidedBy: null, decidedAt: null, decisionNote: null };

export const mockWarrants: Warrant[] = [
  { id: 1, playerId: 3, playerName: "GhostDriver", reason: "Failure to appear in court", issuedBy: "Judge Alvarez", dateIssued: "2026-08-22", status: "active", active: true, ...undecided },
  { id: 2, playerId: 4, playerName: "VendettaX", reason: "Outstanding fines", issuedBy: "Judge Alvarez", dateIssued: "2026-08-18", status: "active", active: true, ...undecided },
  { id: 3, playerId: 5, playerName: "RustyWheels", reason: "Probation violation", issuedBy: "Judge Kim", dateIssued: "2026-07-30", status: "cleared", active: false, ...undecided },
];

export function fetchMockRecords(): Promise<{ arrests: ArrestRecord[]; warrants: Warrant[] }> {
  return new Promise((resolve) => {
    setTimeout(() => resolve({ arrests: mockArrestRecords, warrants: mockWarrants }), 600);
  });
}

export function fetchPlayers(): Promise<Player[]> {
  return api.get("/players").then((res)=>res.data);
}

// Search is done by the server, across ALL records (player name, charge or
// reason, officer or judge), not just the page that happens to be loaded.
// status narrows the list to one status (e.g. the judge's "requested" queue).
function listParams(search?: string, status?: string) {
  const params: Record<string, string> = {};
  if (search) params.q = search;
  if (status) params.status = status;
  return { params };
}

export function fetchArrests(search?: string, status?: ArrestRecord["status"]): Promise<ArrestRecord[]>{
  return api.get("/arrests", listParams(search, status)).then((res)=>res.data);
}

export function fetchWarrants(search?: string, status?: WarrantStatus): Promise<Warrant[]>{
  return api.get("/warrants", listParams(search, status)).then((res)=>res.data);
}

export function createArrest(arrest: Omit<ArrestRecord,"id" | "playerName">): Promise < ArrestRecord > {
  return api.post("/arrests", arrest).then((res) => res.data);
}

/**
 * A judge's warrant is active at once; anyone else who may ask for one
 * creates a request (status "requested") that waits for a judge.
 */
export function createWarrant(
  warrant: Pick<Warrant, "playerId" | "reason" | "issuedBy" | "dateIssued">,
): Promise < Warrant > {
  return api.post("/warrants", warrant).then((res) => res.data);
}

export type WarrantAction = "approve" | "deny" | "clear";

export function actOnWarrant(id: number, action: WarrantAction, note?: string): Promise<void> {
  return api.patch(`/warrants/${id}`, { action, note: note ?? "" }).then(() => undefined);
}

export function clearWarrant(id: number): Promise<void> {
  return actOnWarrant(id, "clear");
}

/** The server's own explanation of a failed request ("This warrant is denied..."), if it sent one. */
export function apiErrorMessage(err: unknown): string {
  if (axios.isAxiosError(err)) {
    const data = err.response?.data;
    if (typeof data === "string" && data.trim()) return data.trim();
    return err.message;
  }
  return err instanceof Error ? err.message : String(err);
}

export function updateArrestStatus(id: number, status: ArrestRecord["status"]): Promise<void> {
  return api.patch(`/arrests/${id}`, { status }).then(() => undefined);
}

export function updatePlayerStatus(id: number, status: string): Promise<void> {
  return api.patch(`/players/${id}`, { status }).then(() => undefined);
}

export function deletePlayer(id: number): Promise<void> {
  return api.delete(`/players/${id}`).then(() => undefined);
}

export interface Stats {
  players: { total: number; online: number; idle: number; offline: number };
  warrants: { total: number; active: number; requested: number };
  arrests: { total: number; pending: number };
}

export function fetchStats(): Promise<Stats> {
  return api.get("/stats").then((res) => res.data);
}

export interface Member {
  userId: number;
  discordId: string;
  username: string;
  displayName: string;
  lastLogin: string | null;
  role: string | null; // null = signed in, no access yet
}

export function fetchMembers(): Promise<Member[]> {
  return api.get("/members").then((res) => res.data);
}

export function setMemberRole(userId: number, role: string): Promise<void> {
  return api.put(`/members/${userId}`, { role }).then(() => undefined);
}