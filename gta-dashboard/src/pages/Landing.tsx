import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  Activity,
  Banknote,
  Clock,
  House,
  LogIn,
  Scale,
  Server,
  Settings,
  Shield,
  ShoppingCart,
  Users,
  VenetianMask,
  type LucideIcon,
} from "lucide-react";
import "@fontsource/permanent-marker";
import "@fontsource/barlow/400.css";
import "@fontsource/barlow/600.css";
import "@fontsource/barlow-condensed/600.css";
import "@fontsource/barlow-condensed/700.css";
import { useAuth } from "../context/AuthContext";
import { fetchPublicStats } from "../services/mockData";
import type { PublicStats } from "../services/mockData";
import Skyline from "./landing/Skyline";

// Public page. Every number on it is live from the server; anything not
// built yet says so instead of pretending.

// Optional: set VITE_DISCORD_INVITE to link the Community menu item.
const DISCORD_INVITE: string | undefined = import.meta.env.VITE_DISCORD_INVITE;

function Wordmark({ size = "md" }: { size?: "md" | "xl" }) {
  const cls =
    size === "xl"
      ? "whitespace-nowrap text-[clamp(1.5rem,8vw,6.5rem)] leading-[0.9]"
      : "text-xl sm:text-2xl leading-none";
  return (
    <span className={`font-marker -rotate-2 inline-block ${cls}`} aria-label="The Blacklist Network">
      <span className="text-white">The</span>
      <span className="text-[var(--accent)]">Blacklist</span>
      <span className="text-white">Network</span>
    </span>
  );
}

function SoonTag() {
  return (
    <span className="ml-1.5 rounded-sm bg-white/10 px-1.5 py-0.5 text-[0.65rem] font-semibold text-white/60 align-middle">
      Soon
    </span>
  );
}

const navItems: { label: string; to?: string; href?: string }[] = [
  { label: "Home", to: "/" },
  { label: "Servers" },
  { label: "Community", href: DISCORD_INVITE },
  { label: "Support" },
];

function Header() {
  const { role, loading } = useAuth();
  return (
    <header className="border-b border-white/10">
      <div className="mx-auto flex max-w-7xl items-center gap-3 px-4 py-4 sm:gap-6 sm:px-6">
        <Link to="/" className="shrink-0 rounded focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--accent)]">
          <Wordmark />
        </Link>

        <nav aria-label="Main" className="hidden flex-1 justify-center gap-8 text-[0.95rem] md:flex">
          {navItems.map((item) =>
            item.to ? (
              <Link key={item.label} to={item.to} aria-current="page"
                className="border-b-2 border-[var(--accent)] pb-1 font-semibold text-white">
                {item.label}
              </Link>
            ) : item.href ? (
              <a key={item.label} href={item.href} target="_blank" rel="noreferrer"
                className="pb-1 text-white/70 hover:text-white">
                {item.label}
              </a>
            ) : (
              <span key={item.label} className="cursor-default pb-1 text-white/45" title="Coming soon">
                {item.label}
                <SoonTag />
              </span>
            ),
          )}
        </nav>

        <div className="ml-auto flex shrink-0 items-center gap-1 sm:gap-3 md:ml-0">
          {!loading &&
            (role ? (
              <Link to="/dashboard"
                className="rounded-md bg-[var(--accent)] px-4 py-2 text-sm font-semibold text-white hover:brightness-110">
                Open dashboard
              </Link>
            ) : (
              <>
                <Link to="/login" aria-label="Log in"
                  className="inline-flex items-center gap-1.5 px-2 py-2 text-sm text-white/85 hover:text-white">
                  <LogIn size={18} aria-hidden /> <span className="hidden sm:inline">Log in</span>
                </Link>
                {/* Signing in with Discord the first time creates the account. */}
                <Link to="/login"
                  className="rounded-md border border-[var(--accent)] px-3 py-2 text-sm font-semibold text-white hover:bg-[var(--accent)] sm:px-4">
                  Register
                </Link>
              </>
            ))}
        </div>
      </div>
    </header>
  );
}

