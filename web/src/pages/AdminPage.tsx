import { useCallback, useEffect, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Chip, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Grid, LinearProgress, MenuItem, Paper, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Typography } from "@mui/material";
import EventNoteRounded from "@mui/icons-material/EventNoteRounded";
import MeetingRoomRounded from "@mui/icons-material/MeetingRoomRounded";
import HourglassTopRounded from "@mui/icons-material/HourglassTopRounded";
import SmsFailedRounded from "@mui/icons-material/SmsFailedRounded";
import AddRounded from "@mui/icons-material/AddRounded";
import OpenInNewRounded from "@mui/icons-material/OpenInNewRounded";
import DownloadRounded from "@mui/icons-material/DownloadRounded";
import ReplayRounded from "@mui/icons-material/ReplayRounded";
import BlockRounded from "@mui/icons-material/BlockRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import { useParams } from "react-router-dom";
import { api, patchJSON, postJSON, putJSON } from "../api";
import type { KioskDevice, OperationalMetrics, ReferenceData, Visit, VisitType } from "../types";
import { MetricCard, PageHeader } from "../components/AdminUI";
import { StatusChip } from "../components/StatusChip";

export function AdminPage({ fixedSection }: { fixedSection?: string }) {
  const { section: routeSection } = useParams(); const section = fixedSection ?? routeSection ?? "dashboard"; const [error, setError] = useState("");
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1500, mx: "auto" }}>{error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}{section === "dashboard" && <Dashboard setError={setError} />}{section === "visits" && <VisitAdmin setError={setError} />}{section === "resources" && <Resources setError={setError} />}{section === "statistics" && <Statistics setError={setError} />}{section === "audit" && <Audit setError={setError} />}{section === "operations" && <Operations setError={setError} />}{section === "api" && <APIPage />}</Box>;
}

function Dashboard({ setError }: { setError: (v: string) => void }) {
  const [counts, setCounts] = useState<Record<string, number>>({});
  const [metrics, setMetrics] = useState<OperationalMetrics | null>(null);
  useEffect(() => { api<{ counts: Record<string, number> }>("/api/v1/admin/dashboard").then((x) => setCounts(x.counts)).catch((e) => setError(e.message)); }, [setError]);
  useEffect(() => { api<OperationalMetrics>("/api/v1/admin/metrics").then(setMetrics).catch(() => setMetrics(null)); }, []);
  return <><PageHeader eyebrow="OPERATIONS" title="관리 Dashboard" description="실시간 방문·승인·알림·보안 운영 상태를 요약합니다." /><Grid container spacing={2}>{[["오늘 방문자", counts.today ?? 0, "개별 방문자 기준", <EventNoteRounded />, "#3978A8"], ["현재 체류", counts.current ?? 0, "재난 대응 기준 인원", <MeetingRoomRounded />, "#176B5B"], ["승인 대기", counts.pendingApproval ?? 0, "Workflow 조치 필요", <HourglassTopRounded />, "#D58A20"], ["알림 실패", counts.failedMessages ?? 0, "최대 5회 자동 재시도", <SmsFailedRounded />, "#C94C53"]].map(([label, value, helper, icon, tone]) => <Grid key={String(label)} size={{ xs: 6, lg: 3 }}><MetricCard label={String(label)} value={Number(value)} helper={String(helper)} icon={icon} tone={String(tone)} /></Grid>)}</Grid><Grid container spacing={2} mt={.2}><Grid size={{ xs: 12, md: 7 }}><Card><CardContent><Typography variant="h6">운영 준비도</Typography><Typography variant="body2" color="text.secondary" mb={3}>관리자 설정에서 회사 정책과 연동을 활성화할 수 있습니다.</Typography>{[["승인 대기 해소", counts.pendingApproval ?? 0], ["알림 실패 처리", counts.failedMessages ?? 0], ["Watch List 활성", counts.watchlist ?? 0]].map(([label, value], i) => <Box key={String(label)} mb={2}><Stack direction="row" justifyContent="space-between"><Typography variant="body2">{label}</Typography><Typography variant="body2" fontWeight={800}>{Number(value)}건</Typography></Stack><LinearProgress variant="determinate" value={Math.max(8, 100 - Number(value) * 8)} color={i === 1 ? "error" : "primary"} sx={{ mt: 1, height: 7, borderRadius: 4 }} /></Box>)}</CardContent></Card></Grid><Grid size={{ xs: 12, md: 5 }}><Card><CardContent><Typography variant="h6">운영 지표</Typography><Typography variant="body2" color="text.secondary" mb={2}>Prometheus 지표와 같은 값을 사용합니다.</Typography><Stack spacing={1.1}>{[["알림 대기열", `${metrics?.queueBacklog ?? 0}건`], ["대기열 최장 지연", `${Math.round((metrics?.queueOldestSeconds ?? 0) / 60)}분`], ["잠긴 계정·주소", `${metrics?.lockedAccounts ?? 0}건`], ["활성 세션", `${metrics?.activeSessions ?? 0}개`], ["활성 API 키", `${metrics?.activeApiKeys ?? 0}개`], ["스키마 버전", `v${metrics?.schemaVersion ?? "-"}`]].map(([label, value]) => <Stack key={label} direction="row" justifyContent="space-between"><Typography variant="body2" color="text.secondary">{label}</Typography><Typography variant="body2" fontWeight={800}>{value}</Typography></Stack>)}</Stack></CardContent></Card><Card sx={{ mt: 2 }}><CardContent><Typography variant="h6">보안 기준</Typography><Stack spacing={1.2} mt={2}>{["QR에는 개인정보 미포함", "랜덤 Token + HMAC 조회", "1회 사용·재발급 즉시 폐기", "개인정보 AES-256-GCM 암호화", "모든 주요 동작 감사 기록"].map((x) => <Stack key={x} direction="row" spacing={1}><Chip label="✓" size="small" color="success" /><Typography variant="body2">{x}</Typography></Stack>)}</Stack></CardContent></Card></Grid></Grid></>;
}

