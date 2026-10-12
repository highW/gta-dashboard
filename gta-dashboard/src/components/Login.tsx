import { useState } from "react";
import { Link, Navigate, useLocation } from "react-router-dom";
import { useAuth } from "../context/AuthContext";

// Shown when Discord sends the browser back with ?error=...
const DISCORD_ERRORS: Record<string, string> = {
  state: "That sign-in link expired or didn't start here. Please try again.",
  denied: "Discord sign-in was cancelled.",
  discord: "Discord didn't confirm the sign-in. Please try again.",
  discord_unavailable: "Discord sign-in isn't set up on this server yet.",
  server: "Something went wrong on our side. Please try again.",
};

export default function Login() {
  const { role, loading, login, loginWithDiscord } = useAuth();
  const location = useLocation();
  const [key, setKey] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const discordError =
    DISCORD_ERRORS[new URLSearchParams(location.search).get("error") ?? ""] ?? "";

  // Where the guard sent us from (falls back to the dashboard).
  const from = (
    location.state as { from?: { pathname: string; search?: string } } | null
  )?.from;
  const target = from ? from.pathname + (from.search ?? "") : "/dashboard";

  // Wait for the session check, and if already signed in, leave. Redirecting
  // from state (not an imperative navigate() after login) means no race
  // between "role is set" and "page changed".
  if (loading) return null;
  if (role) return <Navigate to={target} replace />;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError("");
    const result = await login(key);
    if (result === "invalid") setError("Invalid API key");
    if (result === "unreachable") {
      setError("Can't reach the server. Try again in a moment.");
    }
    setSubmitting(false);
  };

  const field =
    "w-full bg-[var(--bg-page)] border border-[var(--border-color)] rounded-md px-3 py-2 text-sm focus:outline-none focus:border-gray-500";

  return (
    <div className="min-h-screen flex items-center justify-center bg-[var(--bg-page)] text-[var(--text-primary)] px-4">
      <div className="bg-[var(--bg-surface)] border border-[var(--border-color)] rounded-lg p-8 space-y-5 w-80">
        <div className="text-center">
          <div className="text-xl font-extrabold italic">
            THE<span className="text-[var(--accent)]">BLACKLIST</span>
          </div>
          <p className="text-sm text-[var(--text-muted)] mt-1">Sign in</p>
        </div>

        <button
          type="button"
          onClick={loginWithDiscord}
          className="w-full px-4 py-2 bg-[#5865F2] text-white hover:brightness-110 rounded-md text-sm font-medium"
        >
          Continue with Discord
        </button>
        {discordError && <p className="text-red-400 text-sm">{discordError}</p>}

        <div className="flex items-center gap-3 text-xs text-[var(--text-muted)]">
          <div className="flex-1 h-px bg-[var(--border-color)]" />
          or use an API key
          <div className="flex-1 h-px bg-[var(--border-color)]" />
        </div>

        <form onSubmit={handleSubmit} className="space-y-3">
          <input
            type="password"
            placeholder="API key"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            className={field}
          />
          {error && <p className="text-red-400 text-sm">{error}</p>}
          <button
            type="submit"
            disabled={submitting || key === ""}
            className="w-full px-4 py-2 border border-[var(--border-color)] hover:bg-[var(--bg-surface-hover)] disabled:opacity-50 rounded-md text-sm"
          >
            {submitting ? "Checking..." : "Sign in with key"}
          </button>
        </form>

        <p className="text-center text-xs text-[var(--text-muted)]">
          <Link to="/" className="hover:text-[var(--text-primary)]">
            Back
          </Link>
        </p>
      </div>
    </div>
  );
}
