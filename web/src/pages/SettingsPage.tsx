import { useEffect, useMemo, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Chip, Divider, FormControlLabel, Grid, Stack, Switch, Tab, Tabs, TextField, Typography } from "@mui/material";
import SaveRounded from "@mui/icons-material/SaveRounded";
import LinkRounded from "@mui/icons-material/LinkRounded";
import CheckCircleRounded from "@mui/icons-material/CheckCircleRounded";
import WarningAmberRounded from "@mui/icons-material/WarningAmberRounded";
import { api, postJSON, putJSON } from "../api";
import { PageHeader } from "../components/AdminUI";

type Setting = { key: string; value: string; secret: boolean; configured: boolean };
type Field = { key: string; label: string; help?: string; secret?: boolean; type?: string; multiline?: boolean };
const fields: Record<string, Field[]> = {
  general: [
    { key: "general.service_name", label: "서비스 이름" }, { key: "general.company_name", label: "회사/조직명" }, { key: "general.base_url", label: "외부 기준 URL", help: "SMS 모바일 방문증 링크 기준. 예: https://visit.company.intra" }, { key: "general.default_locale", label: "기본 언어" },
  ],
  oidc: [
    { key: "oidc.issuer_url", label: "Keycloak Issuer URL", help: "예: https://keycloak.intra/realms/company" }, { key: "oidc.client_id", label: "Client ID" }, { key: "oidc.client_secret", label: "Client Secret", secret: true }, { key: "oidc.scopes", label: "Scopes" }, { key: "oidc.admin_group", label: "관리자 그룹" }, { key: "oidc.lobby_group", label: "로비 그룹" }, { key: "oidc.security_group", label: "보안 그룹" }, { key: "oidc.auditor_group", label: "감사 그룹" }, { key: "oidc.department_manager_group", label: "부서 관리자 그룹" },
  ],
  visit: [
    { key: "visit.early_checkin_minutes", label: "조기 체크인 허용 (분)", type: "number" }, { key: "visit.late_grace_minutes", label: "미방문 판정 유예 (분)", type: "number" }, { key: "visit.dynamic_qr_seconds", label: "Dynamic QR 주기 (초)", type: "number", help: "0은 고정 서버 검증 QR, 권장 Dynamic 값은 30~60" }, { key: "visit.auto_checkout_hour", label: "자동 퇴실 처리 시각", type: "number" },
  ],
  notification: [
    { key: "notification.provider", label: "Provider", help: "log 또는 webhook" }, { key: "notification.webhook_url", label: "사내 SMS Gateway Webhook URL" }, { key: "notification.auth_header", label: "Authorization Header", secret: true }, { key: "notification.visitor_template", label: "방문증 발송 템플릿", multiline: true, help: "{{company}} {{visitor}} {{start}} {{place}} {{passUrl}}" }, { key: "notification.arrival_template", label: "담당자 도착 알림 템플릿", multiline: true, help: "{{visitor}} {{lobby}} {{checkedIn}}" },
  ],
  privacy: [
    { key: "privacy.mask_after_days", label: "마스킹 전환 (일)", type: "number" }, { key: "privacy.destroy_after_days", label: "개인 식별정보 자동 파기 (일)", type: "number" }, { key: "privacy.audit_retention_days", label: "감사 로그 보존 (일)", type: "number" },
  ],
  security: [
    { key: "security.session_hours", label: "세션 유효시간 (시간)", type: "number" }, { key: "security.api_key_days", label: "개인 키 기본 유효기간 (일)", type: "number" }, { key: "security.rotation_grace_hours", label: "키 회전 유예시간 (시간)", type: "number" }, { key: "security.api_key_allowed_scopes", label: "허용 개인 키 Scope", help: "read write mcp 중 허용할 범위를 공백으로 구분합니다. 제거한 Scope는 기존 키에도 즉시 차단됩니다." }, { key: "security.api_key_max_active", label: "사용자별 활성 키 한도", type: "number" },
  ],
};

