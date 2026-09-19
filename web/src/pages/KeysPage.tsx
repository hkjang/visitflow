import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  FormGroup,
  IconButton,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import AutorenewRounded from "@mui/icons-material/AutorenewRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import EditOutlined from "@mui/icons-material/EditOutlined";
import { api, patchJSON, postJSON } from "../api";
type KeyItem = {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  version: number;
  createdAt: string;
  expiresAt?: string;
  lastUsedAt?: string;
  revokedAt?: string;
  graceUntil?: string;
};
export function KeysPage() {
  const [items, setItems] = useState<KeyItem[]>([]),
    [createOpen, setCreateOpen] = useState(false),
    [editing, setEditing] = useState<string | null>(null),
    [name, setName] = useState("내 연동 키"),
    [scopes, setScopes] = useState(["read", "mcp"]),
    [policy, setPolicy] = useState<{ allowedScopes: string[]; defaultExpiryDays: number; maxActiveKeys: number; mcpOAuth?: { enabled: boolean; resource: string; metadataUrl: string; scopes: string[] } }>({ allowedScopes: ["read", "write", "mcp"], defaultExpiryDays: 90, maxActiveKeys: 10 }),
    [revealed, setRevealed] = useState<{ key: string; message: string } | null>(
      null,
    ),
    [error, setError] = useState("");
  const load = () =>
    Promise.all([api<{ items: KeyItem[] }>("/api/v1/api-keys"), api<typeof policy>("/api/v1/api-key-policy")])
      .then(([keys, keyPolicy]) => { setItems(keys.items); setPolicy(keyPolicy); })
      .catch((e) => setError(e.message));
  useEffect(() => {
    void load();
  }, []);
  const save = async () => {
    try {
      if (editing) {
        await patchJSON(`/api/v1/api-keys/${editing}`, { name, scopes });
        setCreateOpen(false); setEditing(null); await load(); return;
      }
      const x = await postJSON<{ key: string; message: string }>(
        "/api/v1/api-keys",
        { name, scopes },
      );
      setCreateOpen(false);
      setRevealed(x);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "키를 만들지 못했습니다");
    }
  };
  const openCreate = () => { setEditing(null); setName("내 연동 키"); setScopes(policy.allowedScopes.filter((scope) => scope === "read" || scope === "mcp")); setCreateOpen(true); };
  const openEdit = (key: KeyItem) => { setEditing(key.id); setName(key.name); setScopes(key.scopes.filter((scope) => policy.allowedScopes.includes(scope))); setCreateOpen(true); };
  const rotate = async (id: string) => {
    try {
      const x = await postJSON<{ key: string; message: string }>(
        `/api/v1/api-keys/${id}/rotate`,
        {},
      );
      setRevealed(x);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "키를 회전하지 못했습니다");
    }
  };
  const revoke = async (id: string) => {
    if (!confirm("이 키를 즉시 폐기할까요? 되돌릴 수 없습니다.")) return;
    await api(`/api/v1/api-keys/${id}`, { method: "DELETE" });
    await load();
  };
  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1100, mx: "auto" }}>
      <Stack direction="row" justifyContent="space-between" mb={3}>
        <Box>
          <Typography variant="h5">내 API 키</Typography>
          <Typography color="text.secondary">
            REST API와 MCP에 사용하는 개인별 키를 생성하고 주기적으로
            회전합니다.
          </Typography>
        </Box>
        <Button
          variant="contained"
          startIcon={<AddRounded />}
          onClick={openCreate}
        >
          키 만들기
        </Button>
      </Stack>
      {error && (
        <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}
      <Alert severity="info" sx={{ mb: 2 }}>
        키 원문은 생성·회전 직후 한 번만 표시됩니다. 서버에는 복원할 수 없는
        HMAC 해시만 저장됩니다. 현재 허용 Scope는 {policy.allowedScopes.join(", ")}이며 기본 만료는 {policy.defaultExpiryDays}일, 활성 키 한도는 {policy.maxActiveKeys}개입니다.
      </Alert>
      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>이름 / 식별자</TableCell>
              <TableCell>범위</TableCell>
              <TableCell>버전</TableCell>
              <TableCell>마지막 사용</TableCell>
              <TableCell>만료</TableCell>
              <TableCell align="right">관리</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {items.map((k) => (
              <TableRow
                key={k.id}
                sx={{ opacity: k.revokedAt && !k.graceUntil ? 0.5 : 1 }}
              >
                <TableCell>
                  <Typography variant="body2" fontWeight={700}>
                    {k.name}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {k.prefix}••••••
                  </Typography>
                </TableCell>
                <TableCell>
                  {k.scopes.map((s) => (
                    <Chip key={s} size="small" label={s} sx={{ mr: 0.5 }} />
                  ))}
                </TableCell>
                <TableCell>
                  v{k.version}
                  {k.graceUntil && (
                    <Chip
                      size="small"
                      color="warning"
                      label="회전 유예"
                      sx={{ ml: 1 }}
                    />
                  )}
                </TableCell>
                <TableCell>
                  {k.lastUsedAt
                    ? new Date(k.lastUsedAt).toLocaleString("ko-KR")
                    : "사용 전"}
                </TableCell>
                <TableCell>
                  {k.expiresAt
                    ? new Date(k.expiresAt).toLocaleDateString("ko-KR")
                    : "제한 없음"}
                </TableCell>
                <TableCell align="right">
                  <Tooltip title="이름 · Scope 변경">
                    <IconButton onClick={() => openEdit(k)} disabled={Boolean(k.revokedAt)}><EditOutlined /></IconButton>
                  </Tooltip>
                  <Tooltip title="회전">
                    <IconButton
                      onClick={() => void rotate(k.id)}
                      disabled={Boolean(k.revokedAt)}
                    >
                      <AutorenewRounded />
                    </IconButton>
                  </Tooltip>
                  <Tooltip title="폐기">
                    <IconButton
                      color="error"
                      onClick={() => void revoke(k.id)}
                      disabled={Boolean(k.revokedAt)}
                    >
                      <DeleteOutlineRounded />
                    </IconButton>
                  </Tooltip>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      <Typography variant="body2" color="text.secondary" mt={2}>
        MCP Endpoint: <code>{window.location.origin}/mcp</code> · Authorization:{" "}
        <code>Bearer vf_…</code>
      </Typography>
      {policy.mcpOAuth?.enabled && (
        <Alert severity="info" sx={{ mt: 2 }} action={<Button color="inherit" size="small" onClick={() => void navigator.clipboard.writeText(policy.mcpOAuth?.resource ?? "").catch(() => undefined)}>URL 복사</Button>}>
          <strong>키 없이 SSO로 연결하기</strong> — Claude·Cursor 같은 MCP 클라이언트에 키 대신{" "}
          <code>{policy.mcpOAuth.resource}</code> 만 넣으세요. 클라이언트가 Keycloak 로그인 화면을 띄우고 토큰을 받아 오며, 이 계정의 권한과 관리자가 정한 범위({policy.mcpOAuth.scopes.join(", ")})로 MCP 도구를 씁니다. 이미 Keycloak에 로그인돼 있으면 화면은 거의 보이지 않습니다.
        </Alert>
      )}
      <Dialog open={createOpen} onClose={() => setCreateOpen(false)}>
        <DialogTitle>{editing ? "개인 API 키 권한 변경" : "개인 API 키 만들기"}</DialogTitle>
        <DialogContent>
          <TextField
            label="키 이름"
            fullWidth
            value={name}
            onChange={(e) => setName(e.target.value)}
            sx={{ mt: 1 }}
          />
          <Typography variant="subtitle2" mt={2}>
            허용 범위
          </Typography>
          <FormGroup row>
            {policy.allowedScopes.map((scope) => (
              <FormControlLabel
                key={scope}
                control={
                  <Checkbox
                    checked={scopes.includes(scope)}
                    onChange={(e) =>
                      setScopes((v) =>
                        e.target.checked
                          ? [...v, scope]
                          : v.filter((x) => x !== scope),
                      )
                    }
                  />
                }
                label={scope}
              />
            ))}
          </FormGroup>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCreateOpen(false)}>취소</Button>
          <Button
            variant="contained"
            disabled={!name || !scopes.length}
            onClick={() => void save()}
          >
            {editing ? "변경 저장" : "생성"}
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={Boolean(revealed)}
        onClose={() => setRevealed(null)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>지금 키를 복사하세요</DialogTitle>
        <DialogContent>
          <Alert severity="warning" sx={{ mb: 2 }}>
            {revealed?.message}
          </Alert>
          <Paper
            variant="outlined"
            sx={{
              p: 2,
              fontFamily: "monospace",
              wordBreak: "break-all",
              bgcolor: "#F5F8F9",
            }}
          >
            {revealed?.key}
          </Paper>
        </DialogContent>
        <DialogActions>
          <Button
            startIcon={<ContentCopyRounded />}
            onClick={() =>
              void navigator.clipboard.writeText(revealed?.key || "").catch(() => undefined)
            }
          >
            복사
          </Button>
          <Button variant="contained" onClick={() => setRevealed(null)}>
            보관 완료
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
