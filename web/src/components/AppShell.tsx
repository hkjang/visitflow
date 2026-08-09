import { useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  AppBar,
  Avatar,
  Box,
  Divider,
  Drawer,
  IconButton,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Menu,
  MenuItem,
  Toolbar,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import MenuRounded from "@mui/icons-material/MenuRounded";
import MapRounded from "@mui/icons-material/MapRounded";
import RuleRounded from "@mui/icons-material/RuleRounded";
import LayersRounded from "@mui/icons-material/LayersRounded";
import BadgeRounded from "@mui/icons-material/BadgeRounded";
import HistoryRounded from "@mui/icons-material/HistoryRounded";
import SettingsRounded from "@mui/icons-material/SettingsRounded";
import KeyRounded from "@mui/icons-material/KeyRounded";
import ManageAccountsRounded from "@mui/icons-material/ManageAccountsRounded";
import LogoutRounded from "@mui/icons-material/LogoutRounded";
import { Logo } from "./Logo";
import { useAuth } from "../auth";

const drawerWidth = 244;
const roleName: Record<string, string> = {
  employee: "직원",
  department_manager: "부서 관리자",
  seat_manager: "좌석 관리자",
  system_admin: "시스템 관리자",
};
export function AppShell() {
  const { user, version, logout } = useAuth(),
    navigate = useNavigate(),
    location = useLocation(),
    theme = useTheme(),
    mobile = useMediaQuery(theme.breakpoints.down("md"));
  const [drawerOpen, setDrawerOpen] = useState(false),
    [anchor, setAnchor] = useState<HTMLElement | null>(null);
  if (!user) return null;
  const manager = user.role === "seat_manager" || user.role === "system_admin",
    admin = user.role === "system_admin";
  const items = [
    { label: "좌석맵", path: "/", icon: <MapRounded /> },
    ...(manager
      ? [
          { label: "처리필요", path: "/admin/actions", icon: <RuleRounded /> },
          {
            label: "도면 · 좌석",
            path: "/admin/maps",
            icon: <LayersRounded />,
          },
          { label: "직원", path: "/admin/employees", icon: <BadgeRounded /> },
          {
            label: "변경 이력",
            path: "/admin/history",
            icon: <HistoryRounded />,
          },
        ]
      : []),
    ...(admin
      ? [
          {
            label: "사용자 권한",
            path: "/admin/users",
            icon: <ManageAccountsRounded />,
          },
          {
            label: "시스템 설정",
            path: "/admin/settings",
            icon: <SettingsRounded />,
          },
        ]
      : []),
  ];
  const drawer = (
    <Box
      sx={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        bgcolor: "#071A2B",
        color: "white",
      }}
    >
      <Box sx={{ px: 2.5, height: 72, display: "flex", alignItems: "center" }}>
        <Logo inverse />
      </Box>
      <Typography
        variant="overline"
        sx={{ px: 2.5, color: "rgba(255,255,255,.43)", letterSpacing: 1.4 }}
      >
        WORKSPACE
      </Typography>
      <List sx={{ px: 1.25, pt: 0.7 }}>
        {items.map((item) => (
          <ListItemButton
            key={item.path}
            selected={location.pathname === item.path}
            onClick={() => {
              navigate(item.path);
              setDrawerOpen(false);
            }}
            sx={{
              borderRadius: 2.5,
              mb: 0.45,
              color: "rgba(255,255,255,.72)",
              "& .MuiListItemIcon-root": { color: "inherit", minWidth: 39 },
              "&.Mui-selected": {
                bgcolor: "rgba(78,201,211,.15)",
                color: "#78E1E8",
                "&:hover": { bgcolor: "rgba(78,201,211,.2)" },
              },
              "&:hover": { bgcolor: "rgba(255,255,255,.07)", color: "white" },
            }}
          >
            <ListItemIcon>{item.icon}</ListItemIcon>
            <ListItemText
              primary={item.label}
              primaryTypographyProps={{ fontSize: 14, fontWeight: 650 }}
            />
          </ListItemButton>
        ))}
      </List>
      <Box sx={{ mt: "auto", p: 2 }}>
        <Box
          sx={{
            border: "1px solid rgba(255,255,255,.09)",
            borderRadius: 3,
            p: 1.5,
            bgcolor: "rgba(255,255,255,.035)",
          }}
        >
          <Typography variant="caption" color="rgba(255,255,255,.45)">
            OFFLINE READY
          </Typography>
          <Typography
            variant="body2"
            sx={{ mt: 0.3, color: "rgba(255,255,255,.8)" }}
          >
            사내 데이터는 망 밖으로 전송되지 않습니다.
          </Typography>
        </Box>
      </Box>
    </Box>
  );
  return (
    <Box sx={{ display: "flex", minHeight: "100vh" }}>
      <AppBar
        position="fixed"
        elevation={0}
        color="inherit"
        sx={{
          ml: { md: `${drawerWidth}px` },
          width: { md: `calc(100% - ${drawerWidth}px)` },
          borderBottom: "1px solid #E3EAEE",
          bgcolor: "rgba(255,255,255,.92)",
          backdropFilter: "blur(12px)",
        }}
      >
        <Toolbar sx={{ height: 72 }}>
          {mobile && (
            <IconButton
              onClick={() => setDrawerOpen(true)}
              edge="start"
              sx={{ mr: 1 }}
            >
              <MenuRounded />
            </IconButton>
          )}
          {mobile && <Logo compact />}
          <Box sx={{ flex: 1 }} />
          <Tooltip title="프로필 및 버전">
            <IconButton
              onClick={(e) => setAnchor(e.currentTarget)}
              sx={{ p: 0.5, borderRadius: 3 }}
            >
              <Avatar
                sx={{
                  width: 36,
                  height: 36,
                  bgcolor: "primary.main",
                  fontSize: 15,
                  fontWeight: 750,
                }}
              >
                {user.displayName.slice(0, 1).toUpperCase()}
              </Avatar>
            </IconButton>
          </Tooltip>
          <Box
            onClick={(e) => setAnchor(e.currentTarget)}
            sx={{
              ml: 1,
              display: { xs: "none", sm: "block" },
              cursor: "pointer",
            }}
          >
            <Typography variant="body2" fontWeight={750}>
              {user.displayName}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {roleName[user.role]}
            </Typography>
          </Box>
          <Menu
            anchorEl={anchor}
            open={Boolean(anchor)}
            onClose={() => setAnchor(null)}
            slotProps={{
              paper: {
                sx: { mt: 1, minWidth: 250, border: "1px solid #E3EAEE" },
              },
            }}
          >
            <Box sx={{ px: 2, py: 1.3 }}>
              <Typography fontWeight={750}>{user.displayName}</Typography>
              <Typography variant="body2" color="text.secondary">
                {user.email || user.username}
              </Typography>
            </Box>
            <Divider />
            <MenuItem
              onClick={() => {
                navigate("/profile/keys");
                setAnchor(null);
              }}
            >
              <ListItemIcon>
                <KeyRounded fontSize="small" />
              </ListItemIcon>
              내 API 키
            </MenuItem>
            <MenuItem disabled sx={{ opacity: "1!important" }}>
              <ListItemText
                primary={`SeatOn ${version?.version ?? "dev"}`}
                secondary={
                  version?.commit && version.commit !== "unknown"
                    ? `commit ${version.commit.slice(0, 8)}`
                    : "오프라인 배포판"
                }
              />
            </MenuItem>
            <Divider />
            <MenuItem onClick={() => void logout()}>
              <ListItemIcon>
                <LogoutRounded fontSize="small" />
              </ListItemIcon>
              로그아웃
            </MenuItem>
          </Menu>
        </Toolbar>
      </AppBar>
      <Box
        component="nav"
        sx={{ width: { md: drawerWidth }, flexShrink: { md: 0 } }}
      >
        <Drawer
          variant={mobile ? "temporary" : "permanent"}
          open={mobile ? drawerOpen : true}
          onClose={() => setDrawerOpen(false)}
          ModalProps={{ keepMounted: true }}
          sx={{ "& .MuiDrawer-paper": { width: drawerWidth, border: 0 } }}
        >
          {drawer}
        </Drawer>
      </Box>
      <Box component="main" sx={{ flex: 1, minWidth: 0, pt: "72px" }}>
        <Outlet />
      </Box>
    </Box>
  );
}
