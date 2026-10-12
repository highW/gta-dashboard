import { Navigate } from "react-router-dom";
import type { ReactNode } from "react";
import { useAuth } from "../context/AuthContext";

/**
 * UX only. Hiding a route is not security: the backend's permission checks
 * are what actually block the request.
 */
export default function RequirePermission({
  permission,
  children,
}: {
  permission: string;
  children: ReactNode;
}) {
  const { can } = useAuth();
  if (!can(permission)) {
    return <Navigate to="/dashboard" replace />;
  }
  return <>{children}</>;
}
