import { useEffect, useState } from "react";
import { Alert, Box, Button, Card, CardContent, CircularProgress, Stack, TextField, Typography } from "@mui/material";
import { Link as RouterLink, useParams } from "react-router-dom";
import { api, postJSON } from "../api";
import { Logo } from "../components/Logo";

// Landing page of the reset link mailed to a local account.
export function ResetPasswordPage() {
  const { token } = useParams();
  const [state, setState] = useState<"checking" | "valid" | "invalid" | "done">("checking");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState(""); const [confirm, setConfirm] = useState("");
  const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!token) { setState("invalid"); return; }
    api<{ valid: boolean; username: string }>(`/api/v1/auth/password-reset/${encodeURIComponent(token)}`).then((r) => { setUsername(r.username); setState("valid"); }).catch(() => setState("invalid"));
  }, [token]);
  const submit = async () => {
    if (!token) return;
    setBusy(true); setError("");
    try { await postJSON(`/api/v1/auth/password-reset/${encodeURIComponent(token)}`, { newPassword: password }); setState("done"); }
    catch (e) { setError(e instanceof Error ? e.message : "비밀번호를 설정하지 못했습니다"); }
    finally { setBusy(false); }
  };
  return <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center", bgcolor: "#F5F8F6", p: 2 }}>
    <Card sx={{ width: "100%", maxWidth: 460, borderRadius: 4 }}><CardContent sx={{ p: 4 }}>
      <Logo />
      {state === "checking" && <Box sx={{ py: 6, display: "grid", placeItems: "center" }}><CircularProgress /></Box>}
      {state === "invalid" && <><Typography variant="h5" sx={{ mt: 3 }}>링크를 사용할 수 없습니다</Typography><Typography color="text.secondary" sx={{ mt: 1, mb: 3 }}>재설정 링크가 만료되었거나 이미 사용되었습니다. 로그인 화면에서 다시 요청하세요.</Typography><Button component={RouterLink} to="/login" variant="contained">로그인으로</Button></>}
      {state === "valid" && <><Typography variant="h5" sx={{ mt: 3 }}>새 비밀번호 설정</Typography><Typography color="text.secondary" sx={{ mt: 1, mb: 3 }}>{username} 계정의 비밀번호를 12자 이상으로 새로 설정합니다. 설정 후 모든 기기에서 다시 로그인해야 합니다.</Typography>{error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}<Stack spacing={2}><TextField type="password" label="새 비밀번호" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} /><TextField type="password" label="새 비밀번호 확인" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} error={Boolean(confirm) && confirm !== password} /><Button size="large" variant="contained" disabled={busy || password.length < 12 || password !== confirm} onClick={() => void submit()}>{busy ? "설정 중…" : "비밀번호 설정"}</Button></Stack></>}
      {state === "done" && <><Typography variant="h5" sx={{ mt: 3 }}>비밀번호를 변경했습니다</Typography><Typography color="text.secondary" sx={{ mt: 1, mb: 3 }}>새 비밀번호로 로그인하세요.</Typography><Button component={RouterLink} to="/login" variant="contained">로그인</Button></>}
    </CardContent></Card>
  </Box>;
}
