import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
  useLocation,
} from "react-router-dom";
import { AuthProvider, useAuth } from "./context/AuthContext";
import Landing from "./pages/Landing";
import Login from "./components/Login";
import AppShell from "./layout/AppShell";
import RequirePermission from "./components/RequirePermission";
import CommandCenter from "./pages/CommandCenter";
import PoliceLayout from "./pages/police/PoliceLayout";
import PlayersPage from "./pages/police/PlayersPage";
import RecordsView from "./components/RecordsView";
import AuditLogView from "./components/AuditLogView";
import MembersPage from "./pages/admin/MembersPage";

function FullScreenMessage({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-[var(--bg-page)] text-[var(--text-primary)] flex items-center justify-center">
      <div className="text-center space-y-4">{children}</div>
    </div>
  );
}

// Wraps every page that needs a session. It decides what to show while the
// stored key is being checked, when the backend is down, and when nobody is
// signed in (then it remembers where they were headed).
function ProtectedLayout() {
  const { role, loading, serverDown, retry, logout, username } = useAuth();
  const location = useLocation();

  if (loading) {
    return (
      <FullScreenMessage>
        <p className="text-sm text-[var(--text-muted)]">Loading...</p>
      </FullScreenMessage>
    );
  }

  // A key is stored but the backend can't be reached and there's no
  // last-known role: offer a retry instead of pretending the key is wrong.
  if (!role && serverDown) {
    return (
      <FullScreenMessage>
        <p>Can't reach the server.</p>
        <button
          onClick={retry}
          className="px-4 py-2 text-sm rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)]"
        >
          Retry
        </button>
      </FullScreenMessage>
    );
  }

  if (!role) {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }

  // Signed in with Discord, but staff haven't granted a role yet.
  if (role === "pending") {
    return (
      <FullScreenMessage>
        <p className="font-medium">Waiting for access</p>
        <p className="text-sm text-[var(--text-secondary)] max-w-xs">
          {username ? `You're signed in as ${username}, but ` : "You're signed in, but "}
          a staff member hasn't granted you a role on this server yet.
        </p>
        <button
          onClick={logout}
          className="px-4 py-2 text-sm rounded-md border border-[var(--border-color)] bg-[var(--bg-surface)] hover:bg-[var(--bg-surface-hover)]"
        >
          Log out
        </button>
      </FullScreenMessage>
    );
  }

  return <AppShell />;
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          {/* Public */}
          <Route path="/" element={<Landing />} />
          <Route path="/login" element={<Login />} />

          {/* Needs a session */}
          <Route element={<ProtectedLayout />}>
            <Route path="dashboard" element={<CommandCenter />} />

            <Route path="police" element={<PoliceLayout />}>
              <Route index element={<Navigate to="players" replace />} />
              <Route path="players" element={<PlayersPage />} />
              <Route path="records" element={<RecordsView />} />
            </Route>

            <Route
              path="admin/audit"
              element={
                <RequirePermission permission="audit.read">
                  <AuditLogView />
                </RequirePermission>
              }
            />

            <Route
              path="admin/members"
              element={
                <RequirePermission permission="members.manage">
                  <MembersPage />
                </RequirePermission>
              }
            />

            <Route path="*" element={<Navigate to="/dashboard" replace />} />
          </Route>
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  );
}
