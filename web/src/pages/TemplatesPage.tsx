import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  Card,
  CardContent,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControlLabel,
  Grid,
  IconButton,
  Paper,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import EditOutlined from "@mui/icons-material/EditOutlined";
import PeopleAltOutlined from "@mui/icons-material/PeopleAltOutlined";
import PersonAddAltRounded from "@mui/icons-material/PersonAddAltRounded";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import { useNavigate } from "react-router-dom";
import { api, postJSON, putJSON } from "../api";
import type { FrequentVisitor, VisitTemplate } from "../types";
import { PageHeader } from "../components/AdminUI";

type TemplateForm = {
  name: string;
  purpose: string;
  placeDetail: string;
  company: string;
  frequentVisitorIds: string[];
};

type FrequentVisitorForm = {
  name: string;
  phone: string;
  email: string;
  company: string;
  title: string;
  vehicle: string;
  equipment: string;
  consent: boolean;
};

const blankTemplateForm = (): TemplateForm => ({
  name: "",
  purpose: "",
  placeDetail: "",
  company: "",
  frequentVisitorIds: [],
});

const blankFrequentVisitorForm = (): FrequentVisitorForm => ({
  name: "",
  phone: "",
  email: "",
  company: "",
  title: "",
  vehicle: "",
  equipment: "",
  consent: true,
});

const messageOf = (error: unknown, fallback: string) =>
  error instanceof Error ? error.message : fallback;

const equipmentValues = (value: string) =>
  value.split(",").map((item) => item.trim()).filter(Boolean);

