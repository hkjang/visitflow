import { useState } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { Alert, Box, Button, Card, CardContent, Chip, Divider, Stack, TextField, Typography } from "@mui/material";
import LoginRounded from "@mui/icons-material/LoginRounded";
import ShieldOutlined from "@mui/icons-material/ShieldOutlined";
import QrCode2Rounded from "@mui/icons-material/QrCode2Rounded";
import NotificationsActiveOutlined from "@mui/icons-material/NotificationsActiveOutlined";
import { Logo } from "../components/Logo";
import { useAuth } from "../auth";
import { postJSON } from "../api";

export function LoginPage() {
  const { user, config, version, login } = useAuth();
  const location = useLocation();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState(new URLSearchParams(location.search).get("error") ?? "");
  const [busy, setBusy] = useState(false);
  const [forgot, setForgot] = useState(false); const [identifier, setIdentifier] = useState(""); const [resetNotice, setResetNotice] = useState("");
  if (user) return <Navigate to="/" replace />;
  const requestReset = async (event: React.FormEvent) => {
    event.preventDefault(); setBusy(true); setError(""); setResetNotice("");
    try { const result = await postJSON<{ message: string }>("/api/v1/auth/password-reset/request", { identifier }); setResetNotice(result.message); } catch (e) { setError(e instanceof Error ? e.message : "요청을 처리하지 못했습니다"); } finally { setBusy(false); }
  };
  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); setBusy(true); setError("");
    try { await login(username, password); } catch (e) { setError(e instanceof Error ? e.message : "로그인하지 못했습니다"); } finally { setBusy(false); }
  };
  return (
    <Box sx={{ minHeight: "100vh", display: "grid", gridTemplateColumns: { xs: "1fr", lg: "1.12fr .88fr" }, bgcolor: "#F5F8F6" }}>
      <Box sx={{ display: { xs: "none", lg: "flex" }, flexDirection: "column", justifyContent: "space-between", p: 7, color: "white", background: "radial-gradient(circle at 15% 15%,rgba(255,255,255,.16),transparent 32%),linear-gradient(145deg,#0C473E,#176B5B 58%,#347E70)" }}>
        <Logo inverse />
        <Box sx={{ maxWidth: 650 }}><Chip label="ENTERPRISE VISITOR OPERATIONS" sx={{ mb: 3, bgcolor: "rgba(255,255,255,.13)", color: "white" }} /><Typography variant="h2" sx={{ fontWeight: 850, fontSize: 60, lineHeight: 1.05, letterSpacing: "-.055em" }}>방문의 시작부터<br />안전한 퇴실까지.</Typography><Typography sx={{ mt: 3, fontSize: 19, color: "rgba(255,255,255,.78)", lineHeight: 1.75 }}>신청 · 승인 · QR · 로비 · 알림 · 감사 추적을<br />한 흐름으로 운영하는 오프라인 방문자 관리 플랫폼입니다.</Typography><Stack direction="row" spacing={4} mt={5}>{[[<QrCode2Rounded />, "서버 검증 QR"], [<NotificationsActiveOutlined />, "도착 즉시 알림"], [<ShieldOutlined />, "개인정보 보호"]].map(([icon, label]) => <Stack key={String(label)} direction="row" spacing={1} alignItems="center">{icon}<Typography fontWeight={700}>{label}</Typography></Stack>)}</Stack></Box>
        <Typography variant="caption" sx={{ color: "rgba(255,255,255,.58)" }}>완전 오프라인 운영 · PostgreSQL · Keycloak OIDC</Typography>
      </Box>
      <Box sx={{ display: "grid", placeItems: "center", p: { xs: 2, sm: 5 } }}>
        <Card sx={{ width: "100%", maxWidth: 470, borderRadius: 4 }}><CardContent sx={{ p: { xs: 3, sm: 5 } }}><Box sx={{ display: { lg: "none" }, mb: 4 }}><Logo /></Box><Typography variant="h4">다시 만나 반갑습니다</Typography><Typography color="text.secondary" sx={{ mt: 1, mb: 4 }}>{config?.companyName ? `${config.companyName} ` : ""}{config?.serviceName ?? "VisitFlow"}에 로그인하세요.</Typography>{error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
          {config?.oidcEnabled && <><Button fullWidth size="large" variant="contained" startIcon={<ShieldOutlined />} onClick={() => { window.location.href = "/api/v1/auth/oidc/start"; }}>Keycloak SSO로 계속</Button>{config.localEnabled && <Divider sx={{ my: 3 }}>또는 관리자 계정</Divider>}</>}
          {config?.localEnabled && !forgot && <Box component="form" onSubmit={submit}><Stack spacing={2}><TextField autoFocus fullWidth label="아이디" autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} /><TextField fullWidth label="비밀번호" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} /><Button type="submit" fullWidth size="large" variant="contained" endIcon={<LoginRounded />} disabled={busy || !username || !password}>{busy ? "로그인 중…" : "로그인"}</Button>{config.passwordResetEnabled && <Button size="small" onClick={() => { setForgot(true); setError(""); setIdentifier(username); }}>비밀번호를 잊으셨나요?</Button>}</Stack></Box>}
          {config?.localEnabled && forgot && <Box component="form" onSubmit={requestReset}><Stack spacing={2}><Typography variant="subtitle1" fontWeight={750}>비밀번호 재설정</Typography><Typography variant="body2" color="text.secondary">아이디 또는 등록된 이메일을 입력하면 재설정 링크를 메일로 보냅니다. SSO 계정은 Keycloak에서 변경하세요.</Typography>{resetNotice && <Alert severity="success">{resetNotice}</Alert>}<TextField autoFocus fullWidth label="아이디 또는 이메일" value={identifier} onChange={(e) => setIdentifier(e.target.value)} /><Button type="submit" fullWidth size="large" variant="contained" disabled={busy || !identifier.trim()}>{busy ? "요청 중…" : "재설정 메일 보내기"}</Button><Button size="small" onClick={() => { setForgot(false); setResetNotice(""); }}>로그인으로 돌아가기</Button></Stack></Box>}
          {!config?.localEnabled && !config?.oidcEnabled && <Alert severity="warning">사용 가능한 로그인 방식이 없습니다. 부트스트랩 관리자가 설정을 확인해야 합니다.</Alert>}
          <Divider sx={{ my: 3 }} /><Stack direction="row" justifyContent="space-between"><Typography variant="caption" color="text.secondary">VisitFlow v{version?.version ?? "dev"}</Typography><Typography variant="caption" color="text.secondary">{version?.commit?.slice(0, 12) ?? "unknown"}</Typography></Stack>
        </CardContent></Card>
      </Box>
    </Box>
  );
}
