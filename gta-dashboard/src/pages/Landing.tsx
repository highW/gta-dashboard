import { Link } from "react-router-dom";
import { useAuth } from "../context/AuthContext";

// Public page: no private data, no made-up statistics.
export default function Landing() {
  const { role, loading } = useAuth();

  const button =
    "inline-block px-6 py-3 rounded-md text-sm font-medium bg-[var(--accent)] text-white hover:brightness-110 transition";

  return (
    <div className="min-h-screen bg-[var(--bg-page)] text-[var(--text-primary)] flex items-center justify-center px-6">
      <div className="max-w-lg text-center space-y-6">
        <div>
          <div className="text-4xl font-extrabold italic tracking-tight">
            THE<span className="text-[var(--accent)]">BLACKLIST</span>
          </div>
          <div className="text-xs tracking-[0.3em] text-[var(--text-muted)] mt-2">
            NETWORK
          </div>
        </div>

        <p className="text-[var(--text-secondary)]">
          Roleplay server management: live players, arrests, warrants and a
          full audit trail, with access set by role.
        </p>

        {/* empty while the stored session is being checked, so it doesn't flicker */}
        <div className="h-11">
          {!loading &&
            (role ? (
              <Link to="/dashboard" className={button}>
                Open dashboard
              </Link>
            ) : (
              <Link to="/login" className={button}>
                Sign in
              </Link>
            ))}
        </div>

        {!loading && !role && (
          <p className="text-xs text-[var(--text-muted)]">
            Access is granted by your server's staff.
          </p>
        )}
      </div>
    </div>
  );
}