function VisitAdmin({ setError }: { setError: (v: string) => void }) {
  const [items, setItems] = useState<Visit[]>([]); const [visitors, setVisitors] = useState<Record<string, unknown>[]>([]); const [tab, setTab] = useState<"visits" | "visitors" | "watchlist">("visits"); const [watch, setWatch] = useState<Record<string, unknown>[]>([]); const [watchOpen, setWatchOpen] = useState(false); const [watchForm, setWatchForm] = useState({ name: "", phone: "", company: "", reason: "" });
  const load = async () => { try { const [v, people, wl] = await Promise.all([api<{ items: Visit[] }>("/api/v1/visits?limit=200"), api<{ items: Record<string, unknown>[] }>("/api/v1/admin/visitors"), api<{ items: Record<string, unknown>[] }>("/api/v1/admin/watchlist")]); setItems(v.items); setVisitors(people.items); setWatch(wl.items); } catch (e) { setError(e instanceof Error ? e.message : "데이터를 불러오지 못했습니다"); } }; useEffect(() => { void load(); }, []);
  const approval = async (visit: Visit, approve: boolean) => { const reason = prompt(approve ? "승인 메모 (선택)" : "반려 사유") ?? ""; if (!approve && !reason) return; try { await postJSON(`/api/v1/visits/${visit.id}/${approve ? "approve" : "reject"}`, { reason }); await load(); } catch (e) { setError(e instanceof Error ? e.message : "처리하지 못했습니다"); } };
  const addWatch = async () => { try { await postJSON("/api/v1/admin/watchlist", watchForm); setWatchOpen(false); setWatchForm({ name: "", phone: "", company: "", reason: "" }); await load(); } catch (e) { setError(e instanceof Error ? e.message : "등록하지 못했습니다"); } };
  return <><PageHeader eyebrow="VISITOR CONTROL" title="방문 · 방문자 관리" description="전체 방문 Workflow, 방문자 이력과 보안 Watch List를 관리합니다." actions={<><Button startIcon={<DownloadRounded />} href="/api/v1/admin/visits.csv?days=90">방문 이력 CSV</Button>{tab === "watchlist" && <Button variant="contained" startIcon={<AddRounded />} onClick={() => setWatchOpen(true)}>제한 등록</Button>}</>} /><Stack direction="row" spacing={1} mb={2}>{[["visits", "전체 방문"], ["visitors", "방문자 이력"], ["watchlist", "Watch List"]].map(([value, label]) => <Button key={value} variant={tab === value ? "contained" : "outlined"} onClick={() => setTab(value as typeof tab)}>{label}</Button>)}</Stack><TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow>{tab === "visits" ? <><TableCell>일시 / 번호</TableCell><TableCell>방문자</TableCell><TableCell>담당 / 장소</TableCell><TableCell>상태</TableCell><TableCell align="right">승인</TableCell></> : tab === "visitors" ? <><TableCell>방문자</TableCell><TableCell>전화번호</TableCell><TableCell>회사</TableCell><TableCell>방문 횟수</TableCell><TableCell>최근 방문</TableCell></> : <><TableCell>대상</TableCell><TableCell>회사</TableCell><TableCell>제한 사유</TableCell><TableCell>기간</TableCell><TableCell>상태</TableCell></>}</TableRow></TableHead><TableBody>
      {tab === "visits" && items.map((x) => <TableRow key={x.id}><TableCell><Typography variant="body2" fontWeight={700}>{new Date(x.startAt).toLocaleString("ko-KR")}</Typography><Typography variant="caption" sx={{ fontFamily: "monospace" }}>{x.requestNo}</Typography></TableCell><TableCell>{x.primaryVisitor}{x.visitorCount > 1 ? ` 외 ${x.visitorCount - 1}명` : ""}<Typography variant="caption" display="block" color="text.secondary">{x.company}</Typography></TableCell><TableCell>{x.hostName}<Typography variant="caption" display="block" color="text.secondary">{x.siteName} · {x.lobbyName}</Typography></TableCell><TableCell><StatusChip status={x.status} /></TableCell><TableCell align="right">{x.status === "PENDING_APPROVAL" && <Stack direction="row" justifyContent="flex-end"><Button size="small" color="error" onClick={() => void approval(x, false)}>반려</Button><Button size="small" onClick={() => void approval(x, true)}>승인</Button></Stack>}</TableCell></TableRow>)}
      {tab === "visitors" && visitors.map((x) => <TableRow key={String(x.id)}><TableCell>{String(x.name)}</TableCell><TableCell>{String(x.phone)}</TableCell><TableCell>{String(x.company || "-")}</TableCell><TableCell>{String(x.visitCount)}</TableCell><TableCell>{x.lastVisitAt ? new Date(String(x.lastVisitAt)).toLocaleDateString("ko-KR") : "-"}</TableCell></TableRow>)}
      {tab === "watchlist" && watch.map((x) => <TableRow key={String(x.id)}><TableCell>{String(x.name || "전화번호 기준")}</TableCell><TableCell>{String(x.company || "-")}</TableCell><TableCell>{String(x.reason)}</TableCell><TableCell>{new Date(String(x.startsAt)).toLocaleDateString("ko-KR")} – {x.endsAt ? new Date(String(x.endsAt)).toLocaleDateString("ko-KR") : "영구"}</TableCell><TableCell><Chip size="small" color={x.active ? "error" : "default"} label={x.active ? "활성" : "해제"} /></TableCell></TableRow>)}
    </TableBody></Table></TableContainer><Dialog open={watchOpen} onClose={() => setWatchOpen(false)} fullWidth><DialogTitle>Watch List 등록</DialogTitle><DialogContent><Alert severity="warning" sx={{ mb: 2 }}>제한 정보와 사유는 관리자·보안 담당자에게만 노출됩니다.</Alert><Grid container spacing={2}><Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth label="이름" value={watchForm.name} onChange={(e) => setWatchForm({ ...watchForm, name: e.target.value })} /></Grid><Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth label="전화번호" value={watchForm.phone} onChange={(e) => setWatchForm({ ...watchForm, phone: e.target.value })} /></Grid><Grid size={{ xs: 12 }}><TextField fullWidth label="회사" value={watchForm.company} onChange={(e) => setWatchForm({ ...watchForm, company: e.target.value })} /></Grid><Grid size={{ xs: 12 }}><TextField fullWidth required multiline label="제한 사유" value={watchForm.reason} onChange={(e) => setWatchForm({ ...watchForm, reason: e.target.value })} /></Grid></Grid></DialogContent><DialogActions><Button onClick={() => setWatchOpen(false)}>취소</Button><Button variant="contained" color="error" disabled={!watchForm.reason || (!watchForm.phone && !watchForm.company)} onClick={() => void addWatch()}>제한 등록</Button></DialogActions></Dialog></>;
}

