import { useEffect, useMemo, useState } from "react";
import {
  Alert, Box, Button, Card, CardContent, Chip, Dialog, DialogActions, DialogContent, DialogTitle,
  FormControlLabel, Grid, MenuItem, Paper, Stack, Switch, Table, TableBody, TableCell, TableContainer,
  TableHead, TableRow, TextField, Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import EditOutlined from "@mui/icons-material/EditOutlined";
import { api, postJSON, putJSON } from "../api";
import { PageHeader } from "../components/AdminUI";

type Channel = "sms" | "mms" | "kakao" | "webhook";
type NotificationAPI = {
  id: string; name: string; channel: Channel; baseUrl: string; path: string; method: string;
  requestFormat: string; headers: Record<string, string>; parameters: Record<string, string>;
  secretKeys: string[]; timeoutSeconds: number; enabled: boolean;
};
type NotificationRule = {
  id: string; name: string; event: string; audience: string; channel: Channel; apiConfigId?: string;
  apiConfigName?: string; offsetMinutes: number; templateKey: string; bodyTemplate: string; locale: string; enabled: boolean;
};
type APIForm = Omit<NotificationAPI, "id" | "headers" | "parameters" | "secretKeys"> & {
  id?: string; headersJSON: string; parametersJSON: string; secretKeysText: string;
};
type RuleForm = Omit<NotificationRule, "id" | "apiConfigName"> & { id?: string };

const channelLabels: Record<Channel, string> = { sms: "SMS", mms: "MMS", kakao: "카카오톡", webhook: "외부 시스템 연동" };
const eventLabels: Record<string, string> = {
  visit_confirmed: "방문 확정 시", visit_start: "방문 시작 기준", checked_in: "체크인 시",
  checked_out: "체크아웃 시", visit_cancelled: "방문 취소 시", visit_rejected: "방문 반려 시", approval_escalated: "승인 지연 시",
};
const audienceLabels: Record<string, string> = { visitor: "방문자", host: "담당자", system: "외부 시스템" };
const localeLabels: Record<string, string> = { "": "모든 언어", ko: "한국어", en: "English", ja: "日本語", zh: "中文" };
const placeholders = "{{recipient}} {{message}} {{visitor}} {{visitorCompany}} {{host}} {{delegate}} {{company}} {{start}} {{end}} {{place}} {{lobby}} {{requestNo}} {{passUrl}} {{qrcodeFileSeq}} {{qrcodePath}} {{qrcodeUrl}} {{visitType}} {{badgeNo}} {{siteCode}} {{visitId}} {{visitorVisitId}} {{idempotencyKey}}";
const rulePlaceholders = "{{recipient}} {{channel}} {{visitor}} {{visitorCompany}} {{host}} {{delegate}} {{company}} {{start}} {{end}} {{place}} {{lobby}} {{requestNo}} {{passUrl}} {{qrcodeFileSeq}} {{qrcodePath}} {{qrcodeUrl}} {{visitType}} {{badgeNo}} {{siteCode}} {{locale}}";

const emptyAPI = (): APIForm => ({
  name: "", channel: "sms", baseUrl: "", path: "/", method: "POST", requestFormat: "json",
  headersJSON: "{}", parametersJSON: JSON.stringify({ recipient: "{{recipient}}", message: "{{message}}", idempotencyKey: "{{idempotencyKey}}" }, null, 2),
  secretKeysText: "", timeoutSeconds: 10, enabled: true,
});
const emptyRule = (): RuleForm => ({
  name: "", event: "visit_confirmed", audience: "visitor", channel: "sms", apiConfigId: "",
  offsetMinutes: 0, templateKey: "visitor_message", bodyTemplate: "{{visitor}}님, {{start}} 방문 안내입니다. {{passUrl}}", locale: "", enabled: true,
});

function mapJSON(value: string, label: string): Record<string, string> {
  const parsed: unknown = JSON.parse(value || "{}");
  if (!parsed || Array.isArray(parsed) || typeof parsed !== "object" || Object.values(parsed).some((item) => typeof item !== "string")) {
    throw new Error(`${label}는 문자열 값만 가진 JSON 객체여야 합니다.`);
  }
  return parsed as Record<string, string>;
}

export function NotificationSettingsPage() {
  const [apis, setAPIs] = useState<NotificationAPI[]>([]);
  const [rules, setRules] = useState<NotificationRule[]>([]);
  const [apiForm, setAPIForm] = useState<APIForm | null>(null);
  const [ruleForm, setRuleForm] = useState<RuleForm | null>(null);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);

  const load = async () => {
    try {
      const [apiData, ruleData] = await Promise.all([
        api<{ items: NotificationAPI[] }>("/api/v1/admin/notification-apis"),
        api<{ items: NotificationRule[] }>("/api/v1/admin/notification-rules"),
      ]);
      setAPIs(apiData.items); setRules(ruleData.items);
    } catch (e) {
      setError(e instanceof Error ? e.message : "문자 연동 설정을 불러오지 못했습니다.");
    }
  };
  useEffect(() => { void load(); }, []);

  const availableAPIs = useMemo(() => apis.filter((item) => item.channel === ruleForm?.channel), [apis, ruleForm?.channel]);
  const editAPI = (item: NotificationAPI) => {
    setError("");
    setAPIForm({
      ...item, headersJSON: JSON.stringify(item.headers ?? {}, null, 2), parametersJSON: JSON.stringify(item.parameters ?? {}, null, 2),
      secretKeysText: (item.secretKeys ?? []).join(", "),
    });
  };
  const saveAPI = async () => {
    if (!apiForm) return;
    setSaving(true); setError("");
    try {
      const payload = {
        name: apiForm.name, channel: apiForm.channel, baseUrl: apiForm.baseUrl, path: apiForm.path,
        method: apiForm.method, requestFormat: apiForm.requestFormat,
        headers: mapJSON(apiForm.headersJSON, "Headers"), parameters: mapJSON(apiForm.parametersJSON, "Parameters"),
        secretKeys: apiForm.secretKeysText.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean),
        timeoutSeconds: Number(apiForm.timeoutSeconds), enabled: apiForm.enabled,
      };
      if (apiForm.id) await putJSON(`/api/v1/admin/notification-apis/${apiForm.id}`, payload);
      else await postJSON("/api/v1/admin/notification-apis", payload);
      setAPIForm(null); setMessage("문자 API 설정을 저장했습니다."); await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "문자 API 설정을 저장하지 못했습니다.");
    } finally { setSaving(false); }
  };
  const saveRule = async () => {
    if (!ruleForm) return;
    setSaving(true); setError("");
    try {
      const payload = { ...ruleForm, offsetMinutes: Number(ruleForm.offsetMinutes), id: undefined, apiConfigName: undefined };
      if (ruleForm.id) await putJSON(`/api/v1/admin/notification-rules/${ruleForm.id}`, payload);
      else await postJSON("/api/v1/admin/notification-rules", payload);
      setRuleForm(null); setMessage("발송 규칙을 저장했습니다."); await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "발송 규칙을 저장하지 못했습니다.");
    } finally { setSaving(false); }
  };
  const remove = async (kind: "api" | "rule", id: string) => {
    if (!window.confirm("이 항목을 삭제할까요?")) return;
    try {
      await api(`/api/v1/admin/notification-${kind === "api" ? "apis" : "rules"}/${id}`, { method: "DELETE" });
      setMessage("삭제했습니다."); await load();
    } catch (e) { setError(e instanceof Error ? e.message : "삭제하지 못했습니다."); }
  };

  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1450, mx: "auto" }}>
    <PageHeader eyebrow="MESSAGE INTEGRATION" title="문자 API · 발송 규칙" description="SMS, MMS, 카카오톡 API의 URL·path·parameter와 방문 흐름별 발송 시점을 관리합니다." />
    {message && <Alert severity="success" onClose={() => setMessage("")} sx={{ mb: 2 }}>{message}</Alert>}
    {error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}
    <Alert severity="info" sx={{ mb: 3 }}>
      <strong>외부 시스템 연동</strong> 채널과 <strong>외부 시스템</strong> 수신 대상을 조합하면 같은 화면에서 출입 게이트나 게스트 Wi-Fi 발급 API를 호출할 수 있습니다. 방문자 언어를 지정하면 언어별 템플릿을 나란히 운영할 수 있습니다.<br />
      API를 선택하지 않은 규칙은 시스템 설정의 기존 log/webhook Adapter를 사용합니다. 활성 API에는 중복 방지용 <code>{"{{idempotencyKey}}"}</code> 또는 <code>{"{{notificationId}}"}</code>가 필요합니다. MMS Gateway가 QR을 가져가려면 Parameter에 <code>{"{{qrcodeUrl}}"}</code>을 연결하고 외부 기준 URL을 설정하세요.
    </Alert>

    <Card>
      <CardContent>
        <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "center" }} spacing={1} mb={2}>
          <Box><Typography variant="h6">호출 API</Typography><Typography variant="body2" color="text.secondary">채널마다 여러 API를 등록할 수 있으며 URL과 path를 별도로 조합합니다.</Typography></Box>
          <Button variant="contained" startIcon={<AddRounded />} onClick={() => { setError(""); setAPIForm(emptyAPI()); }}>API 추가</Button>
        </Stack>
        <TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>이름</TableCell><TableCell>채널</TableCell><TableCell>호출 주소</TableCell><TableCell>방식</TableCell><TableCell>상태</TableCell><TableCell align="right">관리</TableCell></TableRow></TableHead><TableBody>
          {apis.map((item) => <TableRow key={item.id}><TableCell><Typography fontWeight={750} variant="body2">{item.name}</Typography></TableCell><TableCell><Chip size="small" label={channelLabels[item.channel]} /></TableCell><TableCell sx={{ fontFamily: "monospace", wordBreak: "break-all" }}>{item.baseUrl}{item.path}</TableCell><TableCell>{item.method} · {item.requestFormat}</TableCell><TableCell><Chip size="small" color={item.enabled ? "success" : "default"} label={item.enabled ? "사용" : "중지"} /></TableCell><TableCell align="right"><Button size="small" startIcon={<EditOutlined />} onClick={() => editAPI(item)}>수정</Button><Button size="small" color="error" startIcon={<DeleteOutlineRounded />} onClick={() => void remove("api", item.id)}>삭제</Button></TableCell></TableRow>)}
          {apis.length === 0 && <TableRow><TableCell colSpan={6} align="center" sx={{ py: 4, color: "text.secondary" }}>등록된 API가 없습니다. 기존 log/webhook Adapter만 사용됩니다.</TableCell></TableRow>}
        </TableBody></Table></TableContainer>
      </CardContent>
    </Card>

    <Card sx={{ mt: 3 }}>
      <CardContent>
        <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "center" }} spacing={1} mb={2}>
          <Box><Typography variant="h6">발송 시점 · 규칙</Typography><Typography variant="body2" color="text.secondary">각 시점에서 수신 대상, 채널, 호출 API와 메시지 템플릿을 선택합니다.</Typography></Box>
          <Button variant="contained" startIcon={<AddRounded />} onClick={() => { setError(""); setRuleForm(emptyRule()); }}>규칙 추가</Button>
        </Stack>
        <TableContainer component={Paper} variant="outlined"><Table size="small"><TableHead><TableRow><TableCell>규칙</TableCell><TableCell>시점</TableCell><TableCell>수신</TableCell><TableCell>언어</TableCell><TableCell>채널 · API</TableCell><TableCell>템플릿</TableCell><TableCell>상태</TableCell><TableCell align="right">관리</TableCell></TableRow></TableHead><TableBody>
          {rules.map((item) => <TableRow key={item.id}><TableCell><Typography fontWeight={750} variant="body2">{item.name}</Typography></TableCell><TableCell>{eventLabels[item.event] ?? item.event}{item.offsetMinutes ? ` ${item.offsetMinutes > 0 ? "+" : ""}${item.offsetMinutes}분` : ""}</TableCell><TableCell>{audienceLabels[item.audience] ?? item.audience}</TableCell><TableCell>{localeLabels[item.locale ?? ""] ?? item.locale}</TableCell><TableCell>{channelLabels[item.channel]} · {item.apiConfigName || "기존 Adapter"}</TableCell><TableCell sx={{ fontFamily: "monospace" }}>{item.templateKey}</TableCell><TableCell><Chip size="small" color={item.enabled ? "success" : "default"} label={item.enabled ? "사용" : "중지"} /></TableCell><TableCell align="right"><Button size="small" startIcon={<EditOutlined />} onClick={() => { setError(""); setRuleForm({ ...item }); }}>수정</Button><Button size="small" color="error" startIcon={<DeleteOutlineRounded />} onClick={() => void remove("rule", item.id)}>삭제</Button></TableCell></TableRow>)}
        </TableBody></Table></TableContainer>
      </CardContent>
    </Card>

    <Dialog open={Boolean(apiForm)} onClose={() => setAPIForm(null)} fullWidth maxWidth="md">
      <DialogTitle>{apiForm?.id ? "문자 API 수정" : "문자 API 추가"}</DialogTitle><DialogContent dividers>{apiForm && <Stack spacing={2} mt={1}>
        {error && <Alert severity="error" onClose={() => setError("")}>{error}</Alert>}
        {apiForm.enabled === false && <Alert severity="warning">API를 중지하면 연결된 발송 규칙도 중지되고 아직 발송되지 않은 대기 건은 취소됩니다.</Alert>}
        <Grid container spacing={2}><Grid size={{ xs: 12, sm: 8 }}><TextField fullWidth required label="API 이름" value={apiForm.name} onChange={(e) => setAPIForm({ ...apiForm, name: e.target.value })} /></Grid><Grid size={{ xs: 12, sm: 4 }}><TextField fullWidth select label="채널" value={apiForm.channel} onChange={(e) => setAPIForm({ ...apiForm, channel: e.target.value as Channel })}>{Object.entries(channelLabels).map(([value, label]) => <MenuItem key={value} value={value}>{label}</MenuItem>)}</TextField></Grid>
          <Grid size={{ xs: 12, sm: 8 }}><TextField fullWidth required label="기본 URL" value={apiForm.baseUrl} onChange={(e) => setAPIForm({ ...apiForm, baseUrl: e.target.value })} placeholder="https://message.company.intra/api" /></Grid><Grid size={{ xs: 12, sm: 4 }}><TextField fullWidth label="Path" value={apiForm.path} onChange={(e) => setAPIForm({ ...apiForm, path: e.target.value })} placeholder="/v1/send" /></Grid>
          <Grid size={{ xs: 6, sm: 3 }}><TextField fullWidth select label="Method" value={apiForm.method} onChange={(e) => setAPIForm({ ...apiForm, method: e.target.value })}>{["GET", "POST", "PUT", "PATCH"].map((value) => <MenuItem key={value} value={value}>{value}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 6, sm: 3 }}><TextField fullWidth select label="요청 형식" value={apiForm.requestFormat} onChange={(e) => setAPIForm({ ...apiForm, requestFormat: e.target.value })}>{["json", "form", "query"].map((value) => <MenuItem key={value} value={value}>{value}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 6, sm: 3 }}><TextField fullWidth type="number" label="Timeout (초)" value={apiForm.timeoutSeconds} onChange={(e) => setAPIForm({ ...apiForm, timeoutSeconds: Number(e.target.value) })} /></Grid><Grid size={{ xs: 6, sm: 3 }}><FormControlLabel control={<Switch checked={apiForm.enabled} onChange={(e) => setAPIForm({ ...apiForm, enabled: e.target.checked })} />} label="사용" /></Grid>
        </Grid>
        <Alert severity="info">값에서 사용할 수 있는 변수: <Box component="span" sx={{ fontFamily: "monospace", wordBreak: "break-all" }}>{placeholders}</Box></Alert>
        <TextField multiline minRows={5} label="Headers JSON" value={apiForm.headersJSON} onChange={(e) => setAPIForm({ ...apiForm, headersJSON: e.target.value })} helperText='예: { "Authorization": "Bearer token" }' slotProps={{ htmlInput: { style: { fontFamily: "monospace" } } }} />
        <TextField multiline minRows={7} label="Parameters JSON" value={apiForm.parametersJSON} onChange={(e) => setAPIForm({ ...apiForm, parametersJSON: e.target.value })} helperText='활성 API는 중복 방지 변수가 필수입니다. 예: { "receiver": "{{recipient}}", "msg": "{{message}}", "requestId": "{{idempotencyKey}}", "imageUrl": "{{qrcodeUrl}}" }' slotProps={{ htmlInput: { style: { fontFamily: "monospace" } } }} />
        <TextField label="Secret Keys" value={apiForm.secretKeysText} onChange={(e) => setAPIForm({ ...apiForm, secretKeysText: e.target.value })} helperText="쉼표 구분. 예: headers.Authorization, parameters.apiKey — 저장 후 값은 ********로 표시됩니다." />
      </Stack>}</DialogContent><DialogActions><Button onClick={() => setAPIForm(null)}>취소</Button><Button variant="contained" disabled={saving || !apiForm?.name || !apiForm?.baseUrl} onClick={() => void saveAPI()}>{saving ? "저장 중…" : "저장"}</Button></DialogActions>
    </Dialog>

    <Dialog open={Boolean(ruleForm)} onClose={() => setRuleForm(null)} fullWidth maxWidth="md">
      <DialogTitle>{ruleForm?.id ? "발송 규칙 수정" : "발송 규칙 추가"}</DialogTitle><DialogContent dividers>{ruleForm && <Stack spacing={2} mt={1}>
        {error && <Alert severity="error" onClose={() => setError("")}>{error}</Alert>}
        <TextField required label="규칙 이름" value={ruleForm.name} onChange={(e) => setRuleForm({ ...ruleForm, name: e.target.value })} />
        <Grid container spacing={2}><Grid size={{ xs: 12, sm: 4 }}><TextField fullWidth select label="발송 시점" value={ruleForm.event} onChange={(e) => setRuleForm({ ...ruleForm, event: e.target.value, offsetMinutes: e.target.value === "visit_start" ? ruleForm.offsetMinutes : Math.max(0, ruleForm.offsetMinutes) })}>{Object.entries(eventLabels).map(([value, label]) => <MenuItem key={value} value={value}>{label}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 6, sm: 4 }}><TextField fullWidth select label="수신 대상" value={ruleForm.audience} onChange={(e) => setRuleForm({ ...ruleForm, audience: e.target.value })}>{Object.entries(audienceLabels).map(([value, label]) => <MenuItem key={value} value={value}>{label}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 6, sm: 4 }}><TextField fullWidth type="number" label="Offset (분)" value={ruleForm.offsetMinutes} onChange={(e) => setRuleForm({ ...ruleForm, offsetMinutes: Number(e.target.value) })} helperText="방문 시작 전은 음수" /></Grid>
          <Grid size={{ xs: 12, sm: 4 }}><TextField fullWidth select label="채널" value={ruleForm.channel} onChange={(e) => setRuleForm({ ...ruleForm, channel: e.target.value as Channel, apiConfigId: "" })}>{Object.entries(channelLabels).map(([value, label]) => <MenuItem key={value} value={value}>{label}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 12, sm: 4 }}><TextField fullWidth select label="방문자 언어" value={ruleForm.locale ?? ""} onChange={(e) => setRuleForm({ ...ruleForm, locale: e.target.value })} helperText="선택한 언어의 방문자에게만 발송">{Object.entries(localeLabels).map(([value, label]) => <MenuItem key={value || "all"} value={value}>{label}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 12, sm: 4 }}><TextField fullWidth select label="호출 API" value={ruleForm.apiConfigId ?? ""} onChange={(e) => setRuleForm({ ...ruleForm, apiConfigId: e.target.value })}><MenuItem value="">기존 log/webhook Adapter</MenuItem>{availableAPIs.map((item) => <MenuItem key={item.id} value={item.id}>{item.name}{item.enabled ? "" : " (중지)"}</MenuItem>)}</TextField></Grid>
        </Grid>
        {ruleForm.audience === "system" && <Alert severity="info">외부 시스템 연동 규칙입니다. 채널을 <strong>외부 시스템 연동</strong>으로 두고 게이트·게스트 Wi-Fi 등 호출할 API를 반드시 선택하세요. 수신자 값에는 방문자 참가 ID가 전달됩니다.</Alert>}
        <TextField required label="Template Key" value={ruleForm.templateKey} onChange={(e) => setRuleForm({ ...ruleForm, templateKey: e.target.value })} helperText="알림 이력에서 식별할 영문 키" />
        <TextField required multiline minRows={5} label="메시지 본문 템플릿" value={ruleForm.bodyTemplate} onChange={(e) => setRuleForm({ ...ruleForm, bodyTemplate: e.target.value })} helperText={rulePlaceholders} />
        <FormControlLabel control={<Switch checked={ruleForm.enabled} onChange={(e) => setRuleForm({ ...ruleForm, enabled: e.target.checked })} />} label="규칙 사용" />
      </Stack>}</DialogContent><DialogActions><Button onClick={() => setRuleForm(null)}>취소</Button><Button variant="contained" disabled={saving || !ruleForm?.name || !ruleForm?.bodyTemplate || !ruleForm?.templateKey} onClick={() => void saveRule()}>{saving ? "저장 중…" : "저장"}</Button></DialogActions>
    </Dialog>
  </Box>;
}
