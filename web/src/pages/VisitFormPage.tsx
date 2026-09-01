import { useEffect, useMemo, useState } from "react";
import { Alert, Autocomplete, Box, Button, Card, CardContent, Checkbox, Chip, Divider, FormControlLabel, Grid, IconButton, MenuItem, Paper, Stack, TextField, Typography } from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import SendRounded from "@mui/icons-material/SendRounded";
import ContentCopyRounded from "@mui/icons-material/ContentCopyRounded";
import UploadFileRounded from "@mui/icons-material/UploadFileRounded";
import { useNavigate } from "react-router-dom";
import { api, postJSON } from "../api";
import type { FrequentVisitor, ReferenceData, VisitTemplate } from "../types";
import { PageHeader } from "../components/AdminUI";

type VisitorDraft = { name: string; phone: string; email: string; company: string; title: string; vehicle: string; equipment: string; consent: boolean };
const blankVisitor = (): VisitorDraft => ({ name: "", phone: "", email: "", company: "", title: "", vehicle: "", equipment: "", consent: true });
const templateVisitorDraft = (visitor: FrequentVisitor): VisitorDraft => ({
  name: visitor.name,
  phone: visitor.phone,
  email: visitor.email ?? "",
  company: visitor.company ?? "",
  title: visitor.title ?? "",
  vehicle: visitor.vehicle ?? "",
  equipment: visitor.equipment.join(", "),
  consent: true,
});
const localInput = (date: Date) => { const shifted = new Date(date.getTime() - date.getTimezoneOffset() * 60000); return shifted.toISOString().slice(0, 16); };

