import {
  LayoutDashboard,
  Shield,
  ScrollText,
  Users,
  type LucideIcon,
} from "lucide-react";

export interface NavItem {
  to: string;
  label: string;
  hint: string;
  icon: LucideIcon;
  /** The permission (from GET /api/whoami) needed to see this item. */
  permission: string;
  group: "main" | "admin";
}

/**
 * Single source of truth for the sidebar AND the Command Center cards.
 * To add a module later (Bank, Real Estate...), add one entry here and one
 * <Route> in App.tsx. Don't add it until its backend works.
 */
export const NAV: NavItem[] = [
  {
    to: "/dashboard",
    label: "Command Center",
    hint: "Overview",
    icon: LayoutDashboard,
    permission: "stats.read",
    group: "main",
  },
  {
    to: "/police",
    label: "Police",
    hint: "Players, records, warrants",
    icon: Shield,
    permission: "players.read",
    group: "main",
  },
  {
    to: "/admin/audit",
    label: "Audit log",
    hint: "Every change, who made it",
    icon: ScrollText,
    permission: "audit.read",
    group: "admin",
  },
  {
    to: "/admin/members",
    label: "Members",
    hint: "Who has access, and as what",
    icon: Users,
    permission: "members.manage",
    group: "admin",
  },
];

export function visibleNav(can: (permission: string) => boolean): NavItem[] {
  return NAV.filter((item) => can(item.permission));
}
