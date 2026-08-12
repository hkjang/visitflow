import { useCallback, useEffect, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Paper, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Typography } from "@mui/material";
import CheckCircleOutlineRounded from "@mui/icons-material/CheckCircleOutlineRounded";
import HighlightOffRounded from "@mui/icons-material/HighlightOffRounded";
import { api, postJSON } from "../api";
import type { Visit } from "../types";
import { PageHeader } from "../components/AdminUI";
import { StatusChip } from "../components/StatusChip";

export function ApprovalsPage() {
  const [items, setItems] = useState<Visit[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const load = useCallback(async () => {
    try {
      const result = await api<{ items: Visit[] }>("/api/v1/visits?status=PENDING_APPROVAL&limit=200");
      setItems(result.items);
    } catch (e) { setError(e instanceof Error ? e.message : "승인 대기 목록을 불러오지 못했습니다"); }
  }, []);
  useEffect(() => { void load(); }, [load]);
  const decide = async (visit: Visit, approve: boolean) => {
    const reason = window.prompt(approve ? "승인 메모 (선택)" : "반려 사유를 입력하세요") ?? "";
    if (!approve && !reason.trim()) return;
    setBusy(visit.id); setError("");
    try {
      await postJSON(`/api/v1/visits/${visit.id}/${approve ? "approve" : "reject"}`, { reason });
      await load();
    } catch (e) { setError(e instanceof Error ? e.message : "승인 요청을 처리하지 못했습니다"); }
    finally { setBusy(""); }
  };
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1300, mx: "auto" }}>
    <PageHeader eyebrow="APPROVAL QUEUE" title="방문 승인" description="내 부서 또는 보안 권한 범위의 방문 요청을 검토합니다." />
    {error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}
    <Card><CardContent><TableContainer component={Paper} variant="outlined"><Table>
      <TableHead><TableRow><TableCell>방문 일시</TableCell><TableCell>방문자</TableCell><TableCell>담당자 · 장소</TableCell><TableCell>목적</TableCell><TableCell>상태</TableCell><TableCell align="right">결정</TableCell></TableRow></TableHead>
      <TableBody>{items.map((visit) => <TableRow key={visit.id} hover><TableCell sx={{ whiteSpace: "nowrap" }}>{new Date(visit.startAt).toLocaleString("ko-KR")}</TableCell><TableCell><Typography fontWeight={750}>{visit.primaryVisitor}{visit.visitorCount > 1 ? ` 외 ${visit.visitorCount - 1}명` : ""}</Typography><Typography variant="caption" color="text.secondary">{visit.company || "회사 미입력"}</Typography></TableCell><TableCell>{visit.hostName}<Typography variant="caption" display="block" color="text.secondary">{visit.siteName} · {visit.lobbyName}</Typography></TableCell><TableCell>{visit.purpose}</TableCell><TableCell><StatusChip status={visit.status} /></TableCell><TableCell align="right"><Stack direction="row" justifyContent="flex-end" spacing={1}><Button color="error" startIcon={<HighlightOffRounded />} disabled={busy === visit.id} onClick={() => void decide(visit, false)}>반려</Button><Button variant="contained" startIcon={<CheckCircleOutlineRounded />} disabled={busy === visit.id} onClick={() => void decide(visit, true)}>승인</Button></Stack></TableCell></TableRow>)}{items.length === 0 && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 8 }}>승인 대기 중인 방문이 없습니다.</TableCell></TableRow>}</TableBody>
    </Table></TableContainer></CardContent></Card>
  </Box>;
}