function Resources({ setError }: { setError: (v: string) => void }) {
  const [ref, setRef] = useState<ReferenceData | null>(null); const [users, setUsers] = useState<Record<string, unknown>[]>([]); const [open, setOpen] = useState<"site" | "lobby" | "department" | null>(null); const [form, setForm] = useState<Record<string, string>>({});
  const load = async () => { try { const [r, u] = await Promise.all([api<ReferenceData>("/api/v1/reference-data"), api<{ items: Record<string, unknown>[] }>("/api/v1/admin/users")]); setRef(r); setUsers(u.items); } catch (e) { setError(e instanceof Error ? e.message : "기준정보를 불러오지 못했습니다"); } }; useEffect(() => { void load(); }, []);
  const save = async () => { if (!open) return; try { await postJSON(`/api/v1/admin/${open === "department" ? "organizations" : `${open}s`}`, form); setOpen(null); setForm({}); await load(); } catch (e) { setError(e instanceof Error ? e.message : "저장하지 못했습니다"); } };
  const updateUser = async (id: string, patch: Record<string, unknown>) => { try { await patchJSON(`/api/v1/admin/users/${id}`, patch); await load(); } catch (e) { setError(e instanceof Error ? e.message : "사용자 권한을 변경하지 못했습니다"); } };
  return <><PageHeader eyebrow="MASTER DATA" title="조직 · 사업장 · 권한" description="멀티 사업장과 로비, 조직 및 Keycloak 프로비저닝 사용자를 관리합니다." /><Grid container spacing={2}><Grid size={{ xs: 12, lg: 4 }}><ResourceCard title="사업장" items={ref?.sites ?? []} label={(x) => `${String(x.name)} · ${String(x.address)}`} onAdd={() => { setForm({}); setOpen("site"); }} /></Grid><Grid size={{ xs: 12, lg: 4 }}><ResourceCard title="로비" items={ref?.lobbies ?? []} label={(x) => String(x.name)} onAdd={() => { setForm({ siteId: ref?.sites[0]?.id ?? "" }); setOpen("lobby"); }} /></Grid><Grid size={{ xs: 12, lg: 4 }}><ResourceCard title="조직" items={ref?.departments ?? []} label={(x) => String(x.name)} onAdd={() => { setForm({}); setOpen("department"); }} /></Grid></Grid><Card sx={{ mt: 3 }}><CardContent><Typography variant="h6">사용자 · RBAC</Typography><Typography variant="body2" color="text.secondary" mb={2}>Keycloak 그룹 매핑 후에도 Role과 계정 활성 상태를 즉시 변경할 수 있습니다. 마지막 최고 관리자는 보호됩니다.</Typography><TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>사용자</TableCell><TableCell>아이디</TableCell><TableCell>인증</TableCell><TableCell>Role</TableCell><TableCell>상태</TableCell><TableCell>최근 로그인</TableCell></TableRow></TableHead><TableBody>{users.map((u) => <TableRow key={String(u.id)} sx={{ opacity: u.active === false ? .55 : 1 }}><TableCell>{String(u.displayName)}</TableCell><TableCell>{String(u.username)}</TableCell><TableCell>{String(u.source).toUpperCase()}</TableCell><TableCell><Stack spacing={.5} alignItems="flex-start"><TextField select value={String(u.role)} onChange={(e) => void updateUser(String(u.id), { role: e.target.value })} sx={{ minWidth: 170 }}>{["user", "lobby", "dept_manager", "security", "auditor", "admin", "super_admin"].map((role) => <MenuItem key={role} value={role}>{role}</MenuItem>)}</TextField>{u.source === "oidc" && u.roleOverride === true && <Button size="small" onClick={() => void updateUser(String(u.id), { useOidcMapping: true })}>SSO 그룹 매핑으로 복귀</Button>}</Stack></TableCell><TableCell><Button size="small" color={u.active === false ? "success" : "error"} variant="outlined" onClick={() => void updateUser(String(u.id), { active: u.active === false })}>{u.active === false ? "활성화" : "비활성화"}</Button></TableCell><TableCell>{u.lastLoginAt ? new Date(String(u.lastLoginAt)).toLocaleString("ko-KR") : "-"}</TableCell></TableRow>)}</TableBody></Table></TableContainer></CardContent></Card>
    <Dialog open={Boolean(open)} onClose={() => setOpen(null)} fullWidth><DialogTitle>{open === "site" ? "사업장" : open === "lobby" ? "로비" : "조직"} 추가</DialogTitle><DialogContent><Stack spacing={2} mt={1}>{open === "lobby" && <TextField select label="사업장" value={form.siteId ?? ""} onChange={(e) => setForm({ ...form, siteId: e.target.value })}>{ref?.sites.map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField>}{open !== "department" && <TextField required label="코드" value={form.code ?? ""} onChange={(e) => setForm({ ...form, code: e.target.value })} />}<TextField required label="이름" value={form.name ?? ""} onChange={(e) => setForm({ ...form, name: e.target.value })} />{open === "site" && <><TextField label="주소" value={form.address ?? ""} onChange={(e) => setForm({ ...form, address: e.target.value })} /><TextField label="지도 URL" value={form.mapUrl ?? ""} onChange={(e) => setForm({ ...form, mapUrl: e.target.value })} /></>}{open === "lobby" && <TextField multiline label="방문 안내" value={form.instructions ?? ""} onChange={(e) => setForm({ ...form, instructions: e.target.value })} />}</Stack></DialogContent><DialogActions><Button onClick={() => setOpen(null)}>취소</Button><Button variant="contained" disabled={!form.name || (open !== "department" && !form.code)} onClick={() => void save()}>저장</Button></DialogActions></Dialog></>;
}
function ResourceCard<T extends { id: string }>({ title, items, label, onAdd }: { title: string; items: T[]; label: (x: T) => string; onAdd: () => void }) { return <Card sx={{ height: "100%" }}><CardContent><Stack direction="row" justifyContent="space-between" alignItems="center"><Typography variant="h6">{title}</Typography><Button size="small" startIcon={<AddRounded />} onClick={onAdd}>추가</Button></Stack><Divider sx={{ my: 2 }} /><Stack spacing={1}>{items.map((x) => <Paper key={x.id} variant="outlined" sx={{ p: 1.5 }}><Typography variant="body2" fontWeight={700}>{label(x)}</Typography></Paper>)}{items.length === 0 && <Typography color="text.secondary">등록 항목 없음</Typography>}</Stack></CardContent></Card>; }

function Statistics({ setError }: { setError: (v: string) => void }) {
  const [data, setData] = useState<{ daily: { date: string; scheduled: number; checkedIn: number }[]; byDepartment: { name: string; count: number }[] }>({ daily: [], byDepartment: [] });
  const [notifications, setNotifications] = useState<Record<string, unknown>[]>([]);
  const [busy, setBusy] = useState("");
  const load = useCallback(async () => {
    try {
      const [statistics, list] = await Promise.all([
        api<typeof data>("/api/v1/admin/statistics?days=30"),
        api<{ items: Record<string, unknown>[] }>("/api/v1/admin/notifications"),
      ]);
      setData(statistics); setNotifications(list.items);
    } catch (e) { setError(e instanceof Error ? e.message : "통계를 불러오지 못했습니다"); }
  }, [setError]);
  useEffect(() => { void load(); }, [load]);
  const act = async (path: string, id: string) => {
    setBusy(id);
    try { await postJSON(path, {}); await load(); }
    catch (e) { setError(e instanceof Error ? e.message : "요청을 처리하지 못했습니다"); }
    finally { setBusy(""); }
  };
  const max = Math.max(1, ...data.byDepartment.map((x) => x.count));
  const failedCount = notifications.filter((n) => n.status === "failed").length;
  return <><PageHeader eyebrow="INSIGHTS" title="통계 · 알림" description="최근 30일 방문량과 부서별 수요, 메시지 API 발송 상태를 확인합니다." actions={<><Button startIcon={<DownloadRounded />} href="/api/v1/admin/statistics.csv?days=30">통계 CSV</Button>{failedCount > 0 && <Button variant="contained" color="error" startIcon={<ReplayRounded />} disabled={busy === "bulk"} onClick={() => void act("/api/v1/admin/notifications/retry-failed", "bulk")}>실패 {failedCount}건 일괄 재시도</Button>}</>} /><Grid container spacing={2}><Grid size={{ xs: 12, lg: 7 }}><Card><CardContent><Typography variant="h6">일별 방문 흐름</Typography><Box sx={{ mt: 3, display: "flex", alignItems: "end", gap: .6, height: 220 }}>{data.daily.map((d) => <Box key={d.date} title={`${d.date}: ${d.checkedIn}/${d.scheduled}`} sx={{ flex: 1, minWidth: 3, height: `${Math.max(3, d.scheduled * 6)}px`, maxHeight: 200, bgcolor: d.checkedIn === d.scheduled ? "primary.main" : "info.main", borderRadius: "4px 4px 0 0" }} />)}</Box><Typography variant="caption" color="text.secondary">막대: 예정 방문자 · 체크인 완료 시 녹색</Typography></CardContent></Card></Grid><Grid size={{ xs: 12, lg: 5 }}><Card><CardContent><Typography variant="h6">부서별 방문량</Typography><Stack spacing={2} mt={2}>{data.byDepartment.slice(0, 8).map((x) => <Box key={x.name}><Stack direction="row" justifyContent="space-between"><Typography variant="body2">{x.name}</Typography><Typography variant="body2" fontWeight={800}>{x.count}</Typography></Stack><LinearProgress variant="determinate" value={x.count / max * 100} sx={{ mt: .7, height: 6, borderRadius: 4 }} /></Box>)}</Stack></CardContent></Card></Grid></Grid><Card sx={{ mt: 3 }}><CardContent><Typography variant="h6">최근 알림 이력</Typography><TableContainer component={Paper} variant="outlined" sx={{ mt: 2 }}><Table size="small"><TableHead><TableRow><TableCell>생성 · 예약</TableCell><TableCell>수신자</TableCell><TableCell>채널 · API</TableCell><TableCell>규칙 · 템플릿</TableCell><TableCell>상태</TableCell><TableCell>시도</TableCell><TableCell>오류</TableCell><TableCell align="right">관리</TableCell></TableRow></TableHead><TableBody>{notifications.slice(0, 100).map((n) => <TableRow key={String(n.id)}><TableCell>{new Date(String(n.createdAt)).toLocaleString("ko-KR")}<Typography variant="caption" display="block" color="text.secondary">예약 {new Date(String(n.nextAttemptAt)).toLocaleString("ko-KR")}</Typography></TableCell><TableCell>{String(n.recipient)}</TableCell><TableCell>{String(n.channel).toUpperCase()} · {String(n.apiConfigName)}</TableCell><TableCell>{String(n.ruleName || "-")}<Typography variant="caption" display="block" color="text.secondary">{String(n.templateKey)}</Typography></TableCell><TableCell><Chip size="small" color={n.status === "failed" ? "error" : n.status === "sent" || n.status === "logged" ? "success" : "default"} label={String(n.status)} /></TableCell><TableCell>{String(n.attempts)}</TableCell><TableCell>{String(n.error || "-")}</TableCell><TableCell align="right">{(n.status === "failed" || n.status === "cancelled") && <Button size="small" startIcon={<ReplayRounded />} disabled={busy === String(n.id)} onClick={() => void act(`/api/v1/admin/notifications/${String(n.id)}/retry`, String(n.id))}>재시도</Button>}{(n.status === "queued" || n.status === "failed") && <Button size="small" color="error" startIcon={<BlockRounded />} disabled={busy === String(n.id)} onClick={() => void act(`/api/v1/admin/notifications/${String(n.id)}/cancel`, String(n.id))}>취소</Button>}</TableCell></TableRow>)}</TableBody></Table></TableContainer></CardContent></Card></>;
}

// Operations groups the two device- and policy-level registries an administrator
// maintains rarely but must be able to change without a redeploy.
function Operations({ setError }: { setError: (v: string) => void }) {
  const [types, setTypes] = useState<VisitType[]>([]);
  const [devices, setDevices] = useState<KioskDevice[]>([]);
  const [reference, setReference] = useState<ReferenceData | null>(null);
  const [typeForm, setTypeForm] = useState<VisitType | null>(null);
  const [deviceForm, setDeviceForm] = useState<{ name: string; siteId: string; lobbyId: string; validDays: number } | null>(null);
  const [issued, setIssued] = useState<{ token: string; enrollPath: string } | null>(null);
  const load = useCallback(async () => {
    try {
      const [typeData, deviceData, ref] = await Promise.all([
        api<{ items: VisitType[] }>("/api/v1/admin/visit-types"),
        api<{ items: KioskDevice[] }>("/api/v1/admin/kiosk-devices"),
        api<ReferenceData>("/api/v1/reference-data"),
      ]);
      setTypes(typeData.items); setDevices(deviceData.items); setReference(ref);
    } catch (e) { setError(e instanceof Error ? e.message : "운영 설정을 불러오지 못했습니다"); }
  }, [setError]);
  useEffect(() => { void load(); }, [load]);

  const saveType = async () => {
    if (!typeForm) return;
    try {
      if (typeForm.id) await putJSON(`/api/v1/admin/visit-types/${typeForm.id}`, typeForm);
      else await postJSON("/api/v1/admin/visit-types", typeForm);
      setTypeForm(null); await load();
    } catch (e) { setError(e instanceof Error ? e.message : "방문 유형을 저장하지 못했습니다"); }
  };
  const disableType = async (id: string) => {
    if (!window.confirm("이 방문 유형을 비활성화할까요? 기존 방문 기록은 그대로 유지됩니다.")) return;
    try { await api(`/api/v1/admin/visit-types/${id}`, { method: "DELETE" }); await load(); }
    catch (e) { setError(e instanceof Error ? e.message : "비활성화하지 못했습니다"); }
  };
  const createDevice = async () => {
    if (!deviceForm) return;
    try {
      const result = await postJSON<{ token: string; enrollPath: string }>("/api/v1/admin/kiosk-devices", deviceForm);
      setIssued(result); setDeviceForm(null); await load();
    } catch (e) { setError(e instanceof Error ? e.message : "키오스크 기기를 등록하지 못했습니다"); }
  };
  const revokeDevice = async (id: string) => {
    if (!window.confirm("이 기기의 접근을 즉시 폐기할까요?")) return;
    try { await api(`/api/v1/admin/kiosk-devices/${id}`, { method: "DELETE" }); await load(); }
    catch (e) { setError(e instanceof Error ? e.message : "폐기하지 못했습니다"); }
  };
  const emptyType = (): VisitType => ({ id: "", code: "", name: "", description: "", requiresNda: false, requiresSafetyBriefing: false, requiresVehicle: false, requiresEquipment: false, requiresApproval: false, active: true, sortOrder: 100 });

  return <>
    <PageHeader eyebrow="SITE POLICY" title="방문 유형 · 키오스크" description="방문 유형별 필수 확인 항목과 무인 로비 태블릿 기기를 관리합니다." />
    <Card><CardContent>
      <Stack direction="row" justifyContent="space-between" alignItems="center" mb={2}>
        <Box><Typography variant="h6">방문 유형 · 체크리스트</Typography><Typography variant="body2" color="text.secondary">보안서약, 안전교육, 차량·장비 신고와 승인 강제를 유형별로 지정합니다.</Typography></Box>
        <Button variant="contained" startIcon={<AddRounded />} onClick={() => setTypeForm(emptyType())}>유형 추가</Button>
      </Stack>
      <TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>코드</TableCell><TableCell>이름</TableCell><TableCell>필수 확인</TableCell><TableCell>승인</TableCell><TableCell>상태</TableCell><TableCell align="right">관리</TableCell></TableRow></TableHead><TableBody>
        {types.map((item) => <TableRow key={item.id}>
          <TableCell sx={{ fontFamily: "monospace" }}>{item.code}</TableCell>
          <TableCell><Typography variant="body2" fontWeight={750}>{item.name}</Typography><Typography variant="caption" color="text.secondary">{item.description}</Typography></TableCell>
          <TableCell><Stack direction="row" spacing={.5} flexWrap="wrap">{[["보안서약", item.requiresNda], ["안전교육", item.requiresSafetyBriefing], ["차량", item.requiresVehicle], ["장비", item.requiresEquipment]].filter(([, on]) => on).map(([label]) => <Chip key={String(label)} size="small" label={String(label)} />)}</Stack></TableCell>
          <TableCell>{item.requiresApproval ? <Chip size="small" color="warning" label="필수" /> : "-"}</TableCell>
          <TableCell><Chip size="small" color={item.active ? "success" : "default"} label={item.active ? "사용" : "중지"} /></TableCell>
          <TableCell align="right"><Button size="small" onClick={() => setTypeForm({ ...item })}>수정</Button>{item.active && <Button size="small" color="error" onClick={() => void disableType(item.id)}>중지</Button>}</TableCell>
        </TableRow>)}
        {types.length === 0 && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 4, color: "text.secondary" }}>등록된 방문 유형이 없습니다.</TableCell></TableRow>}
      </TableBody></Table></TableContainer>
    </CardContent></Card>

    <Card sx={{ mt: 3 }}><CardContent>
      <Stack direction="row" justifyContent="space-between" alignItems="center" mb={2}>
        <Box><Typography variant="h6">로비 키오스크 기기</Typography><Typography variant="body2" color="text.secondary">기기 토큰은 로비 API에만 접근하며 발급 시 한 번만 표시됩니다.</Typography></Box>
        <Button variant="contained" startIcon={<AddRounded />} onClick={() => setDeviceForm({ name: "", siteId: reference?.sites[0]?.id ?? "", lobbyId: "", validDays: 365 })}>기기 등록</Button>
      </Stack>
      {issued && <Alert severity="success" sx={{ mb: 2 }} onClose={() => setIssued(null)} action={<Button color="inherit" size="small" startIcon={<ContentCopyRounded />} onClick={() => void navigator.clipboard.writeText(`${window.location.origin}${issued.enrollPath}`)}>링크 복사</Button>}>
        태블릿에서 <strong>{window.location.origin}{issued.enrollPath}</strong> 를 한 번 열면 등록이 끝납니다. 이 토큰은 다시 표시되지 않습니다.
      </Alert>}
      <TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>기기</TableCell><TableCell>사업장 · 로비</TableCell><TableCell>토큰</TableCell><TableCell>만료</TableCell><TableCell>최근 사용</TableCell><TableCell>상태</TableCell><TableCell align="right">관리</TableCell></TableRow></TableHead><TableBody>
        {devices.map((item) => <TableRow key={item.id}>
          <TableCell><Typography variant="body2" fontWeight={750}>{item.name}</Typography></TableCell>
          <TableCell>{item.siteName}{item.lobbyName ? ` · ${item.lobbyName}` : ""}</TableCell>
          <TableCell sx={{ fontFamily: "monospace" }}>{item.prefix}…</TableCell>
          <TableCell>{item.expiresAt ? new Date(item.expiresAt).toLocaleDateString("ko-KR") : "무기한"}</TableCell>
          <TableCell>{item.lastSeenAt ? new Date(item.lastSeenAt).toLocaleString("ko-KR") : "-"}</TableCell>
          <TableCell><Chip size="small" color={item.active ? "success" : "default"} label={item.active ? "사용" : "폐기"} /></TableCell>
          <TableCell align="right">{item.active && <Button size="small" color="error" onClick={() => void revokeDevice(item.id)}>폐기</Button>}</TableCell>
        </TableRow>)}
        {devices.length === 0 && <TableRow><TableCell colSpan={7} align="center" sx={{ py: 4, color: "text.secondary" }}>등록된 키오스크 기기가 없습니다.</TableCell></TableRow>}
      </TableBody></Table></TableContainer>
    </CardContent></Card>

    <Dialog open={Boolean(typeForm)} onClose={() => setTypeForm(null)} fullWidth maxWidth="sm"><DialogTitle>{typeForm?.id ? "방문 유형 수정" : "방문 유형 추가"}</DialogTitle><DialogContent dividers>{typeForm && <Stack spacing={2} mt={1}>
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, sm: 4 }}><TextField fullWidth required label="코드" value={typeForm.code} onChange={(e) => setTypeForm({ ...typeForm, code: e.target.value.toUpperCase() })} /></Grid>
        <Grid size={{ xs: 12, sm: 8 }}><TextField fullWidth required label="이름" value={typeForm.name} onChange={(e) => setTypeForm({ ...typeForm, name: e.target.value })} /></Grid>
        <Grid size={{ xs: 12 }}><TextField fullWidth multiline label="설명" value={typeForm.description ?? ""} onChange={(e) => setTypeForm({ ...typeForm, description: e.target.value })} /></Grid>
        <Grid size={{ xs: 6 }}><TextField fullWidth type="number" label="정렬 순서" value={typeForm.sortOrder} onChange={(e) => setTypeForm({ ...typeForm, sortOrder: Number(e.target.value) })} /></Grid>
      </Grid>
      {[["requiresNda", "보안서약 확인 필수"], ["requiresSafetyBriefing", "안전교육 이수 확인 필수"], ["requiresVehicle", "차량번호 신고 필수"], ["requiresEquipment", "반입 장비 신고 필수"], ["requiresApproval", "승인 Workflow 강제"], ["active", "사용"]].map(([key, label]) => <Box key={key}><label style={{ display: "flex", alignItems: "center", gap: 8 }}><input type="checkbox" checked={Boolean(typeForm[key as keyof VisitType])} onChange={(e) => setTypeForm({ ...typeForm, [key]: e.target.checked })} /><Typography variant="body2">{label}</Typography></label></Box>)}
    </Stack>}</DialogContent><DialogActions><Button onClick={() => setTypeForm(null)}>취소</Button><Button variant="contained" disabled={!typeForm?.code || !typeForm?.name} onClick={() => void saveType()}>저장</Button></DialogActions></Dialog>

    <Dialog open={Boolean(deviceForm)} onClose={() => setDeviceForm(null)} fullWidth maxWidth="sm"><DialogTitle>키오스크 기기 등록</DialogTitle><DialogContent dividers>{deviceForm && <Stack spacing={2} mt={1}>
      <TextField required label="기기 이름" value={deviceForm.name} onChange={(e) => setDeviceForm({ ...deviceForm, name: e.target.value })} placeholder="본사 1층 안내데스크 태블릿" />
      <TextField select required label="사업장" value={deviceForm.siteId} onChange={(e) => setDeviceForm({ ...deviceForm, siteId: e.target.value, lobbyId: "" })}>{reference?.sites.map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField>
      <TextField select label="로비" value={deviceForm.lobbyId} onChange={(e) => setDeviceForm({ ...deviceForm, lobbyId: e.target.value })}><MenuItem value="">미지정</MenuItem>{(reference?.lobbies ?? []).filter((x) => x.siteId === deviceForm.siteId).map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField>
      <TextField type="number" label="유효기간 (일)" value={deviceForm.validDays} onChange={(e) => setDeviceForm({ ...deviceForm, validDays: Number(e.target.value) })} helperText="0을 입력하면 무기한입니다." />
    </Stack>}</DialogContent><DialogActions><Button onClick={() => setDeviceForm(null)}>취소</Button><Button variant="contained" disabled={!deviceForm?.name || !deviceForm?.siteId} onClick={() => void createDevice()}>토큰 발급</Button></DialogActions></Dialog>
  </>;
}

function Audit({ setError }: { setError: (v: string) => void }) {
  const [items, setItems] = useState<Record<string, unknown>[]>([]); useEffect(() => { api<{ items: Record<string, unknown>[] }>("/api/v1/admin/audit-logs?limit=300").then((x) => setItems(x.items)).catch((e) => setError(e.message)); }, [setError]);
  return <><PageHeader eyebrow="IMMUTABLE TRAIL" title="Audit Log" description="로그인, 개인정보 조회, QR 검증과 모든 운영 변경을 추적합니다." actions={<Button startIcon={<DownloadRounded />} href="/api/v1/admin/audit-logs.csv">감사 로그 CSV</Button>} /><TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>시간</TableCell><TableCell>행위자</TableCell><TableCell>이벤트</TableCell><TableCell>대상</TableCell><TableCell>IP</TableCell><TableCell>상세</TableCell></TableRow></TableHead><TableBody>{items.map((x) => <TableRow key={String(x.id)}><TableCell sx={{ whiteSpace: "nowrap" }}>{new Date(String(x.createdAt)).toLocaleString("ko-KR")}</TableCell><TableCell>{String(x.actor)}</TableCell><TableCell><Chip size="small" label={String(x.action)} /></TableCell><TableCell>{String(x.resourceType)} · {String(x.resourceId || "-")}</TableCell><TableCell>{String(x.ipAddress)}</TableCell><TableCell><Typography variant="caption" sx={{ fontFamily: "monospace", wordBreak: "break-all" }}>{JSON.stringify(x.details)}</Typography></TableCell></TableRow>)}</TableBody></Table></TableContainer></>;
}

function APIPage() { const tools = ["search_visits", "get_today_visitors", "get_current_visitors", "get_visit", "create_visit", "cancel_visit", "search_visitor_history", "get_lobby_status", "get_visit_statistics"]; return <><PageHeader eyebrow="INTEGRATION" title="API / MCP" description="사내 시스템과 AI Agent가 동일한 RBAC·감사 경계를 사용합니다." actions={<Button variant="outlined" endIcon={<OpenInNewRounded />} href="/api/v1/openapi.json" target="_blank">OpenAPI 3.1</Button>} /><Grid container spacing={2}><Grid size={{ xs: 12, md: 5 }}><Card><CardContent><Typography variant="h6">MCP Streamable HTTP</Typography><Typography color="text.secondary" mt={1}>개인 API 키의 <code>mcp</code> 범위로 연결합니다.</Typography><Paper variant="outlined" sx={{ p: 2, mt: 2, bgcolor: "#10231F", color: "#DDF3EB", fontFamily: "monospace", wordBreak: "break-all" }}>{window.location.origin}/mcp<br /><br />Authorization: Bearer vf_…</Paper><Alert severity="info" sx={{ mt: 2 }}>도구 결과의 방문자 이름과 전화번호는 기본 마스킹되며 사용자·부서·사업장 범위가 자동 적용됩니다.</Alert></CardContent></Card></Grid><Grid size={{ xs: 12, md: 7 }}><Card><CardContent><Typography variant="h6">제공 Tool</Typography><Grid container spacing={1.2} mt={1}>{tools.map((tool) => <Grid key={tool} size={{ xs: 12, sm: 6 }}><Paper variant="outlined" sx={{ p: 1.5 }}><Typography variant="body2" fontWeight={750} sx={{ fontFamily: "monospace" }}>{tool}</Typography></Paper></Grid>)}</Grid></CardContent></Card></Grid></Grid></>; }