export function SettingsPage() {
  const [items, setItems] = useState<Setting[]>([]); const [values, setValues] = useState<Record<string, string>>({}); const [tab, setTab] = useState("general"); const [message, setMessage] = useState(""); const [error, setError] = useState(""); const [saving, setSaving] = useState(false); const [currentPassword, setCurrentPassword] = useState(""); const [newPassword, setNewPassword] = useState("");
  const load = async () => { try { const data = await api<{ items: Setting[] }>("/api/v1/settings"); setItems(data.items); setValues(Object.fromEntries(data.items.map((x) => [x.key, x.value]))); } catch (e) { setError(e instanceof Error ? e.message : "설정을 불러오지 못했습니다"); } }; useEffect(() => { void load(); }, []);
  const configured = useMemo(() => Object.fromEntries(items.map((x) => [x.key, x.configured])), [items]); const dirty = useMemo(() => items.some((item) => (values[item.key] ?? "") !== item.value), [items, values]);
  useEffect(() => { const warn = (event: BeforeUnloadEvent) => { if (dirty) event.preventDefault(); }; window.addEventListener("beforeunload", warn); return () => window.removeEventListener("beforeunload", warn); }, [dirty]);
  const save = async () => { setSaving(true); setError(""); try { await putJSON("/api/v1/settings", { settings: values }); setMessage("설정을 저장했습니다. 새 요청부터 즉시 적용됩니다."); await load(); return true; } catch (e) { setError(e instanceof Error ? e.message : "저장하지 못했습니다"); return false; } finally { setSaving(false); } };
  const test = async () => { try { if (!(await save())) return; const result = await api<{ redirectUri: string }>("/api/v1/settings/oidc/test", { method: "POST" }); setMessage(`Keycloak Discovery 연결 성공 · Callback ${result.redirectUri}`); } catch (e) { setError(e instanceof Error ? e.message : "연결하지 못했습니다"); } };
  const changePassword = async () => { try { await postJSON("/api/v1/auth/password", { currentPassword, newPassword }); setCurrentPassword(""); setNewPassword(""); setMessage("관리자 비밀번호를 변경하고 다른 세션을 종료했습니다."); } catch (e) { setError(e instanceof Error ? e.message : "비밀번호를 변경하지 못했습니다"); } };
  const toggle = (key: string, label: string) => <FormControlLabel control={<Switch checked={values[key] === "true"} onChange={(e) => setValues((v) => ({ ...v, [key]: String(e.target.checked) }))} />} label={label} />;
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1150, mx: "auto" }}><PageHeader eyebrow="SYSTEM CONTROL" title="시스템 설정" description="배포 환경변수는 PostgreSQL DSN, Bootstrap 계정 2개와 Encryption Key뿐이며, 나머지 운영 설정은 여기에서 관리합니다." actions={dirty ? <Chip icon={<WarningAmberRounded />} color="warning" label="저장하지 않은 변경" /> : <Chip icon={<CheckCircleRounded />} color="success" variant="outlined" label="모든 변경 저장됨" />} />{message && <Alert severity="success" onClose={() => setMessage("")} sx={{ mb: 2 }}>{message}</Alert>}{error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}
    <Card><Stack direction={{ xs: "column", sm: "row" }} spacing={1} sx={{ px: 2.5, pt: 2 }}><Chip size="small" color={values["oidc.enabled"] === "true" ? "success" : "default"} variant="outlined" label={`Keycloak ${values["oidc.enabled"] === "true" ? "활성" : "비활성"}`} /><Chip size="small" color={values["visit.approval_enabled"] === "true" ? "warning" : "success"} variant="outlined" label={`승인 ${values["visit.approval_enabled"] === "true" ? "Workflow" : "자동 확정"}`} /><Chip size="small" variant="outlined" label={`알림 ${values["notification.provider"] || "log"}`} /></Stack><Tabs value={tab} onChange={(_, value) => setTab(value)} variant="scrollable" scrollButtons="auto" sx={{ px: 2, borderBottom: "1px solid #E1E9E6" }}>{[["general", "일반"], ["oidc", "Keycloak SSO"], ["visit", "방문 · QR 정책"], ["notification", "SMS · 알림"], ["privacy", "개인정보"], ["security", "보안 · 키"]].map(([value, label]) => <Tab key={value} value={value} label={label} />)}</Tabs><CardContent sx={{ p: { xs: 2, md: 4 } }}>
      {tab === "oidc" && <><Stack direction={{ xs: "column", sm: "row" }} spacing={2} mb={3}>{toggle("oidc.enabled", "Keycloak SSO 사용")}{toggle("auth.local_enabled", "로컬 관리자 로그인 허용")}{toggle("oidc.auto_provision", "SSO 사용자 자동 생성")}</Stack><Alert severity="info" sx={{ mb: 3 }}>Keycloak Client의 Valid Redirect URI에 <strong>{window.location.origin}/api/v1/auth/oidc/callback</strong>을 등록하세요. Issuer, Client ID, Client Secret만으로 Discovery와 PKCE 연결이 자동 구성됩니다.</Alert></>}
      {tab === "visit" && <Stack direction={{ xs: "column", sm: "row" }} spacing={2} mb={3}>{toggle("visit.approval_enabled", "방문 승인 Workflow")}{toggle("visit.single_use_qr", "QR 1회 체크인")}{toggle("visit.company_required", "회사명 필수")}</Stack>}
      {tab === "privacy" && <Alert severity="warning" sx={{ mb: 3 }}>방문 정보는 목록에서 항상 최소 노출되며, 파기일이 지나면 이름·전화·이메일·차량번호를 복구 불가능하게 대체합니다. 감사 통계는 식별정보 없이 유지됩니다.</Alert>}
      {tab === "notification" && <Alert severity="info" sx={{ mb: 3 }}><strong>log</strong>는 오프라인 검증용으로 실제 발송 없이 성공 기록합니다. <strong>webhook</strong>은 사내 SMS Gateway에 recipient, message, idempotencyKey JSON을 전송합니다.</Alert>}
      <Grid container spacing={2.5}>{fields[tab].map((f) => <Grid key={f.key} size={{ xs: 12, md: f.multiline ? 12 : 6 }}><TextField fullWidth multiline={f.multiline} minRows={f.multiline ? 3 : undefined} label={f.label} type={f.secret ? "password" : f.type || "text"} value={values[f.key] ?? ""} onChange={(e) => setValues((v) => ({ ...v, [f.key]: e.target.value }))} helperText={f.help || (f.secret && configured[f.key] ? "AES-256-GCM으로 암호화되어 있습니다. 변경할 때만 새 값을 입력하세요." : " ")} /></Grid>)}</Grid>
      {tab === "security" && <Box sx={{ mt: 3 }}><Alert severity="warning" sx={{ mb: 3 }}><strong>ENCRYPTION_KEY</strong>는 설정 화면에서 변경하지 않습니다. PostgreSQL 백업과 함께 안전하게 보관하고, 모든 노드에 동일한 32바이트 키를 주입하세요. 키가 달라지면 암호화된 개인정보와 Client Secret을 복호화할 수 없습니다.</Alert><Divider sx={{ mb: 3 }} /><Typography variant="h6">내 로컬 관리자 비밀번호</Typography><Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>SSO 사용자는 Keycloak에서 변경합니다.</Typography><Stack direction={{ xs: "column", sm: "row" }} spacing={2}><TextField type="password" label="현재 비밀번호" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} /><TextField type="password" label="새 비밀번호 (12자 이상)" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} /><Button variant="outlined" disabled={!currentPassword || newPassword.length < 12} onClick={() => void changePassword()}>비밀번호 변경</Button></Stack></Box>}
      <Divider sx={{ my: 3 }} /><Stack direction={{ xs: "column-reverse", sm: "row" }} justifyContent="flex-end" spacing={1}>{tab === "oidc" && <Button startIcon={<LinkRounded />} onClick={() => void test()}>저장 후 연결 테스트</Button>}<Button variant="contained" startIcon={<SaveRounded />} disabled={!dirty || saving} onClick={() => void save()}>{saving ? "저장 중…" : "설정 저장"}</Button></Stack>
    </CardContent></Card><Stack direction="row" spacing={1} mt={2} flexWrap="wrap"><Chip label="비밀값 AES-256-GCM" variant="outlined" /><Chip label="변경 Before/After 감사 대상" variant="outlined" /><Chip label="런타임 CDN 0개" variant="outlined" /></Stack>
  </Box>;
}
