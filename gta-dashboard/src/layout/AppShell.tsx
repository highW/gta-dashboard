import { useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { LogOut, Moon, Sun } from "lucide-react";
import { useAuth } from "../context/AuthContext";
import { visibleNav } from "../config/nav";

type Theme = "dark" | "light";

const linkClass = ({ isActive }: { isActive: boolean }) =>
  `flex items-center gap-3 px-3 py-2 rounded-md text-sm border-l-2 transition-colors ${
    isActive
      ? "border-[var(--accent)] bg-[var(--accent-soft)] text-[var(--text-primary)]"
      : "border-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)]"
  }`;

export default function AppShell() {
  const { role, can, logout } = useAuth();
  const navigate = useNavigate();
  const [theme, setTheme] = useState<Theme>("dark");
  const items = visibleNav(can);
  const main = items.filter((i) => i.group === "main");
  const admin = items.filter((i) => i.group === "admin");

  return (
    <div
      className={`min-h-screen flex bg-[var(--bg-page)] text-[var(--text-primary)] ${
        theme === "light" ? "light" : ""
      }`}
    >
      {/* SIDEBAR (desktop) */}
      <aside className="hidden md:flex w-60 shrink-0 flex-col border-r border-[var(--border-color)] p-4">
        <div className="mb-8 px-2">
          <div className="text-lg font-extrabold italic tracking-tight leading-none">
            THE<span className="text-[var(--accent)]">BLACKLIST</span>
          </div>
          <div className="text-xs text-[var(--text-muted)] mt-1">Network</div>
        </div>

        <nav className="space-y-1">
          {main.map(({ to, label, icon: Icon }) => (
            <NavLink key={to} to={to} className={linkClass}>
              <Icon size={16} strokeWidth={1.75} />
              {label}
            </NavLink>
          ))}
        </nav>

        {admin.length > 0 && (
          <nav className="mt-6 pt-4 border-t border-[var(--border-color)] space-y-1">
            <p className="px-3 pb-1 text-xs text-[var(--text-muted)]">
              Administration
            </p>
            {admin.map(({ to, label, icon: Icon }) => (
              <NavLink key={to} to={to} className={linkClass}>
                <Icon size={16} strokeWidth={1.75} />
                {label}
              </NavLink>
            ))}
          </nav>
        )}
      </aside>

      <div className="flex-1 min-w-0 flex flex-col">
        {/* TOP BAR */}
        <header className="h-14 px-4 md:px-6 flex items-center justify-between border-b border-[var(--border-color)]">
          <span className="md:hidden text-sm font-extrabold italic">
            THE<span className="text-[var(--accent)]">BLACKLIST</span>
          </span>
          <span className="hidden md:block" />

          <div className="flex items-center gap-2">
            <span className="text-xs px-2 py-1 rounded bg-[var(--bg-surface)] border border-[var(--border-color)] text-[var(--text-muted)]">
              {role ? role.charAt(0).toUpperCase() + role.slice(1) : ""}
            </span>
            <button
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
              aria-label="Toggle theme"
              className="p-2 rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)]"
            >
              {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
            </button>
            <button
              onClick={() => {
                logout();
                navigate("/");
              }}
              className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)]"
            >
              <LogOut size={14} />
              Log out
            </button>
          </div>
        </header>

        {/* MOBILE NAV */}
        <nav className="md:hidden flex gap-1 overflow-x-auto px-3 py-2 border-b border-[var(--border-color)]">
          {items.map(({ to, label }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                `whitespace-nowrap px-3 py-1.5 rounded-md text-sm ${
                  isActive
                    ? "bg-[var(--accent)] text-white"
                    : "text-[var(--text-secondary)]"
                }`
              }
            >
              {label}
            </NavLink>
          ))}
        </nav>

        <main className="flex-1 p-4 md:p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
