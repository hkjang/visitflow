import { CircularProgress, Box } from "@mui/material";
import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { AppShell } from "./components/AppShell";
import { LoginPage } from "./pages/LoginPage";
import { PersonalDashboardPage } from "./pages/PersonalDashboardPage";
import { VisitFormPage } from "./pages/VisitFormPage";
import { VisitsPage } from "./pages/VisitsPage";
import { TemplatesPage } from "./pages/TemplatesPage";
import { LobbyPage } from "./pages/LobbyPage";
import { ScannerPage } from "./pages/ScannerPage";
import { AdminPage } from "./pages/AdminPage";
import { SettingsPage } from "./pages/SettingsPage";
import { KeysPage } from "./pages/KeysPage";
import { MobilePassPage } from "./pages/MobilePassPage";

function Protected() {
  const { user } = useAuth();
  return user ? <AppShell /> : <Navigate to="/login" replace />;
}
function LobbyGuard({ children }: { children: React.ReactNode }) {
  const { user } = useAuth();
  return user && ["lobby", "security", "admin", "super_admin"].includes(user.role) ? children : <Navigate to="/" replace />;
}
function AdminGuard({ children, audit = false }: { children: React.ReactNode; audit?: boolean }) {
  const { user } = useAuth();
  const roles = audit ? ["auditor", "security", "admin", "super_admin"] : ["admin", "super_admin"];
  return user && roles.includes(user.role) ? children : <Navigate to="/" replace />;
}
export default function App() {
  const { loading } = useAuth();
  if (loading) return <Box sx={{ height: "100vh", display: "grid", placeItems: "center" }}><CircularProgress /></Box>;
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/q/:token" element={<MobilePassPage />} />
      <Route element={<Protected />}>
        <Route index element={<PersonalDashboardPage />} />
        <Route path="visits/new" element={<VisitFormPage />} />
        <Route path="visits" element={<VisitsPage />} />
        <Route path="templates" element={<TemplatesPage />} />
        <Route path="lobby" element={<LobbyGuard><LobbyPage /></LobbyGuard>} />
        <Route path="lobby/scan" element={<LobbyGuard><ScannerPage /></LobbyGuard>} />
        <Route path="lobby/walk-in" element={<LobbyGuard><VisitFormPage walkIn /></LobbyGuard>} />
        <Route path="admin/:section" element={<AdminGuard><AdminPage /></AdminGuard>} />
        <Route path="admin/audit" element={<AdminGuard audit><AdminPage fixedSection="audit" /></AdminGuard>} />
        <Route path="admin/settings" element={<AdminGuard><SettingsPage /></AdminGuard>} />
        <Route path="profile/keys" element={<KeysPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