export function TemplatesPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<VisitTemplate[]>([]);
  const [frequentVisitors, setFrequentVisitors] = useState<FrequentVisitor[]>([]);
  const [templateOpen, setTemplateOpen] = useState(false);
  const [editingTemplateID, setEditingTemplateID] = useState<string | null>(null);
  const [templateForm, setTemplateForm] = useState<TemplateForm>(blankTemplateForm);
  const [frequentOpen, setFrequentOpen] = useState(false);
  const [editingFrequentVisitorID, setEditingFrequentVisitorID] = useState<string | null>(null);
  const [frequentForm, setFrequentForm] = useState<FrequentVisitorForm>(blankFrequentVisitorForm);
  const [selectFrequentAfterSave, setSelectFrequentAfterSave] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const [templates, visitors] = await Promise.all([
        api<{ items: VisitTemplate[] }>("/api/v1/visit-templates"),
        api<{ items: FrequentVisitor[] }>("/api/v1/frequent-visitors"),
      ]);
      setItems(templates.items);
      setFrequentVisitors(visitors.items);
      setError("");
    } catch (loadError) {
      setError(messageOf(loadError, "템플릿 정보를 불러오지 못했습니다"));
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const frequentByID = useMemo(
    () => new Map(frequentVisitors.map((visitor) => [visitor.id, visitor])),
    [frequentVisitors],
  );

  const openCreateTemplate = () => {
	setError("");
    setEditingTemplateID(null);
    setTemplateForm(blankTemplateForm());
    setTemplateOpen(true);
  };

  const openEditTemplate = (item: VisitTemplate) => {
	setError("");
    setEditingTemplateID(item.id);
    setTemplateForm({
      name: item.name,
      purpose: item.payload.purpose ?? "",
      placeDetail: item.payload.placeDetail ?? "",
      company: item.payload.company ?? "",
      frequentVisitorIds: item.frequentVisitorIds ?? [],
    });
    setTemplateOpen(true);
  };

  const saveTemplate = async () => {
    setSaving(true);
    setError("");
    const body = {
      name: templateForm.name,
      payload: {
        purpose: templateForm.purpose,
        placeDetail: templateForm.placeDetail,
        company: templateForm.company,
      },
      frequentVisitorIds: templateForm.frequentVisitorIds,
    };
    try {
      if (editingTemplateID) {
        await putJSON(`/api/v1/visit-templates/${editingTemplateID}`, body);
      } else {
        await postJSON("/api/v1/visit-templates", body);
      }
      setTemplateOpen(false);
      await load();
    } catch (saveError) {
      setError(messageOf(saveError, "템플릿을 저장하지 못했습니다"));
    } finally {
      setSaving(false);
    }
  };

  const removeTemplate = async (id: string) => {
    if (!confirm("템플릿을 삭제할까요?")) return;
    try {
      await api(`/api/v1/visit-templates/${id}`, { method: "DELETE" });
      await load();
    } catch (removeError) {
      setError(messageOf(removeError, "템플릿을 삭제하지 못했습니다"));
    }
  };

  const useTemplate = (id: string) => {
    localStorage.removeItem("visitflow_template");
    sessionStorage.setItem("visitflow_template_id", id);
    navigate("/visits/new");
  };

  const openCreateFrequentVisitor = (selectAfterSave = false) => {
	setError("");
    setEditingFrequentVisitorID(null);
    setFrequentForm(blankFrequentVisitorForm());
    setSelectFrequentAfterSave(selectAfterSave);
    setFrequentOpen(true);
  };

  const openEditFrequentVisitor = (visitor: FrequentVisitor) => {
	setError("");
    setEditingFrequentVisitorID(visitor.id);
    setFrequentForm({
      name: visitor.name,
      phone: visitor.phone,
      email: visitor.email ?? "",
      company: visitor.company ?? "",
      title: visitor.title ?? "",
      vehicle: visitor.vehicle ?? "",
      equipment: visitor.equipment.join(", "),
      consent: true,
    });
    setSelectFrequentAfterSave(false);
    setFrequentOpen(true);
  };

  const saveFrequentVisitor = async () => {
    setSaving(true);
    setError("");
    const body = {
      ...frequentForm,
      equipment: equipmentValues(frequentForm.equipment),
    };
    try {
      let result: { id: string };
      if (editingFrequentVisitorID) {
        result = await putJSON<{ id: string }>(`/api/v1/frequent-visitors/${editingFrequentVisitorID}`, body);
      } else {
        result = await postJSON<{ id: string }>("/api/v1/frequent-visitors", body);
      }
      if (selectFrequentAfterSave && !templateForm.frequentVisitorIds.includes(result.id)) {
        setTemplateForm((current) => ({
          ...current,
          frequentVisitorIds: [...current.frequentVisitorIds, result.id],
        }));
      }
      setFrequentOpen(false);
      await load();
    } catch (saveError) {
      setError(messageOf(saveError, "자주 방문자를 저장하지 못했습니다"));
    } finally {
      setSaving(false);
    }
  };

  const removeFrequentVisitor = async (visitor: FrequentVisitor) => {
    const linked = visitor.templateCount > 0
      ? ` 이 방문자는 ${visitor.templateCount}개 템플릿에서도 제거됩니다.`
      : "";
    if (!confirm(`${visitor.name} 님을 자주 방문자에서 삭제할까요?${linked}`)) return;
    try {
      await api(`/api/v1/frequent-visitors/${visitor.id}`, { method: "DELETE" });
      setTemplateForm((current) => ({
        ...current,
        frequentVisitorIds: current.frequentVisitorIds.filter((id) => id !== visitor.id),
      }));
      await load();
    } catch (removeError) {
      setError(messageOf(removeError, "자주 방문자를 삭제하지 못했습니다"));
    }
  };

  const selectedFrequentVisitors = templateForm.frequentVisitorIds
    .map((id) => frequentByID.get(id))
    .filter((visitor): visitor is FrequentVisitor => Boolean(visitor));

  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1100, mx: "auto" }}>
      <PageHeader
        eyebrow="REUSABLE VISITS"
        title="방문 템플릿"
        description="자주 방문하는 사람과 방문 목적을 저장하고 다음 신청에 안전하게 불러옵니다."
        actions={
          <Stack direction={{ xs: "column", sm: "row" }} spacing={1}>
            <Button variant="outlined" startIcon={<PersonAddAltRounded />} onClick={() => openCreateFrequentVisitor(false)}>자주 방문자 등록</Button>
            <Button variant="contained" startIcon={<AddRounded />} onClick={openCreateTemplate}>템플릿 만들기</Button>
          </Stack>
        }
      />
      {error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}

      <Grid container spacing={2}>
        {items.map((item) => {
          const selected = (item.frequentVisitorIds ?? []).map((id) => frequentByID.get(id)).filter(Boolean) as FrequentVisitor[];
          const companies = [...new Set(selected.map((visitor) => visitor.company).filter(Boolean))];
          return (
            <Grid key={item.id} size={{ xs: 12, md: 6 }}>
              <Card sx={{ height: "100%" }}>
                <CardContent>
                  <Stack direction="row" justifyContent="space-between" alignItems="flex-start" spacing={1}>
                    <Box>
                      <Typography variant="h6">{item.name}</Typography>
                      <Typography color="text.secondary">
                        {companies.join(", ") || item.payload.company || "회사 미지정"}
                      </Typography>
                    </Box>
                    <Stack direction="row">
                      <Tooltip title="템플릿 수정"><IconButton aria-label={`${item.name} 수정`} onClick={() => openEditTemplate(item)}><EditOutlined /></IconButton></Tooltip>
                      <Tooltip title="템플릿 삭제"><IconButton aria-label={`${item.name} 삭제`} color="error" onClick={() => void removeTemplate(item.id)}><DeleteOutlineRounded /></IconButton></Tooltip>
                    </Stack>
                  </Stack>
                  <Paper variant="outlined" sx={{ p: 2, my: 2, bgcolor: "#F7FAF8" }}>
                    <Typography variant="body2" fontWeight={700}>{item.payload.purpose || "방문 목적 미지정"}</Typography>
                    <Typography variant="caption" color="text.secondary">{item.payload.placeDetail || "세부 장소 미지정"}</Typography>
                  </Paper>
                  <Stack direction="row" spacing={1} alignItems="center" mb={2} flexWrap="wrap" useFlexGap>
                    <PeopleAltOutlined fontSize="small" color="action" />
                    {selected.length > 0
                      ? selected.map((visitor) => <Chip key={visitor.id} size="small" label={visitor.name} />)
                      : <Typography variant="body2" color="text.secondary">등록된 자주 방문자 없음</Typography>}
                  </Stack>
                  <Button fullWidth variant="outlined" startIcon={<PlayArrowRounded />} onClick={() => useTemplate(item.id)}>이 템플릿으로 신청</Button>
                </CardContent>
              </Card>
            </Grid>
          );
        })}
        {items.length === 0 && (
          <Grid size={{ xs: 12 }}>
            <Paper variant="outlined" sx={{ p: 7, textAlign: "center" }}>
              <Typography color="text.secondary">아직 저장한 템플릿이 없습니다.</Typography>
            </Paper>
          </Grid>
        )}
      </Grid>

      <Divider sx={{ my: 4 }} />
      <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "center" }} spacing={1} mb={2}>
        <Box>
          <Typography variant="h5" fontWeight={800}>자주 방문자</Typography>
          <Typography variant="body2" color="text.secondary">연락처는 암호화해 보관하며 내 템플릿에서만 선택할 수 있습니다.</Typography>
        </Box>
        <Button startIcon={<PersonAddAltRounded />} onClick={() => openCreateFrequentVisitor(false)}>방문자 등록</Button>
      </Stack>
      <Grid container spacing={2}>
        {frequentVisitors.map((visitor) => (
          <Grid key={visitor.id} size={{ xs: 12, sm: 6, lg: 4 }}>
            <Paper variant="outlined" sx={{ p: 2.5, height: "100%" }}>
              <Stack direction="row" justifyContent="space-between" spacing={1}>
                <Box sx={{ minWidth: 0 }}>
                  <Typography fontWeight={800}>{visitor.name}</Typography>
                  <Typography variant="body2" color="text.secondary">{visitor.company || "회사 미지정"}{visitor.title ? ` · ${visitor.title}` : ""}</Typography>
                </Box>
                <Stack direction="row">
                  <Tooltip title="자주 방문자 수정"><IconButton size="small" aria-label={`${visitor.name} 수정`} onClick={() => openEditFrequentVisitor(visitor)}><EditOutlined fontSize="small" /></IconButton></Tooltip>
                  <Tooltip title="자주 방문자 삭제"><IconButton size="small" aria-label={`${visitor.name} 삭제`} color="error" onClick={() => void removeFrequentVisitor(visitor)}><DeleteOutlineRounded fontSize="small" /></IconButton></Tooltip>
                </Stack>
              </Stack>
              <Typography variant="body2" sx={{ mt: 1 }}>{visitor.phone}</Typography>
              {visitor.email && <Typography variant="caption" color="text.secondary" sx={{ wordBreak: "break-all" }}>{visitor.email}</Typography>}
              <Stack direction="row" spacing={0.7} mt={1.5} flexWrap="wrap" useFlexGap>
                <Chip size="small" variant="outlined" label={`템플릿 ${visitor.templateCount}개`} />
                {visitor.equipment.slice(0, 2).map((equipment) => <Chip key={equipment} size="small" variant="outlined" label={equipment} />)}
              </Stack>
            </Paper>
          </Grid>
        ))}
        {frequentVisitors.length === 0 && (
          <Grid size={{ xs: 12 }}><Alert severity="info">자주 방문자를 등록하면 템플릿에 이름과 연락처를 함께 저장하지 않고 안전하게 연결할 수 있습니다.</Alert></Grid>
        )}
      </Grid>

      <Dialog open={templateOpen} onClose={() => !saving && setTemplateOpen(false)} fullWidth maxWidth="md">
        <DialogTitle>{editingTemplateID ? "방문 템플릿 수정" : "방문 템플릿 만들기"}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} mt={1}>
			{error && <Alert severity="error" onClose={() => setError("")}>{error}</Alert>}
            <TextField required label="템플릿 이름" value={templateForm.name} onChange={(event) => setTemplateForm((current) => ({ ...current, name: event.target.value }))} />
            <TextField required label="방문 목적" value={templateForm.purpose} onChange={(event) => setTemplateForm((current) => ({ ...current, purpose: event.target.value }))} />
            <TextField label="세부 장소" value={templateForm.placeDetail} onChange={(event) => setTemplateForm((current) => ({ ...current, placeDetail: event.target.value }))} />
            <TextField label="기본 회사명" value={templateForm.company} onChange={(event) => setTemplateForm((current) => ({ ...current, company: event.target.value }))} helperText="자주 방문자를 선택하지 않은 템플릿의 기본 회사명" />
            <Divider />
            <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "center" }} spacing={1}>
              <Box><Typography fontWeight={800}>자주 방문자 선택</Typography><Typography variant="caption" color="text.secondary">최대 100명 · 선택 순서대로 대표 방문자가 정해집니다.</Typography></Box>
              <Button size="small" startIcon={<PersonAddAltRounded />} onClick={() => openCreateFrequentVisitor(true)}>새 방문자 등록</Button>
            </Stack>
            <Autocomplete
              multiple
              disableCloseOnSelect
              options={frequentVisitors}
              value={selectedFrequentVisitors}
              isOptionEqualToValue={(option, value) => option.id === value.id}
              getOptionLabel={(option) => `${option.name}${option.company ? ` · ${option.company}` : ""} · ${option.phone}`}
              onChange={(_, value) => setTemplateForm((current) => ({ ...current, frequentVisitorIds: value.map((visitor) => visitor.id) }))}
              renderInput={(params) => <TextField {...params} label="템플릿 방문자" placeholder="이름 또는 회사로 선택" />}
            />
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button disabled={saving} onClick={() => setTemplateOpen(false)}>취소</Button>
          <Button variant="contained" disabled={saving || !templateForm.name.trim() || !templateForm.purpose.trim()} onClick={() => void saveTemplate()}>{saving ? "저장 중…" : "저장"}</Button>
        </DialogActions>
      </Dialog>

      <Dialog open={frequentOpen} onClose={() => !saving && setFrequentOpen(false)} fullWidth maxWidth="md">
        <DialogTitle>{editingFrequentVisitorID ? "자주 방문자 수정" : "자주 방문자 등록"}</DialogTitle>
        <DialogContent>
		  {error && <Alert severity="error" onClose={() => setError("")} sx={{ mt: 1 }}>{error}</Alert>}
          <Grid container spacing={2} mt={0.5}>
            <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth required label="이름" value={frequentForm.name} onChange={(event) => setFrequentForm((current) => ({ ...current, name: event.target.value }))} /></Grid>
            <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth required label="휴대전화" value={frequentForm.phone} onChange={(event) => setFrequentForm((current) => ({ ...current, phone: event.target.value }))} placeholder="010-0000-0000" /></Grid>
            <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="회사명" value={frequentForm.company} onChange={(event) => setFrequentForm((current) => ({ ...current, company: event.target.value }))} /></Grid>
            <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="이메일" value={frequentForm.email} onChange={(event) => setFrequentForm((current) => ({ ...current, email: event.target.value }))} /></Grid>
            <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="직책" value={frequentForm.title} onChange={(event) => setFrequentForm((current) => ({ ...current, title: event.target.value }))} /></Grid>
            <Grid size={{ xs: 12, md: 4 }}><TextField fullWidth label="차량번호" value={frequentForm.vehicle} onChange={(event) => setFrequentForm((current) => ({ ...current, vehicle: event.target.value }))} /></Grid>
            <Grid size={{ xs: 12 }}><TextField fullWidth label="기본 반입 장비" value={frequentForm.equipment} onChange={(event) => setFrequentForm((current) => ({ ...current, equipment: event.target.value }))} helperText="노트북, 카메라처럼 쉼표로 구분" /></Grid>
            <Grid size={{ xs: 12 }}><FormControlLabel control={<Checkbox checked={frequentForm.consent} onChange={(event) => setFrequentForm((current) => ({ ...current, consent: event.target.checked }))} />} label="방문자 개인정보 수집·이용 동의를 확인했습니다." /></Grid>
          </Grid>
        </DialogContent>
        <DialogActions>
          <Button disabled={saving} onClick={() => setFrequentOpen(false)}>취소</Button>
          <Button variant="contained" disabled={saving || !frequentForm.name.trim() || !frequentForm.phone.trim() || !frequentForm.consent} onClick={() => void saveFrequentVisitor()}>{saving ? "저장 중…" : "저장"}</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