export function VisitFormPage({ walkIn = false }: { walkIn?: boolean }) {
  const navigate = useNavigate();
  const now = useMemo(() => new Date(), []);
  const [ref, setRef] = useState<ReferenceData | null>(null);
  const [siteId, setSiteId] = useState(""); const [lobbyId, setLobbyId] = useState(""); const [departmentId, setDepartmentId] = useState(""); const [hostUserId, setHostUserId] = useState("");
  const [startAt, setStartAt] = useState(localInput(new Date(now.getTime() + (walkIn ? 0 : 60) * 60000))); const [endAt, setEndAt] = useState(localInput(new Date(now.getTime() + (walkIn ? 120 : 180) * 60000)));
  const [purpose, setPurpose] = useState(""); const [placeDetail, setPlaceDetail] = useState(""); const [notes, setNotes] = useState(""); const [visitors, setVisitors] = useState<VisitorDraft[]>([blankVisitor()]);
  const [repeatWeekly, setRepeatWeekly] = useState(false); const [repeatCount, setRepeatCount] = useState(2); const [importWarnings, setImportWarnings] = useState<string[]>([]);
  const [busy, setBusy] = useState(false); const [templateLoading, setTemplateLoading] = useState(false); const [error, setError] = useState(""); const [created, setCreated] = useState<{ requestNo: string; status: string; passUrls?: string[] } | null>(null);
  useEffect(() => { api<ReferenceData>("/api/v1/reference-data").then((x) => { setRef(x); if (x.sites[0]) setSiteId(x.sites[0].id); }).catch((e) => setError(e.message)); }, []);
  useEffect(() => {
    if (walkIn) return undefined;
    const templateID = sessionStorage.getItem("visitflow_template_id");
    localStorage.removeItem("visitflow_template");
    if (!templateID) return undefined;
    let active = true;
    setTemplateLoading(true);
    api<VisitTemplate>(`/api/v1/visit-templates/${encodeURIComponent(templateID)}`).then((template) => {
      if (!active) return;
      setPurpose(template.payload.purpose ?? ""); setPlaceDetail(template.payload.placeDetail ?? "");
      const frequent = template.frequentVisitors ?? [];
      if (frequent.length > 0) {
        setVisitors(frequent.map(templateVisitorDraft));
      } else if (template.payload.company) {
        setVisitors([{ ...blankVisitor(), company: template.payload.company }]);
      }
      sessionStorage.removeItem("visitflow_template_id");
    }).catch((e) => {
      if (active) setError(e instanceof Error ? e.message : "방문 템플릿을 불러오지 못했습니다");
    }).finally(() => {
      if (active) setTemplateLoading(false);
    });
    return () => { active = false; };
  }, [walkIn]);
  const siteLobbies = ref?.lobbies.filter((x) => x.siteId === siteId) ?? [];
  useEffect(() => { if (siteLobbies.length && !siteLobbies.some((x) => x.id === lobbyId)) setLobbyId(siteLobbies[0].id); }, [siteLobbies, lobbyId]);
  const updateVisitor = (index: number, patch: Partial<VisitorDraft>) => setVisitors((list) => list.map((x, i) => i === index ? { ...x, ...patch } : x));
  const submit = async () => {
    setBusy(true); setError("");
    try {
      const body = { siteId, lobbyId, departmentId, hostUserId: walkIn ? hostUserId : undefined, startAt: new Date(startAt).toISOString(), endAt: new Date(endAt).toISOString(), purpose, placeDetail, notes, recurrence: !walkIn && repeatWeekly ? { frequency: "weekly", occurrences: repeatCount } : undefined, visitors: visitors.map((v) => ({ ...v, equipment: v.equipment.split(",").map((x) => x.trim()).filter(Boolean) })) };
      const result = await postJSON<{ requestNo: string; status: string; passUrls?: string[] }>(walkIn ? "/api/v1/lobby/walk-ins" : "/api/v1/visits", body); sessionStorage.removeItem("visitflow_template_id"); setCreated(result);
    } catch (e) { setError(e instanceof Error ? e.message : "방문을 등록하지 못했습니다"); } finally { setBusy(false); }
  };
  const importVisitors = async (file?: File) => {
    if (!file) return;
    setError(""); setImportWarnings([]);
    try {
      const form = new FormData(); form.append("file", file);
      const result = await api<{ visitors: Array<Omit<VisitorDraft, "equipment"> & { equipment?: string[] }>; warnings: string[] }>("/api/v1/visits/import/preview", { method: "POST", body: form });
      setVisitors(result.visitors.map((visitor) => ({ ...blankVisitor(), ...visitor, equipment: (visitor.equipment ?? []).join(", ") })));
      setImportWarnings(result.warnings);
    } catch (e) { setError(e instanceof Error ? e.message : "방문자 파일을 읽지 못했습니다"); }
  };
  if (created) return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 900, mx: "auto" }}><Card><CardContent sx={{ p: { xs: 3, md: 6 }, textAlign: "center" }}><Box sx={{ width: 72, height: 72, borderRadius: "50%", bgcolor: "#E4F3EC", color: "success.main", display: "grid", placeItems: "center", mx: "auto", fontSize: 34 }}>✓</Box><Typography variant="h4" sx={{ mt: 2 }}>방문 등록 완료</Typography><Typography color="text.secondary" sx={{ mt: 1 }}>방문번호 <strong>{created.requestNo}</strong> · 상태 {created.status}</Typography>{created.passUrls?.map((url) => <Paper key={url} variant="outlined" sx={{ mt: 2, p: 1.5, display: "flex", alignItems: "center", gap: 1, wordBreak: "break-all" }}><Typography variant="body2" sx={{ flex: 1 }}>{url}</Typography><IconButton onClick={() => void navigator.clipboard.writeText(url)}><ContentCopyRounded /></IconButton></Paper>)}<Alert severity="info" sx={{ mt: 3, textAlign: "left" }}>{walkIn ? "현장 방문자를 즉시 체크인하고 담당자 도착 알림을 대기열에 등록했습니다." : created.status === "PENDING_APPROVAL" ? "승인 완료 후 방문자별 QR 방문증이 발급되고 알림 큐에 등록됩니다." : "방문자별 모바일 방문증이 발급되었고 관리자가 설정한 메시지 발송 규칙에 등록됩니다."}</Alert><Stack direction="row" justifyContent="center" spacing={1} mt={3}><Button onClick={() => navigate(walkIn ? "/lobby" : "/visits")}>목록으로</Button><Button variant="contained" onClick={() => window.location.reload()}>새로 등록</Button></Stack></CardContent></Card></Box>;
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1100, mx: "auto" }}><PageHeader eyebrow={walkIn ? "WALK-IN" : "NEW VISIT"} title={walkIn ? "현장 방문 등록" : "방문 신청"} description={walkIn ? "예약 없이 도착한 방문자를 담당자와 연결해 등록합니다." : "여러 방문자도 한 번에 등록하고 각각 고유한 QR 방문증을 발급합니다."} />{error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}{templateLoading && <Alert severity="info" sx={{ mb: 2 }}>선택한 템플릿과 자주 방문자를 불러오는 중입니다.</Alert>}
    <Card sx={{ opacity: templateLoading ? 0.65 : 1, pointerEvents: templateLoading ? "none" : "auto" }}><CardContent sx={{ p: { xs: 2, md: 4 } }}><Typography variant="h6">방문 일정</Typography><Typography variant="body2" color="text.secondary" mb={3}>언제, 어디에서, 누구를 만나는지 입력하세요.</Typography><Grid container spacing={2}>
      <Grid size={{ xs: 12, md: 6 }}><TextField select fullWidth required label="사업장" value={siteId} onChange={(e) => setSiteId(e.target.value)}>{ref?.sites.map((x) => <MenuItem key={x.id} value={x.id}>{x.name} · {x.address}</MenuItem>)}</TextField></Grid>
      <Grid size={{ xs: 12, md: 6 }}><TextField select fullWidth label="로비" value={lobbyId} onChange={(e) => setLobbyId(e.target.value)}>{siteLobbies.map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField></Grid>
      {walkIn && <Grid size={{ xs: 12 }}><Autocomplete options={ref?.hosts ?? []} getOptionLabel={(x) => `${x.name}${x.email ? ` · ${x.email}` : ""}`} value={(ref?.hosts ?? []).find((x) => x.id === hostUserId) ?? null} onChange={(_, x) => { setHostUserId(x?.id ?? ""); setDepartmentId(x?.departmentId ?? ""); }} renderInput={(params) => <TextField {...params} required label="방문 담당자 검색" helperText="현장 방문 승인을 요청할 사내 담당자" />} /></Grid>}
      <Grid size={{ xs: 12, md: 6 }}><TextField fullWidth required type="datetime-local" label="방문 시작" value={startAt} onChange={(e) => setStartAt(e.target.value)} slotProps={{ inputLabel: { shrink: true } }} /></Grid><Grid size={{ xs: 12, md: 6 }}><TextField fullWidth required type="datetime-local" label="방문 종료" value={endAt} onChange={(e) => setEndAt(e.target.value)} slotProps={{ inputLabel: { shrink: true } }} /></Grid>
      <Grid size={{ xs: 12, md: 6 }}><TextField select fullWidth label="대상 부서" value={departmentId} onChange={(e) => setDepartmentId(e.target.value)}><MenuItem value="">자동 / 미지정</MenuItem>{ref?.departments.map((x) => <MenuItem key={x.id} value={x.id}>{x.name}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 12, md: 6 }}><TextField fullWidth label="세부 방문 장소" value={placeDetail} onChange={(e) => setPlaceDetail(e.target.value)} placeholder="예: 본관 8층 회의실 A" /></Grid>
      <Grid size={{ xs: 12 }}><TextField fullWidth required label="방문 목적" value={purpose} onChange={(e) => setPurpose(e.target.value)} /></Grid><Grid size={{ xs: 12 }}><TextField fullWidth multiline minRows={2} label="담당자 메모" value={notes} onChange={(e) => setNotes(e.target.value)} /></Grid>
      {!walkIn && <Grid size={{ xs: 12 }}><Stack direction={{ xs: "column", sm: "row" }} spacing={2} alignItems={{ sm: "center" }}><FormControlLabel control={<Checkbox checked={repeatWeekly} onChange={(e) => setRepeatWeekly(e.target.checked)} />} label="매주 같은 시간으로 반복 예약" />{repeatWeekly && <TextField type="number" label="총 예약 횟수" value={repeatCount} onChange={(e) => setRepeatCount(Math.max(2, Math.min(52, Number(e.target.value))))} slotProps={{ htmlInput: { min: 2, max: 52 } }} helperText="현재 일정을 포함해 최대 52회" />}</Stack></Grid>}
    </Grid><Divider sx={{ my: 4 }} /><Stack direction="row" alignItems="center" justifyContent="space-between" mb={2}><Box><Typography variant="h6">방문자</Typography><Typography variant="body2" color="text.secondary">최대 100명까지 한 신청에 등록할 수 있습니다.</Typography></Box><Button startIcon={<AddRounded />} onClick={() => setVisitors((x) => [...x, blankVisitor()])}>방문자 추가</Button></Stack>
    <Stack direction={{ xs: "column", sm: "row" }} spacing={1} mb={2}><Button component="label" variant="outlined" startIcon={<UploadFileRounded />}>CSV / XLSX 가져오기<input hidden type="file" accept=".csv,.xlsx" onChange={(e) => { void importVisitors(e.target.files?.[0]); e.currentTarget.value = ""; }} /></Button><Typography variant="body2" color="text.secondary" alignSelf={{ sm: "center" }}>첫 행 열 이름: 이름, 휴대전화, 회사명, 이메일, 직책, 차량번호, 반입장비, 개인정보동의</Typography></Stack>
    {importWarnings.length > 0 && <Alert severity="warning" sx={{ mb: 2 }}>{importWarnings.slice(0, 5).join(" · ")}{importWarnings.length > 5 ? ` 외 ${importWarnings.length - 5}건` : ""}</Alert>}
    <Stack spacing={2}>{visitors.map((visitor, index) => <Paper key={index} variant="outlined" sx={{ p: { xs: 2, md: 3 }, borderRadius: 3 }}><Stack direction="row" justifyContent="space-between" alignItems="center" mb={2}><Stack direction="row" spacing={1} alignItems="center"><Typography fontWeight={800}>방문자 {index + 1}</Typography>{index === 0 && <Chip size="small" label="대표 방문자" color="primary" variant="outlined" />}</Stack><IconButton color="error" disabled={visitors.length === 1} onClick={() => setVisitors((x) => x.filter((_, i) => i !== index))}><DeleteOutlineRounded /></IconButton></Stack><Grid container spacing={2}>
      <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth required label="이름" value={visitor.name} onChange={(e) => updateVisitor(index, { name: e.target.value })} /></Grid><Grid size={{ xs: 12, md: 4 }}><TextField fullWidth required label="휴대전화" value={visitor.phone} onChange={(e) => updateVisitor(index, { phone: e.target.value })} placeholder="010-0000-0000" /></Grid><Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="회사명" value={visitor.company} onChange={(e) => updateVisitor(index, { company: e.target.value })} /></Grid>
      <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="이메일" value={visitor.email} onChange={(e) => updateVisitor(index, { email: e.target.value })} /></Grid><Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="직책" value={visitor.title} onChange={(e) => updateVisitor(index, { title: e.target.value })} /></Grid><Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="차량번호" value={visitor.vehicle} onChange={(e) => updateVisitor(index, { vehicle: e.target.value })} /></Grid>
      <Grid size={{ xs: 12 }}><TextField fullWidth label="반입 장비" value={visitor.equipment} onChange={(e) => updateVisitor(index, { equipment: e.target.value })} helperText="노트북, 카메라, 저장장치처럼 쉼표로 구분" /></Grid><Grid size={{ xs: 12 }}><FormControlLabel control={<Checkbox checked={visitor.consent} onChange={(e) => updateVisitor(index, { consent: e.target.checked })} />} label="방문자 개인정보 수집·이용 동의를 확인했습니다." /></Grid>
    </Grid></Paper>)}</Stack><Divider sx={{ my: 3 }} /><Stack direction={{ xs: "column-reverse", sm: "row" }} justifyContent="flex-end" spacing={1}><Button onClick={() => navigate(-1)}>취소</Button><Button variant="contained" endIcon={<SendRounded />} disabled={busy || templateLoading || !siteId || !purpose || visitors.some((x) => !x.name || !x.phone || !x.consent) || (walkIn && !hostUserId)} onClick={() => void submit()}>{busy ? "등록 중…" : walkIn ? "현장 방문 등록" : "방문 신청 제출"}</Button></Stack></CardContent></Card>
  </Box>;
}
