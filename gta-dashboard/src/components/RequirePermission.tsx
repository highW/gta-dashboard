import { Navigate } from "react-router-dom";
import type { ReactNode } from "react";
import { useAuth } from "../context/AuthContext";
import { canAny } from "../config/nav";

/**
 * UX only. Hiding a route is not security: the backend's permission checks
 * are what actually block the request.
 */
export default function RequirePermission({
  permission,
  children,
}: {
  /** One permission, or a list where any one is enough. */
  permission: string | string[];
  children: ReactNode;
}) {
  const { can } = useAuth();
  if (!canAny(can, permission)) {
    return <Navigate to="/dashboard" replace />;
  }
  return <>{children}</>;
}
