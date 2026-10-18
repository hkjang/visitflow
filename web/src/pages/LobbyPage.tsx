import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Dialog, DialogActions, DialogContent, DialogTitle, Grid, InputAdornment, MenuItem, Paper, Stack, Tab, Tabs, TextField, Typography } from "@mui/material";
import PrintRounded from "@mui/icons-material/PrintRounded";
import HowToRegRounded from "@mui/icons-material/HowToRegRounded";
import EventAvailableRounded from "@mui/icons-material/EventAvailableRounded";
import MeetingRoomRounded from "@mui/icons-material/MeetingRoomRounded";
import ExitToAppRounded from "@mui/icons-material/ExitToAppRounded";
import PersonOffRounded from "@mui/icons-material/PersonOffRounded";
import QrCodeScannerRounded from "@mui/icons-material/QrCodeScannerRounded";
import PersonAddAltRounded from "@mui/icons-material/PersonAddAltRounded";
import SearchRounded from "@mui/icons-material/SearchRounded";
import ChecklistRounded from "@mui/icons-material/ChecklistRounded";
import { useNavigate } from "react-router-dom";
import { api, postJSON } from "../api";
import { lobbyPanel, lobbyView, type LobbyLoad, type LobbyToday } from "../lobbyCounts";
import type { LobbyVisitor, ReferenceData } from "../types";
import { useAuth } from "../auth";
import { MetricCard, PageHeader } from "../components/AdminUI";
import { StatusChip } from "../components/StatusChip";

