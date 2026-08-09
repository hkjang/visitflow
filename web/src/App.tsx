import { CircularProgress, Box } from "@mui/material";
import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { AppShell } from "./components/AppShell";
import { LoginPage } from "./pages/LoginPage";
import { SeatMapPage } from "./pages/SeatMapPage";
import { DashboardPage } from "./pages/DashboardPage";
import { MapsPage } from "./pages/MapsPage";
import { EmployeesPage } from "./pages/EmployeesPage";
import { HistoryPage } from "./pages/HistoryPage";
import { SettingsPage } from "./pages/SettingsPage";
import { KeysPage } from "./pages/KeysPage";
import { UsersPage } from "./pages/UsersPage";

function Protected() {
  const { user } = useAuth();
  return user ? <AppShell /> : <Navigate to="/login" replace />;
}
function Manager({
  children,
  admin = false,
}: {
  children: React.ReactNode;
  admin?: boolean;
}) {
  const { user } = useAuth();
  const ok = admin
    ? user?.role === "system_admin"
    : user?.role === "system_admin" || user?.role === "seat_manager";
  return ok ? children : <Navigate to="/" replace />;
}
export default function App() {
  const { loading } = useAuth();
  if (loading)
    return (
      <Box sx={{ height: "100vh", display: "grid", placeItems: "center" }}>
        <CircularProgress />
      </Box>
    );
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<Protected />}>
        <Route index element={<SeatMapPage />} />
        <Route
          path="admin/actions"
          element={
            <Manager>
              <DashboardPage />
            </Manager>
          }
        />
        <Route
          path="admin/maps"
          element={
            <Manager>
              <MapsPage />
            </Manager>
          }
        />
        <Route
          path="admin/employees"
          element={
            <Manager>
              <EmployeesPage />
            </Manager>
          }
        />
        <Route
          path="admin/history"
          element={
            <Manager>
              <HistoryPage />
            </Manager>
          }
        />
        <Route
          path="admin/settings"
          element={
            <Manager admin>
              <SettingsPage />
            </Manager>
          }
        />
        <Route
          path="admin/users"
          element={
            <Manager admin>
              <UsersPage />
            </Manager>
          }
        />
        <Route path="profile/keys" element={<KeysPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
