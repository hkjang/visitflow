import { useState } from "react";
import { Alert, Box, Button, Card, CardContent, Stack, TextField, Typography } from "@mui/material";
import { postJSON } from "../api";
import { useAuth } from "../auth";
import { Logo } from "./Logo";

// Shown instead of the application while the server refuses every other call
// because the account still carries an administrator-issued temporary password.
export function PasswordChangeGate() {
  const { user, reload, logout } = useAuth();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    setBusy(true); setError("");
    try {
      await postJSON("/api/v1/auth/password", { currentPassword: current, newPassword: next });
      await reload();
    } catch (e) {
      setError(e instanceof Error ? e.message : "비밀번호를 변경하지 못했습니다");
    } finally { setBusy(false); }
  };
  return <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center", bgcolor: "#F5F8F6", p: 2 }}>
    <Card sx={{ width: "100%", maxWidth: 460, borderRadius: 4 }}><CardContent sx={{ p: 4 }}>
      <Logo />
      <Typography variant="h5" sx={{ mt: 3 }}>새 비밀번호를 설정하세요</Typography>
      <Typography color="text.secondary" sx={{ mt: 1, mb: 3 }}>{user?.displayName} 님, 관리자가 발급한 임시 비밀번호는 한 번만 쓸 수 있습니다. 12자 이상의 새 비밀번호로 바꾸면 바로 이용할 수 있습니다.</Typography>
      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      <Stack spacing={2}>
        <TextField type="password" label="임시 비밀번호" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} />
        <TextField type="password" label="새 비밀번호 (12자 이상)" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
        <TextField type="password" label="새 비밀번호 확인" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} error={Boolean(confirm) && confirm !== next} helperText={confirm && confirm !== next ? "새 비밀번호가 일치하지 않습니다" : " "} />
        <Button size="large" variant="contained" disabled={busy || !current || next.length < 12 || next !== confirm} onClick={() => void submit()}>{busy ? "변경 중…" : "비밀번호 변경"}</Button>
        <Button onClick={() => void logout()}>로그아웃</Button>
      </Stack>
    </CardContent></Card>
  </Box>;
}