export function LobbyPage() {
  // `error` 는 직접 체크인·퇴실 같은 **행동** 실패용이다. 조회 실패는 `data` 가
  // 판별 값으로 들고 있다(lobbyCounts.ts) — 양쪽에 중복 표시하면 경고가 두 개가 된다.
  const navigate = useNavigate(); const [tab, setTab] = useState<"today" | "current">("today"); const [data, setData] = useState<LobbyLoad>({ state: "loading" }); const [error, setError] = useState(""); const [connected, setConnected] = useState(false);
  const [query, setQuery] = useState(""); const [lobbyFilter, setLobbyFilter] = useState("");
  const { user } = useAuth();
  const [reference, setReference] = useState<ReferenceData | null>(null);
  const scope = user?.role === "lobby" ? (user.siteScope ?? []) : [];
  const lobbies = (reference?.lobbies ?? []).filter((x) => scope.length === 0 || scope.includes(x.siteId));
  useEffect(() => { api<ReferenceData>("/api/v1/reference-data").then(setReference).catch(() => undefined); }, []);
  const [manual, setManual] = useState<LobbyVisitor | null>(null); const [manualReason, setManualReason] = useState(""); const [manualBadge, setManualBadge] = useState("");
  const load = useCallback(async () => {
    const view = lobbyView(tab, query, lobbyFilter);
    try {
      const params = new URLSearchParams({ q: query }); if (lobbyFilter) params.set("lobby", lobbyFilter);
      setData({ state: "ready", value: await api<LobbyToday>(`/api/v1/lobby/${tab}?${params}`), at: new Date().toISOString(), view });
    } catch (e) {
      // 받아 둔 숫자는 버리지 않는다. 다만 그것이 마지막 성공 조회의 값이라는
      // 사실을 닫을 수 없는 표시로 함께 들고 다닌다.
      setData((prev) => ({ state: "failed", message: e instanceof Error ? e.message : "", last: prev.state === "ready" ? { value: prev.value, at: prev.at, view: prev.view } : prev.state === "failed" ? prev.last : null }));
    }
  }, [tab, query, lobbyFilter]);
  // A visitor without a working phone is still expected; admit them after an
  // identity check and keep the reason on the record.
  const manualCheckIn = async () => { if (!manual) return; try { await postJSON("/api/v1/checkins/manual", { visitorVisitId: manual.visitorVisitId, lobbyId: lobbyFilter, badgeNo: manualBadge, reason: manualReason.trim() }); setManual(null); setManualReason(""); setManualBadge(""); await load(); } catch (e) { setError(e instanceof Error ? e.message : "직접 체크인하지 못했습니다"); } };
  const printBadge = (item: LobbyVisitor) => {
    const popup = window.open("", "_blank", "width=420,height=300");
    if (!popup) return;
    const escape = (value: string) => value.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c] ?? c));
    popup.document.write(`<!doctype html><html lang="ko"><head><meta charset="utf-8"><title>출입증</title><style>body{font-family:system-ui,sans-serif;margin:0;padding:18px;width:360px}h1{font-size:30px;margin:0 0 4px}p{margin:2px 0;font-size:14px;color:#333}.tag{font-size:11px;letter-spacing:.12em;color:#666}.foot{margin-top:14px;font-size:12px;color:#777;border-top:1px solid #ccc;padding-top:6px}@media print{body{padding:8px}}</style></head><body><div class="tag">VISITOR</div><h1>${escape(item.visitor)}</h1><p>${escape(item.company || "")}</p><p>담당 ${escape(item.host)}${item.department ? ` · ${escape(item.department)}` : ""}</p><p>${escape(item.site)}${item.lobby ? ` · ${escape(item.lobby)}` : ""}</p><div class="foot">${new Date(item.startAt).toLocaleDateString("ko-KR")} · 입실 ${item.checkedInAt ? new Date(item.checkedInAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" }) : "-"} · 퇴실 시 반납</div><script>window.onload=function(){window.print();}</script></body></html>`);
    popup.document.close();
  };
  useEffect(() => { void load(); }, [load]);
  // The stream must outlive filter changes: re-subscribing on every keystroke
  // reconnects the SSE channel and drops events in between.
  const loadRef = useRef(load); useEffect(() => { loadRef.current = load; }, [load]);
  useEffect(() => { const stream = new EventSource("/api/v1/lobby/stream"); stream.addEventListener("ready", () => setConnected(true)); stream.addEventListener("visitflow", () => void loadRef.current()); stream.onerror = () => setConnected(false); return () => stream.close(); }, []);
  const checkout = async (id: string) => { try { await postJSON("/api/v1/checkouts", { visitorVisitId: id, method: "lobby" }); await load(); } catch (e) { setError(e instanceof Error ? e.message : "퇴실 처리하지 못했습니다"); } };
  const panel = lobbyPanel(data, lobbyView(tab, query, lobbyFilter)); const summary = panel.summary;
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1500, mx: "auto" }}><PageHeader eyebrow="LIVE LOBBY" title="Lobby Dashboard" description="오늘 방문 예정과 현재 사내 체류 인원을 실시간으로 관리합니다." actions={<><Button startIcon={<ChecklistRounded />} onClick={() => navigate("/lobby/roster")}>비상 명단</Button><Button startIcon={<PersonAddAltRounded />} onClick={() => navigate("/lobby/walk-in")}>현장 등록</Button><Button variant="contained" startIcon={<QrCodeScannerRounded />} onClick={() => navigate("/lobby/scan")}>QR 스캔</Button></>} />{error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}<Stack direction="row" alignItems="center" spacing={1} mb={2}><Box sx={{ width: 9, height: 9, borderRadius: "50%", bgcolor: connected ? "success.main" : "warning.main" }} /><Typography variant="caption" color="text.secondary">{connected ? "실시간 연결됨" : "재연결 중 · 화면 조회는 정상"}</Typography></Stack>
    {/* 조회가 한 번도 성공하지 못했으면 네 카드 자리를 경고 하나로 치환한다 —
        라벨만 남기고 `-` 를 뿌리면 그 자체가 또 하나의 「말」이 된다. */}
    <Box>{summary.kind === "notice" ? <Alert severity={summary.severity}>{summary.message}</Alert> : <><Grid container spacing={2}>{([["scheduled", "방문 예정", "오늘 체크인 대기", <EventAvailableRounded />, "#3978A8"], ["current", "현재 방문중", "재난 시 체류 기준 인원", <MeetingRoomRounded />, "#176B5B"], ["completed", "퇴실 완료", "오늘 퇴실 처리", <ExitToAppRounded />, "#61736E"], ["noShow", "미방문", "정책시간 경과", <PersonOffRounded />, "#D58A20"]] as const).map(([key, label, helper, icon, tone]) => <Grid key={label} size={{ xs: 6, lg: 3 }}><MetricCard label={label} value={summary.counts[key]} helper={helper} icon={icon} tone={tone} /></Grid>)}</Grid>{summary.staleNote && <Alert severity="warning" sx={{ mt: 2 }}>{summary.staleNote}</Alert>}</>}</Box>
    <Card sx={{ mt: 3 }}><Tabs value={tab} onChange={(_, value) => setTab(value)} sx={{ px: 2, borderBottom: "1px solid #E1E9E6" }}><Tab value="today" label="오늘 방문자" /><Tab value="current" label={panel.currentTabLabel} /></Tabs><CardContent>{lobbies.length > 1 && <TextField select size="small" label="로비" value={lobbyFilter} onChange={(e) => setLobbyFilter(e.target.value)} sx={{ mb: 2, minWidth: 220 }}><MenuItem value="">전체 로비</MenuItem>{lobbies.map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField>}{panel.truncationNote && <Alert severity="info" sx={{ mb: 2 }}>{panel.truncationNote}</Alert>}<TextField fullWidth value={query} onChange={(e) => setQuery(e.target.value)} placeholder="이름 / 회사 / 담당자 / 부서 / 전화번호 검색" sx={{ mb: 2 }} slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchRounded /></InputAdornment> } }} /><Stack spacing={1.25}>{panel.items.map((item) => <Paper key={item.visitorVisitId} variant="outlined" sx={{ p: 2 }}><Stack direction={{ xs: "column", md: "row" }} justifyContent="space-between" alignItems={{ md: "center" }} spacing={2}><Stack direction="row" spacing={2} alignItems="center"><Box sx={{ width: 72, textAlign: "center" }}><Typography fontWeight={850}>{new Date(item.startAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" })}</Typography><Typography variant="caption" color="text.secondary">{item.checkedInAt ? `입실 ${new Date(item.checkedInAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" })}` : "예정"}</Typography></Box><Box><Typography fontWeight={800}>{item.visitor} · {item.company || "회사 미입력"}</Typography><Typography variant="body2" color="text.secondary">{item.host} / {item.department || "부서 미지정"} · {item.site} {item.lobby && `/ ${item.lobby}`}</Typography></Box></Stack><Stack direction="row" spacing={1} alignItems="center"><StatusChip status={item.status} />{["SCHEDULED", "ARRIVED"].includes(item.status) && <Button variant="outlined" startIcon={<HowToRegRounded />} onClick={() => { setManual(item); setManualReason(""); setManualBadge(""); }}>직접 체크인</Button>}{item.status === "CHECKED_IN" && <Button variant="text" startIcon={<PrintRounded />} onClick={() => printBadge(item)}>출입증</Button>}{item.status === "CHECKED_IN" && <Button color="secondary" variant="outlined" startIcon={<ExitToAppRounded />} onClick={() => void checkout(item.visitorVisitId)}>퇴실</Button>}</Stack></Stack></Paper>)}{panel.items.length === 0 && <Box sx={{ py: 8, textAlign: "center" }}><Typography color="text.secondary">{panel.emptyText}</Typography></Box>}</Stack></CardContent></Card>
    <Dialog open={Boolean(manual)} onClose={() => setManual(null)} fullWidth maxWidth="sm"><DialogTitle>QR 없이 직접 체크인</DialogTitle><DialogContent dividers>{manual && <Stack spacing={2} mt={1}><Alert severity="warning">{manual.visitor} · {manual.company || "회사 미입력"} · 담당 {manual.host}. 신분증 등으로 본인을 확인한 뒤 진행하세요. 발급된 QR은 사용 처리되어 다시 쓸 수 없습니다.</Alert><TextField required label="신분 확인 방법 / 사유" value={manualReason} onChange={(e) => setManualReason(e.target.value)} placeholder="예: 신분증 대조, 휴대폰 방전" /><TextField label="임시 출입증 번호 (선택)" value={manualBadge} onChange={(e) => setManualBadge(e.target.value)} /></Stack>}</DialogContent><DialogActions><Button onClick={() => setManual(null)}>취소</Button><Button variant="contained" color="success" disabled={!manualReason.trim()} onClick={() => void manualCheckIn()}>체크인</Button></DialogActions></Dialog>
  </Box>;
}
