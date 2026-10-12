import api from "./api"

export interface Player {
  id: number;
  name: string;
  ping: number;
  status: "online" | "idle" | "offline";
  playtimeHours: number;
  vehicle: string;
  lastSeen: string;
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
  charge: string;
  officer: string;
  date: string;
  status: "convicted" | "released" | "pending";
}

export interface Warrant {
  id: number;
  playerId: number;
  reason: string;
  issuedBy: string;
  dateIssued: string;
  active: boolean;
}

export const mockArrestRecords: ArrestRecord[] = [
  { id: 1, playerId: 3, charge: "Grand Theft Auto", officer: "Officer Reyes", date: "2026-08-14", status: "convicted" },
  { id: 2, playerId: 3, charge: "Evading Police", officer: "Officer Reyes", date: "2026-08-14", status: "convicted" },
  { id: 3, playerId: 5, charge: "Reckless Driving", officer: "Officer Chen", date: "2026-08-20", status: "released" },
  { id: 4, playerId: 1, charge: "Assault", officer: "Officer Chen", date: "2026-08-25", status: "pending" },
];

export const mockWarrants: Warrant[] = [
  { id: 1, playerId: 3, reason: "Failure to appear in court", issuedBy: "Judge Alvarez", dateIssued: "2026-08-22", active: true },
  { id: 2, playerId: 4, reason: "Outstanding fines", issuedBy: "Judge Alvarez", dateIssued: "2026-08-18", active: true },
  { id: 3, playerId: 5, reason: "Probation violation", issuedBy: "Judge Kim", dateIssued: "2026-07-30", active: false },
];

export function fetchMockRecords(): Promise<{ arrests: ArrestRecord[]; warrants: Warrant[] }> {
  return new Promise((resolve) => {
    setTimeout(() => resolve({ arrests: mockArrestRecords, warrants: mockWarrants }), 600);
  });
}

export function fetchPlayers(): Promise<Player[]> {
  return api.get("/players").then((res)=>res.data);
}

export function fetchArrests(): Promise<ArrestRecord[]>{
  return api.get("/arrests").then((res)=>res.data);
}

export function fetchWarrants(): Promise<Warrant[]>{
  return api.get("/warrants").then((res)=>res.data);
}

export function createArrest(arrest: Omit<ArrestRecord,"id">): Promise < ArrestRecord > {
  return api.post("/arrests", arrest).then((res) => res.data);
}

export function createWarrant(warrant: Omit<Warrant, "id" | "active">): Promise < Warrant > {
  return api.post("/warrants", warrant).then((res) => res.data);
}

export function clearWarrant(id: number): Promise<void> {
  return api.patch(`/warrants/${id}`, { active: false }).then(() => undefined);
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
  warrants: { total: number; active: number };
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