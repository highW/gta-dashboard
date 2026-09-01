import { AuthProvider, useAuth } from "./context/AuthContext";
import Login from "./components/Login";
import Dashboard from "./pages/Dashboard";

function AppContent() {
  const { role } = useAuth();
  if (!role) return <Login />;
  return <Dashboard />;
}

export default function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  );
}
