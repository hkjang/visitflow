import { useMemo, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { AppBar, Avatar, Box, Button, Chip, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Drawer, IconButton, List, ListItemButton, ListItemIcon, ListItemText, Menu, MenuItem, Stack, TextField, Toolbar, Tooltip, Typography, useMediaQuery, useTheme } from "@mui/material";
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
import EditOutlined from "@mui/icons-material/EditOutlined";
import HourglassTopRounded from "@mui/icons-material/HourglassTopRounded";
import MenuBookRounded from "@mui/icons-material/MenuBookRounded";
import NotificationsActiveRounded from "@mui/icons-material/NotificationsActiveRounded";
import { Logo } from "./Logo";
import { useAuth } from "../auth";
import { api, patchJSON } from "../api";
import type { ReferenceData } from "../types";

const drawerWidth = 264;
const menuScrollbar = {
  scrollbarWidth: "thin",
  scrollbarColor: "#8BAEA5 transparent",
  "&::-webkit-scrollbar": { width: 8, height: 8 },
  "&::-webkit-scrollbar-track": { background: "transparent", marginBlock: 8 },
  "&::-webkit-scrollbar-thumb": { backgroundColor: "#8BAEA5", borderRadius: 12, border: "2px solid transparent", backgroundClip: "padding-box" },
  "&::-webkit-scrollbar-thumb:hover": { backgroundColor: "#5F8F83" },
} as const;
const roleNames: Record<string, string> = { user: "방문 요청자", lobby: "로비 담당자", dept_manager: "부서 관리자", security: "보안 담당자", auditor: "감사 담당자", admin: "서비스 관리자", super_admin: "최고 관리자" };
type Nav = { label: string; path: string; icon: React.ReactNode };

export function AppShell() {
  const { user, version, config, logout, reload } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const theme = useTheme();
  const mobile = useMediaQuery(theme.breakpoints.down("md"));
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [profileAnchor, setProfileAnchor] = useState<HTMLElement | null>(null);
  const [profileOpen, setProfileOpen] = useState(false);
  const [profileName, setProfileName] = useState(user?.displayName ?? "");
  const [profilePhone, setProfilePhone] = useState("");
  const [profileDepartment, setProfileDepartment] = useState(user?.departmentId ?? "");
  const [reference, setReference] = useState<ReferenceData | null>(null);
  const lobby = user && ["lobby", "security", "admin", "super_admin"].includes(user.role);
  const admin = user && ["admin", "super_admin"].includes(user.role);
  const security = user?.role === "security";
  const auditor = user && ["auditor", "security", "admin", "super_admin"].includes(user.role);
  const groups = useMemo(() => {
    const personal: Nav[] = [
      { label: "Dashboard", path: "/", icon: <HomeRounded /> },
      { label: "방문 신청", path: "/visits/new", icon: <AddCircleOutlineRounded /> },
      { label: "내 방문 일정", path: "/visits", icon: <EventNoteRounded /> },
      { label: "방문 템플릿", path: "/templates", icon: <BookmarkBorderRounded /> },
      { label: "사용자 가이드", path: "/guides", icon: <MenuBookRounded /> },
    ];
    if (user && ["dept_manager", "security", "admin", "super_admin"].includes(user.role)) personal.push({ label: "방문 승인", path: "/approvals", icon: <HourglassTopRounded /> });
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
      { label: "문자 API · 발송 규칙", path: "/admin/notification-settings", icon: <NotificationsActiveRounded /> },
      { label: "사용자 가이드 관리", path: "/admin/guides", icon: <MenuBookRounded /> },
    ] : security ? [{ label: "방문 · Watch List", path: "/admin/visits", icon: <GroupsRounded /> }] : [];
    if (auditor) adminItems.push({ label: "Audit Log", path: "/admin/audit", icon: <PolicyRounded /> });
    if (admin) adminItems.push({ label: "시스템 설정", path: "/admin/settings", icon: <SettingsRounded /> }, { label: "API / MCP", path: "/admin/api", icon: <KeyRounded /> });
    return [{ title: "개인 서비스", items: personal }, { title: "로비 서비스", items: lobbyItems }, { title: "관리자", items: adminItems }].filter((g) => g.items.length);
  }, [admin, auditor, lobby, security, user]);
  const activeLabel = groups.flatMap((g) => g.items).find((x) => x.path === location.pathname)?.label ?? (location.pathname.startsWith("/admin/") ? "관리자" : "VisitFlow");
  const openProfile = async () => {
    setProfileAnchor(null); setProfileName(user?.displayName ?? ""); setProfileDepartment(user?.departmentId ?? ""); setProfilePhone(""); setProfileOpen(true);
    if (!reference) setReference(await api<ReferenceData>("/api/v1/reference-data"));
  };
  const saveProfile = async () => {
    await patchJSON("/api/v1/profile", { displayName: profileName, phone: profilePhone, departmentId: profileDepartment });
    setProfileOpen(false); await reload();
  };
  const drawer = (
    <Box sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <Box sx={{ p: 2.5 }}><Logo /></Box>
      <Divider />
      <Box sx={{ flex: 1, overflowY: "auto", overflowX: "hidden", py: 1.5, ...menuScrollbar }}>
        {groups.map((group) => <Box key={group.title} sx={{ mb: 1.5 }}><Typography variant="overline" sx={{ px: 2.5, color: "text.secondary", fontWeight: 800, letterSpacing: ".09em" }}>{group.title}</Typography><List dense>{group.items.map((item) => { const selected = item.path === "/" ? location.pathname === "/" : location.pathname.startsWith(item.path); return <ListItemButton key={item.path} selected={selected} onClick={() => { navigate(item.path); setDrawerOpen(false); }} sx={{ mx: 1.25, my: .25, borderRadius: 2.5, minHeight: 44, "&.Mui-selected": { bgcolor: "rgba(23,107,91,.11)", color: "primary.dark" } }}><ListItemIcon sx={{ minWidth: 38, color: "inherit" }}>{item.icon}</ListItemIcon><ListItemText primary={item.label} primaryTypographyProps={{ fontWeight: selected ? 750 : 580, fontSize: 15 }} /></ListItemButton>; })}</List></Box>)}
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
      <Menu anchorEl={profileAnchor} open={Boolean(profileAnchor)} onClose={() => setProfileAnchor(null)} slotProps={{ paper: { sx: { minWidth: 280, maxHeight: "min(520px, calc(100vh - 32px))", overflowY: "auto", mt: 1, ...menuScrollbar } } }}>
        <Box sx={{ px: 2, py: 1.5 }}><Typography fontWeight={800}>{user?.displayName}</Typography><Typography variant="body2" color="text.secondary">{user?.email || user?.username}</Typography></Box><Divider />
        <MenuItem onClick={() => void openProfile()}><EditOutlined fontSize="small" sx={{ mr: 1.5 }} />프로필 · 연락처</MenuItem>
        <MenuItem onClick={() => { navigate("/profile/keys"); setProfileAnchor(null); }}><KeyRounded fontSize="small" sx={{ mr: 1.5 }} />내 API 키</MenuItem>
        <MenuItem disabled><InfoOutlined fontSize="small" sx={{ mr: 1.5 }} /><Box><Typography variant="body2">{config?.serviceName ?? "VisitFlow"} v{version?.version ?? "dev"}</Typography><Typography variant="caption" color="text.secondary">commit {version?.commit?.slice(0, 12) ?? "unknown"}</Typography></Box></MenuItem>
        <Divider /><MenuItem onClick={() => void logout()}><LogoutRounded fontSize="small" sx={{ mr: 1.5 }} />로그아웃</MenuItem>
      </Menu>
      <Dialog open={profileOpen} onClose={() => setProfileOpen(false)} fullWidth maxWidth="sm"><DialogTitle>프로필 · 도착 알림 연락처</DialogTitle><DialogContent><Stack spacing={2} mt={1}><TextField label="표시 이름" value={profileName} onChange={(e) => setProfileName(e.target.value)} /><TextField label="휴대전화" value={profilePhone} onChange={(e) => setProfilePhone(e.target.value)} placeholder="010-0000-0000" helperText="방문자 체크인 시 담당자 SMS 알림에 사용하며 암호화 저장됩니다. 비워 두면 기존 연락처가 삭제됩니다." /><TextField select label="소속 부서" value={profileDepartment} onChange={(e) => setProfileDepartment(e.target.value)}><MenuItem value="">미지정</MenuItem>{reference?.departments.map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField></Stack></DialogContent><DialogActions><Button onClick={() => setProfileOpen(false)}>취소</Button><Button variant="contained" disabled={!profileName} onClick={() => void saveProfile()}>저장</Button></DialogActions></Dialog>
    </Box>
  );
}
