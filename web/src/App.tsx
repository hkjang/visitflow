import { lazy, Suspense } from "react";
import { CircularProgress, Box } from "@mui/material";
import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { AppShell } from "./components/AppShell";
import { PasswordChangeGate } from "./components/PasswordChangeGate";
import { LoginPage } from "./pages/LoginPage";
import { PersonalDashboardPage } from "./pages/PersonalDashboardPage";

// Every page except the login and dashboard is its own chunk. Visitor-facing
// pages (pass, self-registration) and the kiosk load on phones and tablets that
// must never download the admin console.
const VisitFormPage = lazy(() => import("./pages/VisitFormPage").then((m) => ({ default: m.VisitFormPage })));
const VisitsPage = lazy(() => import("./pages/VisitsPage").then((m) => ({ default: m.VisitsPage })));
const TemplatesPage = lazy(() => import("./pages/TemplatesPage").then((m) => ({ default: m.TemplatesPage })));
const LobbyPage = lazy(() => import("./pages/LobbyPage").then((m) => ({ default: m.LobbyPage })));
const ScannerPage = lazy(() => import("./pages/ScannerPage").then((m) => ({ default: m.ScannerPage })));
const AdminPage = lazy(() => import("./pages/AdminPage").then((m) => ({ default: m.AdminPage })));
const SettingsPage = lazy(() => import("./pages/SettingsPage").then((m) => ({ default: m.SettingsPage })));
const KeysPage = lazy(() => import("./pages/KeysPage").then((m) => ({ default: m.KeysPage })));
const MobilePassPage = lazy(() => import("./pages/MobilePassPage").then((m) => ({ default: m.MobilePassPage })));
const ApprovalsPage = lazy(() => import("./pages/ApprovalsPage").then((m) => ({ default: m.ApprovalsPage })));
const GuidePage = lazy(() => import("./pages/GuidePage").then((m) => ({ default: m.GuidePage })));
const NotificationSettingsPage = lazy(() => import("./pages/NotificationSettingsPage").then((m) => ({ default: m.NotificationSettingsPage })));
const SelfRegistrationPage = lazy(() => import("./pages/SelfRegistrationPage").then((m) => ({ default: m.SelfRegistrationPage })));
const KioskPage = lazy(() => import("./pages/KioskPage").then((m) => ({ default: m.KioskPage })));
const RosterPage = lazy(() => import("./pages/RosterPage").then((m) => ({ default: m.RosterPage })));

const Spinner = () => <Box sx={{ minHeight: "60vh", display: "grid", placeItems: "center" }}><CircularProgress /></Box>;

function Protected() {
  const { user } = useAuth();
  if (!user) return <Navigate to="/login" replace />;
  // A temporary password only unlocks the change-password dialog.
  return user.mustChangePassword ? <PasswordChangeGate /> : <AppShell />;
}
function LobbyGuard({ children }: { children: React.ReactNode }) {
  const { user } = useAuth();
  return user && ["lobby", "security", "admin", "super_admin"].includes(user.role) ? children : <Navigate to="/" replace />;
}
function AdminGuard({ children, audit = false, security = false }: { children: React.ReactNode; audit?: boolean; security?: boolean }) {
  const { user } = useAuth();
  const roles = audit ? ["auditor", "security", "admin", "super_admin"] : security ? ["security", "admin", "super_admin"] : ["admin", "super_admin"];
  return user && roles.includes(user.role) ? children : <Navigate to="/" replace />;
}
function ApprovalGuard({ children }: { children: React.ReactNode }) {
  const { user } = useAuth();
  return user && (["dept_manager", "security", "admin", "super_admin"].includes(user.role) || user.approvalDelegate) ? children : <Navigate to="/" replace />;
}
export default function App() {
  const { loading } = useAuth();
  if (loading) return <Box sx={{ height: "100vh", display: "grid", placeItems: "center" }}><CircularProgress /></Box>;
  return (
    <Suspense fallback={<Spinner />}>
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/q/:token" element={<MobilePassPage />} />
      <Route path="/r/:token" element={<SelfRegistrationPage />} />
      <Route path="/kiosk" element={<KioskPage />} />
      <Route element={<Protected />}>
        <Route index element={<PersonalDashboardPage />} />
        <Route path="visits/new" element={<VisitFormPage />} />
        <Route path="visits" element={<VisitsPage />} />
        <Route path="templates" element={<TemplatesPage />} />
        <Route path="guides" element={<GuidePage />} />
        <Route path="approvals" element={<ApprovalGuard><ApprovalsPage /></ApprovalGuard>} />
        <Route path="lobby" element={<LobbyGuard><LobbyPage /></LobbyGuard>} />
        <Route path="lobby/scan" element={<LobbyGuard><ScannerPage /></LobbyGuard>} />
        <Route path="lobby/roster" element={<LobbyGuard><RosterPage /></LobbyGuard>} />
        <Route path="lobby/walk-in" element={<LobbyGuard><VisitFormPage walkIn /></LobbyGuard>} />
        <Route path="admin/visits" element={<AdminGuard security><AdminPage fixedSection="visits" /></AdminGuard>} />
        <Route path="admin/guides" element={<AdminGuard><GuidePage admin /></AdminGuard>} />
        <Route path="admin/notification-settings" element={<AdminGuard><NotificationSettingsPage /></AdminGuard>} />
        <Route path="admin/:section" element={<AdminGuard><AdminPage /></AdminGuard>} />
        <Route path="admin/audit" element={<AdminGuard audit><AdminPage fixedSection="audit" /></AdminGuard>} />
        <Route path="admin/settings" element={<AdminGuard><SettingsPage /></AdminGuard>} />
        <Route path="profile/keys" element={<KeysPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
    </Suspense>
  );
}
