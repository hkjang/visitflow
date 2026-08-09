import { useEffect, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Dialog, DialogActions, DialogContent, DialogTitle, Grid, IconButton, Paper, Stack, TextField, Typography } from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import PlayArrowRounded from "@mui/icons-material/PlayArrowRounded";
import { useNavigate } from "react-router-dom";
import { api, postJSON } from "../api";
import { PageHeader } from "../components/AdminUI";

type Template = { id: string; name: string; payload: Record<string, unknown>; updatedAt: string };
export function TemplatesPage() {
  const navigate = useNavigate(); const [items, setItems] = useState<Template[]>([]); const [open, setOpen] = useState(false); const [name, setName] = useState(""); const [purpose, setPurpose] = useState(""); const [placeDetail, setPlaceDetail] = useState(""); const [company, setCompany] = useState(""); const [error, setError] = useState("");
  const load = () => api<{ items: Template[] }>("/api/v1/visit-templates").then((x) => setItems(x.items)).catch((e) => setError(e.message)); useEffect(() => { void load(); }, []);
  const create = async () => { await postJSON("/api/v1/visit-templates", { name, payload: { purpose, placeDetail, company } }); setOpen(false); setName(""); setPurpose(""); setPlaceDetail(""); setCompany(""); load(); };
  const remove = async (id: string) => { if (!confirm("템플릿을 삭제할까요?")) return; await api(`/api/v1/visit-templates/${id}`, { method: "DELETE" }); load(); };
  const use = (item: Template) => { localStorage.setItem("visitflow_template", JSON.stringify(item.payload)); navigate("/visits/new"); };
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1100, mx: "auto" }}><PageHeader eyebrow="REUSABLE VISITS" title="방문 템플릿" description="자주 방문하는 업체와 목적을 저장해 신청 시간을 줄입니다." actions={<Button variant="contained" startIcon={<AddRounded />} onClick={() => setOpen(true)}>템플릿 만들기</Button>} />{error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}<Grid container spacing={2}>{items.map((item) => <Grid key={item.id} size={{ xs: 12, md: 6 }}><Card><CardContent><Stack direction="row" justifyContent="space-between"><Box><Typography variant="h6">{item.name}</Typography><Typography color="text.secondary">{String(item.payload.company || "회사 미지정")}</Typography></Box><IconButton color="error" onClick={() => void remove(item.id)}><DeleteOutlineRounded /></IconButton></Stack><Paper variant="outlined" sx={{ p: 2, my: 2, bgcolor: "#F7FAF8" }}><Typography variant="body2" fontWeight={700}>{String(item.payload.purpose || "방문 목적 미지정")}</Typography><Typography variant="caption" color="text.secondary">{String(item.payload.placeDetail || "세부 장소 미지정")}</Typography></Paper><Button fullWidth variant="outlined" startIcon={<PlayArrowRounded />} onClick={() => use(item)}>이 템플릿으로 신청</Button></CardContent></Card></Grid>)}{items.length === 0 && <Grid size={{ xs: 12 }}><Paper variant="outlined" sx={{ p: 8, textAlign: "center" }}><Typography color="text.secondary">아직 저장한 템플릿이 없습니다.</Typography></Paper></Grid>}</Grid>
    <Dialog open={open} onClose={() => setOpen(false)} fullWidth><DialogTitle>방문 템플릿 만들기</DialogTitle><DialogContent><Stack spacing={2} mt={1}><TextField required label="템플릿 이름" value={name} onChange={(e) => setName(e.target.value)} /><TextField label="회사명" value={company} onChange={(e) => setCompany(e.target.value)} /><TextField required label="방문 목적" value={purpose} onChange={(e) => setPurpose(e.target.value)} /><TextField label="세부 장소" value={placeDetail} onChange={(e) => setPlaceDetail(e.target.value)} /></Stack></DialogContent><DialogActions><Button onClick={() => setOpen(false)}>취소</Button><Button variant="contained" disabled={!name || !purpose} onClick={() => void create()}>저장</Button></DialogActions></Dialog>
  </Box>;
}
