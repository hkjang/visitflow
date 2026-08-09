import { useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { AppBar, Avatar, Box, Chip, Divider, Drawer, IconButton, List, ListItemButton, ListItemIcon, ListItemText, Menu, MenuItem, Stack, Toolbar, Tooltip, Typography, useMediaQuery, useTheme } from "@mui/material";
import MenuRounded from "@mui/icons-material/MenuRounded";
import HomeRounded from "@mui/icons-material/HomeRounded";
import AddCircleOutlineRounded from "@mui/icons-material/AddCircleOutlineRounded";
import EventNoteRounded from "@mui/icons-material/EventNoteRounded";
import BookmarkBorderRounded from "@mui/icons-material/BookmarkBorderRounded";
import SpaceDashboardRounded from "@mui/icons-material/SpaceDashboardRounded";
import QrCodeScannerRounded from "@mui/icons-material/QrCodeScannerRounded";
import PersonAddAltRounded from "@mui/icons-material/PersonAddAltRounded";
import AdminPanelSettingsRounded from "@mui/icons-material/AdminPanelSettingsRounded";
import GroupsRounded from "@mui/icons-material/GroupsRounded";
import DomainRounded from "@mui/icons-material/DomainRounded";
import BarChartRounded from "@mui/icons-material/BarChartRounded";
import PolicyRounded from "@mui/icons-material/PolicyRounded";
import SettingsRounded from "@mui/icons-material/SettingsRounded";
import KeyRounded from "@mui/icons-material/KeyRounded";
import LogoutRounded from "@mui/icons-material/LogoutRounded";
import InfoOutlined from "@mui/icons-material/InfoOutlined";
import { Logo } from "./Logo";
import { useAuth } from "../auth";

const drawerWidth = 264;
const roleNames: Record<string, string> = { user: "방문 요청자", lobby: "로비 담당자", dept_manager: "부서 관리자", security: "보안 담당자", auditor: "감사 담당자", admin: "서비스 관리자", super_admin: "최고 관리자" };
type Nav = { label: string; path: string; icon: React.ReactNode };

export function AppShell() {
  const { user, version, config, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const theme = useTheme();
  const mobile = useMediaQuery(theme.breakpoints.down("md"));
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [profileAnchor, setProfileAnchor] = useState<HTMLElement | null>(null);
  const lobby = user && ["lobby", "security", "admin", "super_admin"].includes(user.role);
  const admin = user && ["admin", "super_admin"].includes(user.role);
  const auditor = user && ["auditor", "security", "admin", "super_admin"].includes(user.role);
  const groups = useMemo(() => {
    const personal: Nav[] = [
      { label: "Dashboard", path: "/", icon: <HomeRounded /> },
      { label: "방문 신청", path: "/visits/new", icon: <AddCircleOutlineRounded /> },
      { label: "내 방문 일정", path: "/visits", icon: <EventNoteRounded /> },
      { label: "방문 템플릿", path: "/templates", icon: <BookmarkBorderRounded /> },
    ];
    const lobbyItems: Nav[] = lobby ? [
      { label: "Lobby Dashboard", path: "/lobby", icon: <SpaceDashboardRounded /> },
      { label: "QR Scan", path: "/lobby/scan", icon: <QrCodeScannerRounded /> },
      { label: "현장 방문 등록", path: "/lobby/walk-in", icon: <PersonAddAltRounded /> },
    ] : [];
    const adminItems: Nav[] = admin ? [
      { label: "관리 Dashboard", path: "/admin/dashboard", icon: <AdminPanelSettingsRounded /> },
      { label: "방문 · 방문자", path: "/admin/visits", icon: <GroupsRounded /> },
      { label: "조직 · 사업장", path: "/admin/resources", icon: <DomainRounded /> },
      { label: "통계 · 알림", path: "/admin/statistics", icon: <BarChartRounded /> },
    ] : [];
    if (auditor) adminItems.push({ label: "Audit Log", path: "/admin/audit", icon: <PolicyRounded /> });
    if (admin) adminItems.push({ label: "시스템 설정", path: "/admin/settings", icon: <SettingsRounded /> }, { label: "API / MCP", path: "/admin/api", icon: <KeyRounded /> });
    return [{ title: "개인 서비스", items: personal }, { title: "로비 서비스", items: lobbyItems }, { title: "관리자", items: adminItems }].filter((g) => g.items.length);
  }, [admin, auditor, lobby]);
  const activeLabel = groups.flatMap((g) => g.items).find((x) => x.path === location.pathname)?.label ?? (location.pathname.startsWith("/admin/") ? "관리자" : "VisitFlow");
  const drawer = (
    <Box sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <Box sx={{ p: 2.5 }}><Logo /></Box>
      <Divider />
      <Box sx={{ flex: 1, overflow: "auto", py: 1.5 }}>
        {groups.map((group) => <Box key={group.title} sx={{ mb: 1.5 }}><Typography variant="overline" sx={{ px: 2.5, color: "text.secondary", fontWeight: 800, letterSpacing: ".09em" }}>{group.title}</Typography><List dense>{group.items.map((item) => { const selected = item.path === "/" ? location.pathname === "/" : location.pathname.startsWith(item.path); return <ListItemButton key={item.path} selected={selected} onClick={() => { navigate(item.path); setDrawerOpen(false); }} sx={{ mx: 1.25, my: .25, borderRadius: 2.5, "&.Mui-selected": { bgcolor: "rgba(23,107,91,.11)", color: "primary.dark" } }}><ListItemIcon sx={{ minWidth: 38, color: "inherit" }}>{item.icon}</ListItemIcon><ListItemText primary={item.label} primaryTypographyProps={{ fontWeight: selected ? 750 : 580, fontSize: 14 }} /></ListItemButton>; })}</List></Box>)}
      </Box>
      <Box sx={{ p: 2, m: 1.5, borderRadius: 3, bgcolor: "#EDF4F1" }}><Typography variant="caption" color="text.secondary">현재 운영 버전</Typography><Typography variant="body2" fontWeight={800}>VisitFlow v{version?.version ?? "dev"}</Typography></Box>
    </Box>
  );
  return (
    <Box sx={{ display: "flex", minHeight: "100vh" }}>
      <AppBar position="fixed" elevation={0} sx={{ width: { md: `calc(100% - ${drawerWidth}px)` }, ml: { md: `${drawerWidth}px` }, bgcolor: "rgba(255,255,255,.9)", backdropFilter: "blur(16px)", color: "text.primary", borderBottom: "1px solid #E1E9E6" }}>
        <Toolbar><IconButton onClick={() => setDrawerOpen(true)} sx={{ display: { md: "none" }, mr: 1 }}><MenuRounded /></IconButton><Typography sx={{ flex: 1, fontWeight: 780 }}>{activeLabel}</Typography><Stack direction="row" alignItems="center" spacing={1.5}><Chip size="small" variant="outlined" label={roleNames[user?.role ?? "user"]} sx={{ display: { xs: "none", sm: "flex" } }} /><Tooltip title="프로필 메뉴"><IconButton onClick={(e) => setProfileAnchor(e.currentTarget)}><Avatar sx={{ width: 36, height: 36, bgcolor: "primary.main", fontSize: 15 }}>{user?.displayName?.slice(0, 1)}</Avatar></IconButton></Tooltip></Stack></Toolbar>
      </AppBar>
      <Box component="nav" sx={{ width: { md: drawerWidth }, flexShrink: { md: 0 } }}><Drawer variant={mobile ? "temporary" : "permanent"} open={mobile ? drawerOpen : true} onClose={() => setDrawerOpen(false)} ModalProps={{ keepMounted: true }} sx={{ "& .MuiDrawer-paper": { width: drawerWidth, borderRight: "1px solid #E1E9E6" } }}>{drawer}</Drawer></Box>
      <Box component="main" sx={{ flex: 1, minWidth: 0, pt: "64px" }}><Outlet /></Box>
      <Menu anchorEl={profileAnchor} open={Boolean(profileAnchor)} onClose={() => setProfileAnchor(null)} slotProps={{ paper: { sx: { minWidth: 260, mt: 1 } } }}>
        <Box sx={{ px: 2, py: 1.5 }}><Typography fontWeight={800}>{user?.displayName}</Typography><Typography variant="body2" color="text.secondary">{user?.email || user?.username}</Typography></Box><Divider />
        <MenuItem onClick={() => { navigate("/profile/keys"); setProfileAnchor(null); }}><KeyRounded fontSize="small" sx={{ mr: 1.5 }} />내 API 키</MenuItem>
        <MenuItem disabled><InfoOutlined fontSize="small" sx={{ mr: 1.5 }} /><Box><Typography variant="body2">{config?.serviceName ?? "VisitFlow"} v{version?.version ?? "dev"}</Typography><Typography variant="caption" color="text.secondary">commit {version?.commit?.slice(0, 12) ?? "unknown"}</Typography></Box></MenuItem>
        <Divider /><MenuItem onClick={() => void logout()}><LogoutRounded fontSize="small" sx={{ mr: 1.5 }} />로그아웃</MenuItem>
      </Menu>
    </Box>
  );
}
