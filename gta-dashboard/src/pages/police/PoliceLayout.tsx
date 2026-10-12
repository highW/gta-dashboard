import { NavLink, Outlet } from "react-router-dom";

const tabs = [
  { to: "/police/players", label: "Players" },
  { to: "/police/records", label: "Records" },
];

export default function PoliceLayout() {
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-3xl font-bold">Police</h1>
        <p className="text-[var(--text-secondary)] mt-1">
          Look up players, review arrests, and manage warrants.
        </p>
      </header>

      <nav className="flex gap-1 border-b border-[var(--border-color)]">
        {tabs.map((t) => (
          <NavLink
            key={t.to}
            to={t.to}
            className={({ isActive }) =>
              `px-4 py-2 text-sm -mb-px border-b-2 transition-colors ${
                isActive
                  ? "border-[var(--accent)] text-[var(--text-primary)]"
                  : "border-transparent text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
              }`
            }
          >
            {t.label}
          </NavLink>
        ))}
      </nav>

      <Outlet />
    </div>
  );
}