function Hero() {
  return (
    <section className="relative overflow-hidden rounded-xl border border-white/10">
      <Skyline className="absolute inset-0 h-full w-full" />
      {/* keeps the wordmark and tagline readable over the lit windows */}
      <div className="absolute inset-0 bg-gradient-to-b from-black/10 via-black/20 to-black/60" aria-hidden />
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_55%_45%_at_50%_58%,rgba(7,8,13,0.78),transparent)]" aria-hidden />
      <div className="relative flex min-h-[340px] flex-col items-center justify-center px-4 py-16 text-center sm:min-h-[420px]">
        <h1 className="drop-shadow-[0_4px_18px_rgba(0,0,0,0.6)]">
          <Wordmark size="xl" />
        </h1>
        <p className="mt-5 font-barlow-condensed text-lg font-semibold tracking-[0.35em] text-white/90 sm:text-xl">
          GTA 6 ROLEPLAY
        </p>
        <p className="mt-3 text-[0.95rem] text-white/75">
          Real people. Real stories. Endless possibilities.
        </p>
      </div>
    </section>
  );
}

function StatusStrip() {
  const [stats, setStats] = useState<PublicStats | null>(null);
  const [reachable, setReachable] = useState<boolean | null>(null);

  useEffect(() => {
    const load = () =>
      fetchPublicStats()
        .then((s) => {
          setStats(s);
          setReachable(true);
        })
        .catch(() => setReachable(false));
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, []);

  const statusText = reachable === null ? "Checking..." : reachable ? "Online" : "Unreachable";
  const statusColor = reachable === null ? "bg-white/40" : reachable ? "bg-green-500" : "bg-red-500";

  const cells: { icon: LucideIcon; label: string; value: string; note: string; muted?: boolean }[] = [
    {
      icon: Activity,
      label: "Network status",
      value: statusText,
      note: reachable ? "Dashboard and API responding" : reachable === false ? "Trying again every 30 seconds" : "",
    },
    {
      icon: Users,
      label: "Players online",
      value: stats ? stats.players.online.toLocaleString() : "—",
      note: "Across all live servers",
    },
    {
      icon: Server,
      label: "Live servers",
      value: stats ? stats.servers.active.toLocaleString() : "—",
      note: "Hosted on the network",
    },
    {
      icon: Clock,
      label: "Uptime",
      value: "Coming soon",
      note: "Monitoring isn't set up yet",
      muted: true,
    },
  ];

  return (
    <section aria-label="Network status"
      className="grid grid-cols-2 divide-white/10 rounded-xl border border-white/10 bg-[#10121a] lg:grid-cols-4 lg:divide-x">
      {cells.map(({ icon: Icon, label, value, note, muted }, i) => (
        <div key={label} className={`flex items-start gap-3 p-5 ${i < 2 ? "border-b border-white/10 lg:border-b-0" : ""}`}>
          {i === 0 ? (
            <span className={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full ${statusColor}`} aria-hidden />
          ) : (
            <Icon size={22} className="mt-0.5 shrink-0 text-white/60" aria-hidden />
          )}
          <div>
            <p className="text-sm text-white/55">{label}</p>
            <p className={`font-barlow-condensed text-2xl font-bold ${muted ? "text-white/45" : i === 0 && reachable ? "text-green-400" : "text-white"}`}>
              {value}
            </p>
            <p className="text-xs text-white/40">{note}</p>
          </div>
        </div>
      ))}
    </section>
  );
}

function Hosting() {
  return (
    <section className="grid overflow-hidden rounded-xl border border-white/10 bg-[#10121a] md:grid-cols-[1.15fr_1fr]">
      <div className="flex flex-col justify-center gap-4 p-8 sm:p-10">
        <h2 className="font-marker text-[clamp(1.7rem,3.4vw,2.6rem)] leading-tight text-white -rotate-1">
          Want us to host your server and bring it to life?
        </h2>
        <p className="max-w-[46ch] text-white/70">
          Get your own roleplay server on The Blacklist Network, with the police and court
          dashboard and staff tools already set up, so your team can focus on the story.
        </p>
        <div className="flex flex-wrap items-center gap-3">
          <button type="button" disabled aria-describedby="hosting-soon"
            className="inline-flex cursor-not-allowed items-center gap-2 rounded-md bg-[var(--accent)] px-6 py-3 font-semibold text-white opacity-60">
            <Server size={18} aria-hidden /> Rent a server
          </button>
          <span id="hosting-soon" className="text-sm text-white/55">
            Coming soon: server hosting isn't open yet.
          </span>
        </div>
      </div>
      <div className="relative min-h-[220px]" aria-hidden>
        <Skyline seed={7} helicopter={false} className="absolute inset-0 h-full w-full" />
        <div className="absolute inset-0 bg-gradient-to-r from-[#10121a] via-transparent to-transparent" />
      </div>
    </section>
  );
}

interface Module {
  title: string;
  line: string;
  icon: LucideIcon;
  tint: string; // top glow of the tile
  to?: string; // missing = not built yet
}

const MODULES: Module[] = [
  { title: "Judicial", line: "Warrants and verdicts", icon: Scale, tint: "rgba(59,108,255,0.35)", to: "/judicial" },
  { title: "Police", line: "Arrests, records, warrants", icon: Shield, tint: "rgba(59,130,246,0.30)", to: "/police" },
  { title: "Criminal", line: "Gangs and the underworld", icon: VenetianMask, tint: "rgba(237,28,46,0.38)" },
  { title: "Store", line: "Gear and vehicles", icon: ShoppingCart, tint: "rgba(168,85,247,0.35)" },
  { title: "Bank", line: "Accounts and transfers", icon: Banknote, tint: "rgba(34,197,94,0.28)" },
  { title: "Real estate", line: "Buy, sell, own", icon: House, tint: "rgba(20,184,166,0.30)" },
  { title: "Admin dashboard", line: "Run your server", icon: Settings, tint: "rgba(148,163,184,0.28)", to: "/dashboard" },
];

function Modules() {
  return (
    <section aria-labelledby="modules-title">
      <h2 id="modules-title" className="sr-only">What's on the network</h2>
      <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-7">
        {MODULES.map(({ title, line, icon: Icon, tint, to }) => {
          const body = (
            <>
              <Icon size={44} strokeWidth={1.5} className="text-white" aria-hidden />
              <span className="mt-4 font-barlow-condensed text-xl font-bold uppercase tracking-wide text-white">
                {title}
              </span>
              <span className="mt-1 text-sm text-white/60">{line}</span>
              {!to && (
                <span className="mt-3 rounded-sm border border-white/20 px-2 py-0.5 text-xs text-white/60">
                  Coming soon
                </span>
              )}
            </>
          );
          const base =
            "flex h-full flex-col items-center rounded-lg border px-3 pb-5 pt-7 text-center";
          const glow = { backgroundImage: `radial-gradient(120% 70% at 50% 0%, ${tint}, transparent 70%)` };
          return (
            <li key={title}>
              {to ? (
                <Link to={to} style={glow}
                  className={`${base} border-white/15 bg-[#10121a] hover:border-[var(--accent)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]`}>
                  {body}
                </Link>
              ) : (
                <div style={glow} aria-disabled="true"
                  className={`${base} border-white/10 bg-[#0c0e14] opacity-65 saturate-50`}>
                  {body}
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function Footer() {
  return (
    <footer className="border-t border-white/10">
      <div className="mx-auto flex max-w-7xl flex-col items-center justify-between gap-4 px-4 py-8 text-sm text-white/50 sm:flex-row sm:px-6">
        <Wordmark />
        <p className="text-center">
          Not affiliated with Rockstar Games or Take-Two Interactive.
        </p>
        <p>© {new Date().getFullYear()} The Blacklist Network</p>
      </div>
    </footer>
  );
}

export default function Landing() {
  return (
    <div className="min-h-screen bg-[#07080d] font-barlow text-white">
      <Header />
      <main className="mx-auto max-w-7xl space-y-5 px-4 py-6 sm:px-6">
        <Hero />
        <StatusStrip />
        <Hosting />
        <Modules />
      </main>
      <Footer />
    </div>
  );
}
