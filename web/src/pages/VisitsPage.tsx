import { useCallback, useEffect, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Chip, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, InputAdornment, MenuItem, Paper, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Tooltip, Typography } from "@mui/material";
import SearchRounded from "@mui/icons-material/SearchRounded";
import AddRounded from "@mui/icons-material/AddRounded";
import VisibilityOutlined from "@mui/icons-material/VisibilityOutlined";
import CancelOutlined from "@mui/icons-material/CancelOutlined";
import AutorenewRounded from "@mui/icons-material/AutorenewRounded";
import SendRounded from "@mui/icons-material/SendRounded";
import EditOutlined from "@mui/icons-material/EditOutlined";
import PersonAddAltRounded from "@mui/icons-material/PersonAddAltRounded";
import ExpandMoreRounded from "@mui/icons-material/ExpandMoreRounded";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api, postJSON, putJSON } from "../api";
import type { ReferenceData, Visit } from "../types";
import { PageHeader } from "../components/AdminUI";
import { StatusChip } from "../components/StatusChip";
import { useAuth } from "../auth";

type Participant = { id: string; name: string; phone: string; company?: string; status: string; passPath?: string; qrcodeImagePath?: string; qrVersion?: number; locale?: string; consentSource?: string; registrationInviteExpiresAt?: string };
export function VisitsPage() {
  const { user } = useAuth(); const navigate = useNavigate(); const [params, setParams] = useSearchParams();
  const [items, setItems] = useState<Visit[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState(""); const [q, setQ] = useState(params.get("q") ?? ""); const [detail, setDetail] = useState<{ visit: Visit; visitors: Participant[] } | null>(null);
  const [nextCursor, setNextCursor] = useState(""); const [loadingMore, setLoadingMore] = useState(false); const [notice, setNotice] = useState("");
  const [selfRegistration, setSelfRegistration] = useState(true);
  useEffect(() => { api<ReferenceData>("/api/v1/reference-data").then((x) => setSelfRegistration(x.selfRegistrationEnabled !== false)).catch(() => undefined); }, []);
  const period = params.get("period") ?? ""; const status = params.get("status") ?? "";
  const buildQuery = useCallback((cursor?: string) => {
    const query = new URLSearchParams({ limit: "100" });
    if (period) query.set("period", period);
    if (status) query.set("status", status);
    if (q) query.set("q", q);
    if (cursor) query.set("cursor", cursor);
    return query;
  }, [period, status, q]);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api<{ items: Visit[]; nextCursor: string }>(`/api/v1/visits?${buildQuery()}`);
      setItems(data.items); setNextCursor(data.nextCursor ?? "");
    } catch (e) { setError(e instanceof Error ? e.message : "방문을 불러오지 못했습니다"); } finally { setLoading(false); }
  }, [buildQuery]);
  // Paging is keyset based, so newly created visits never shift a later page.
  const loadMore = useCallback(async () => {
    if (!nextCursor) return;
    setLoadingMore(true);
    try {
      const data = await api<{ items: Visit[]; nextCursor: string }>(`/api/v1/visits?${buildQuery(nextCursor)}`);
      setItems((current) => [...current, ...data.items]); setNextCursor(data.nextCursor ?? "");
    } catch (e) { setError(e instanceof Error ? e.message : "다음 목록을 불러오지 못했습니다"); } finally { setLoadingMore(false); }
  }, [buildQuery, nextCursor]);
  useEffect(() => { void load(); }, [load]);
  const view = async (id: string) => { try { setDetail(await api(`/api/v1/visits/${id}`)); } catch (e) { setError(e instanceof Error ? e.message : "상세를 불러오지 못했습니다"); } };
  const cancel = async (id: string) => { if (!confirm("방문을 취소하고 모든 QR을 즉시 폐기할까요?")) return; try { await postJSON(`/api/v1/visits/${id}/cancel`, {}); await load(); setDetail(null); } catch (e) { setError(e instanceof Error ? e.message : "취소하지 못했습니다"); } };
  const reissue = async (participantId: string) => { try { const result = await postJSON<{ passUrl: string }>(`/api/v1/visitor-visits/${participantId}/qr/reissue`, {}); try { await navigator.clipboard.writeText(result.passUrl); setNotice("새 모바일 방문증 URL을 복사했습니다."); } catch { setNotice(`새 모바일 방문증 URL: ${result.passUrl}`); } if (detail) await view(detail.visit.id); } catch (e) { setError(e instanceof Error ? e.message : "QR을 재발급하지 못했습니다"); } };
  const [edit, setEdit] = useState<{ startAt: string; endAt: string; purpose: string; placeDetail: string; lobbyId: string } | null>(null);
  const [lobbies, setLobbies] = useState<{ id: string; siteId: string; name: string }[]>([]);
  const toLocal = (value: string) => { const date = new Date(value); return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16); };
  const openEdit = async () => { if (!detail) return; if (lobbies.length === 0) { try { const ref = await api<ReferenceData>("/api/v1/reference-data"); setLobbies(ref.lobbies); } catch { /* lobby list is optional */ } } setEdit({ startAt: toLocal(detail.visit.startAt), endAt: toLocal(detail.visit.endAt), purpose: detail.visit.purpose, placeDetail: detail.visit.placeDetail ?? "", lobbyId: detail.visit.lobbyId ?? "" }); };
  const saveEdit = async () => { if (!detail || !edit) return; try { await putJSON(`/api/v1/visits/${detail.visit.id}`, { startAt: new Date(edit.startAt).toISOString(), endAt: new Date(edit.endAt).toISOString(), purpose: edit.purpose, placeDetail: edit.placeDetail, lobbyId: edit.lobbyId }); setEdit(null); setNotice("방문 일정을 수정했습니다. 발급된 QR 유효기간과 예약 알림도 함께 갱신됩니다."); await load(); await view(detail.visit.id); } catch (e) { setError(e instanceof Error ? e.message : "일정을 수정하지 못했습니다"); } };
  // The visitor fills in their own details and records their own consent, so the
  // host only shares the link.
  const invite = async (participantId: string) => {
    try {
      const result = await postJSON<{ registrationUrl: string; expiresAt: string }>(`/api/v1/visitor-visits/${participantId}/invitation`, {});
      try { await navigator.clipboard.writeText(result.registrationUrl); } catch { /* clipboard may be unavailable */ }
      setNotice(`사전등록 링크를 복사했습니다. ${new Date(result.expiresAt).toLocaleString("ko-KR")}까지 유효합니다.`);
      if (detail) await view(detail.visit.id);
    } catch (e) { setError(e instanceof Error ? e.message : "사전등록 링크를 만들지 못했습니다"); }
  };
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1400, mx: "auto" }}><PageHeader eyebrow="VISIT DIRECTORY" title="내 방문 일정" description="예정·도착·방문중·지난 방문을 검색하고 QR을 관리합니다." actions={<Button variant="contained" startIcon={<AddRounded />} onClick={() => navigate("/visits/new")}>방문 신청</Button>} />{error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}{notice && <Alert severity="success" onClose={() => setNotice("")} sx={{ mb: 2 }}>{notice}</Alert>}
    <Card><CardContent><Stack direction={{ xs: "column", md: "row" }} spacing={1.5} mb={2}><TextField fullWidth placeholder="방문번호 / 회사 / 담당자 검색" value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") void load(); }} slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchRounded /></InputAdornment> } }} /><TextField select label="기간" value={period} onChange={(e) => { const next = new URLSearchParams(params); e.target.value ? next.set("period", e.target.value) : next.delete("period"); setParams(next); }} sx={{ minWidth: 150 }}><MenuItem value="">전체</MenuItem><MenuItem value="today">오늘</MenuItem><MenuItem value="upcoming">예정</MenuItem><MenuItem value="past">지난 방문</MenuItem></TextField><TextField select label="상태" value={status} onChange={(e) => { const next = new URLSearchParams(params); e.target.value ? next.set("status", e.target.value) : next.delete("status"); setParams(next); }} sx={{ minWidth: 160 }}><MenuItem value="">전체</MenuItem>{["PENDING_APPROVAL", "SCHEDULED", "CHECKED_IN", "CHECKED_OUT", "CANCELLED", "REJECTED", "NO_SHOW"].map((x) => <MenuItem key={x} value={x}>{x}</MenuItem>)}</TextField></Stack>
      <TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>일시</TableCell><TableCell>방문자</TableCell><TableCell>장소 / 목적</TableCell><TableCell>상태</TableCell><TableCell>방문번호</TableCell><TableCell align="right">관리</TableCell></TableRow></TableHead><TableBody>{items.map((visit) => <TableRow hover key={visit.id}><TableCell sx={{ whiteSpace: "nowrap" }}><Typography variant="body2" fontWeight={750}>{new Date(visit.startAt).toLocaleDateString("ko-KR")}</Typography><Typography variant="caption" color="text.secondary">{new Date(visit.startAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" })}–{new Date(visit.endAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" })}</Typography></TableCell><TableCell><Typography variant="body2" fontWeight={750}>{visit.primaryVisitor}{visit.visitorCount > 1 ? ` 외 ${visit.visitorCount - 1}명` : ""}</Typography><Typography variant="caption" color="text.secondary">{visit.company || "회사 미입력"}</Typography></TableCell><TableCell><Typography variant="body2">{visit.siteName} {visit.lobbyName && `· ${visit.lobbyName}`}</Typography><Typography variant="caption" color="text.secondary">{visit.purpose}</Typography></TableCell><TableCell><StatusChip status={visit.status} /></TableCell><TableCell><Typography variant="caption" sx={{ fontFamily: "monospace" }}>{visit.requestNo}</Typography></TableCell><TableCell align="right"><Tooltip title="상세"><IconButton onClick={() => void view(visit.id)}><VisibilityOutlined /></IconButton></Tooltip>{["PENDING_APPROVAL", "APPROVED", "SCHEDULED"].includes(visit.status) && <Tooltip title="방문 취소"><IconButton color="error" onClick={() => void cancel(visit.id)}><CancelOutlined /></IconButton></Tooltip>}</TableCell></TableRow>)}{!loading && items.length === 0 && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 8 }}>조건에 맞는 방문이 없습니다.</TableCell></TableRow>}</TableBody></Table></TableContainer>
      <Stack direction="row" justifyContent="center" alignItems="center" spacing={2} mt={2}>
        <Typography variant="caption" color="text.secondary">{items.length}건 표시{nextCursor ? " · 더 있음" : ""}</Typography>
        {nextCursor && <Button startIcon={<ExpandMoreRounded />} disabled={loadingMore} onClick={() => void loadMore()}>{loadingMore ? "불러오는 중…" : "더 보기"}</Button>}
      </Stack>
    </CardContent></Card>
    <Dialog open={Boolean(detail)} onClose={() => setDetail(null)} maxWidth="md" fullWidth><DialogTitle>{detail?.visit.requestNo} · 방문 상세</DialogTitle><DialogContent dividers>{detail && <Stack spacing={2}><Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between"><Box><Typography variant="h6">{detail.visit.siteName} · {detail.visit.purpose}</Typography><Typography color="text.secondary">{new Date(detail.visit.startAt).toLocaleString("ko-KR")} – {new Date(detail.visit.endAt).toLocaleString("ko-KR")}</Typography></Box><StatusChip status={detail.visit.status} /></Stack>{detail.visitors.map((p) => <Paper key={p.id} variant="outlined" sx={{ p: 2 }}><Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "center" }}><Box><Typography fontWeight={800}>{p.name} · {p.company || "회사 미입력"}</Typography><Typography variant="body2" color="text.secondary">{p.phone} · QR v{p.qrVersion ?? "-"}{p.locale ? ` · ${p.locale.toUpperCase()}` : ""}</Typography>{p.consentSource === "self" ? <Chip size="small" color="success" variant="outlined" label="본인 사전등록 완료" sx={{ mt: .5 }} /> : p.registrationInviteExpiresAt ? <Chip size="small" color="info" variant="outlined" label={`사전등록 대기 · ${new Date(p.registrationInviteExpiresAt).toLocaleDateString("ko-KR")}까지`} sx={{ mt: .5 }} /> : null}</Box><Stack direction="row" spacing={1} alignItems="center"><StatusChip status={p.status} />{p.passPath && <Button size="small" href={p.passPath} target="_blank">방문증</Button>}{p.qrcodeImagePath && <Button size="small" href={p.qrcodeImagePath} download>QR JPG</Button>}{selfRegistration && ["PENDING_APPROVAL", "APPROVED", "SCHEDULED"].includes(detail.visit.status) && <IconButton title="방문자 사전등록 링크" onClick={() => void invite(p.id)}><PersonAddAltRounded /></IconButton>}{["SCHEDULED", "APPROVED"].includes(detail.visit.status) && <IconButton title="QR 재발급" onClick={() => void reissue(p.id)}><AutorenewRounded /></IconButton>}</Stack></Stack></Paper>)}</Stack>}</DialogContent><DialogActions>{detail && ["PENDING_APPROVAL", "APPROVED", "SCHEDULED"].includes(detail.visit.status) && <Button startIcon={<EditOutlined />} onClick={() => void openEdit()}>일정 수정</Button>}{detail && ["PENDING_APPROVAL", "APPROVED", "SCHEDULED"].includes(detail.visit.status) && <Button color="error" startIcon={<CancelOutlined />} onClick={() => void cancel(detail.visit.id)}>방문 취소</Button>}{detail && ["APPROVED", "SCHEDULED", "ARRIVED", "CHECKED_IN"].includes(detail.visit.status) && <Button startIcon={<SendRounded />} onClick={async () => { await postJSON(`/api/v1/visits/${detail.visit.id}/notifications/resend`, {}); setNotice("현재 QR과 발송 규칙으로 알림을 다시 등록했습니다."); }}>알림 재발송</Button>}<Button onClick={() => setDetail(null)}>닫기</Button></DialogActions></Dialog>
    <Dialog open={Boolean(edit)} onClose={() => setEdit(null)} fullWidth maxWidth="sm"><DialogTitle>방문 일정 수정</DialogTitle><DialogContent dividers>{edit && <Stack spacing={2} mt={1}><Stack direction={{ xs: "column", sm: "row" }} spacing={2}><TextField fullWidth type="datetime-local" label="시작" value={edit.startAt} onChange={(e) => setEdit({ ...edit, startAt: e.target.value })} slotProps={{ inputLabel: { shrink: true } }} /><TextField fullWidth type="datetime-local" label="종료" value={edit.endAt} onChange={(e) => setEdit({ ...edit, endAt: e.target.value })} slotProps={{ inputLabel: { shrink: true } }} /></Stack><TextField required label="방문 목적" value={edit.purpose} onChange={(e) => setEdit({ ...edit, purpose: e.target.value })} /><TextField label="세부 방문 장소" value={edit.placeDetail} onChange={(e) => setEdit({ ...edit, placeDetail: e.target.value })} /><TextField select label="로비" value={edit.lobbyId} onChange={(e) => setEdit({ ...edit, lobbyId: e.target.value })}><MenuItem value="">미지정</MenuItem>{lobbies.filter((x) => x.siteId === detail?.visit.siteId).map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField></Stack>}</DialogContent><DialogActions><Button onClick={() => setEdit(null)}>취소</Button><Button variant="contained" disabled={!edit?.purpose || !edit?.startAt || !edit?.endAt || new Date(edit.endAt) <= new Date(edit.startAt)} onClick={() => void saveEdit()}>저장</Button></DialogActions></Dialog>
  </Box>;
}
