import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  CardActionArea,
  CardContent,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  Grid,
  IconButton,
  Paper,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@mui/material";
import AddRounded from "@mui/icons-material/AddRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import EditOutlined from "@mui/icons-material/EditOutlined";
import PushPinRounded from "@mui/icons-material/PushPinRounded";
import { api, postJSON, putJSON } from "../api";
import { PageHeader } from "../components/AdminUI";
import type { GuidePost } from "../types";

type GuideForm = {
  title: string;
  category: string;
  content: string;
  published: boolean;
  pinned: boolean;
};

const emptyForm: GuideForm = {
  title: "",
  category: "일반",
  content: "",
  published: false,
  pinned: false,
};

export function GuidePage({ admin = false }: { admin?: boolean }) {
  const [items, setItems] = useState<GuidePost[]>([]);
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("");
  const [selected, setSelected] = useState<GuidePost | null>(null);
  const [editingID, setEditingID] = useState<string | null>(null);
  const [form, setForm] = useState<GuideForm>(emptyForm);
  const [editorOpen, setEditorOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const load = async () => {
    try {
      const path = admin ? "/api/v1/admin/guides" : "/api/v1/guides";
      const data = await api<{ items: GuidePost[] }>(path);
      setItems(data.items);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "가이드를 불러오지 못했습니다");
    }
  };

  useEffect(() => {
    void load();
  }, [admin]);

  const categories = useMemo(
    () => Array.from(new Set(items.map((item) => item.category))).sort((a, b) => a.localeCompare(b, "ko")),
    [items],
  );
  const visibleItems = useMemo(() => {
    const query = search.trim().toLocaleLowerCase("ko-KR");
    return items.filter((item) => {
      if (category && item.category !== category) return false;
      if (!query) return true;
      return `${item.title} ${item.content ?? item.excerpt ?? ""}`.toLocaleLowerCase("ko-KR").includes(query);
    });
  }, [category, items, search]);

  const openCreate = () => {
	setError("");
    setEditingID(null);
    setForm(emptyForm);
    setEditorOpen(true);
  };
  const openEdit = (item: GuidePost) => {
	setError("");
    setEditingID(item.id);
    setForm({
      title: item.title,
      category: item.category,
      content: item.content ?? "",
      published: item.published,
      pinned: item.pinned,
    });
    setEditorOpen(true);
  };
  const save = async () => {
    setSaving(true);
    setError("");
    try {
      if (editingID) {
        await putJSON(`/api/v1/admin/guides/${editingID}`, form);
      } else {
        await postJSON("/api/v1/admin/guides", form);
      }
      setEditorOpen(false);
      await load();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "가이드를 저장하지 못했습니다");
    } finally {
      setSaving(false);
    }
  };
  const remove = async (item: GuidePost) => {
    if (!window.confirm(`‘${item.title}’ 글을 삭제할까요?`)) return;
    try {
      await api(`/api/v1/admin/guides/${item.id}`, { method: "DELETE" });
      await load();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "가이드를 삭제하지 못했습니다");
    }
  };
  const openGuide = async (item: GuidePost) => {
    if (admin) {
      setSelected(item);
      return;
    }
    try {
      const data = await api<{ item: GuidePost }>(`/api/v1/guides/${item.id}`);
      setSelected(data.item);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "가이드를 열지 못했습니다");
    }
  };

  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1180, mx: "auto" }}>
      <PageHeader
        eyebrow={admin ? "GUIDE MANAGEMENT" : "HELP CENTER"}
        title={admin ? "사용자 가이드 관리" : "사용자 가이드"}
        description={admin ? "가이드 글을 초안으로 저장하거나 게시하고, 중요 글을 상단에 고정합니다." : "VisitFlow 이용 방법과 사내 방문 절차를 확인하세요."}
        actions={admin ? <Button variant="contained" startIcon={<AddRounded />} onClick={openCreate}>가이드 등록</Button> : undefined}
      />
      {error && <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>{error}</Alert>}
      <Paper variant="outlined" sx={{ p: 2, mb: 2.5 }}>
        <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} alignItems={{ md: "center" }}>
          <TextField
            size="small"
            fullWidth
            label="제목 또는 내용 검색"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
          <Stack direction="row" spacing={1} sx={{ overflowX: "auto", pb: { xs: 0.5, md: 0 }, flexShrink: 0 }}>
            <Chip clickable color={category === "" ? "primary" : "default"} label="전체" onClick={() => setCategory("")} />
            {categories.map((value) => <Chip key={value} clickable color={category === value ? "primary" : "default"} variant={category === value ? "filled" : "outlined"} label={value} onClick={() => setCategory(value)} />)}
          </Stack>
        </Stack>
      </Paper>

      <Grid container spacing={2}>
        {visibleItems.map((item) => (
          <Grid key={item.id} size={{ xs: 12, md: 6 }}>
            <Card sx={{ height: "100%" }}>
              <CardActionArea component="div" onClick={() => void openGuide(item)} sx={{ height: "100%", alignItems: "stretch" }}>
                <CardContent sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
                  <Stack direction="row" justifyContent="space-between" alignItems="flex-start" spacing={1}>
                    <Stack direction="row" spacing={0.8} flexWrap="wrap" useFlexGap>
                      <Chip size="small" label={item.category} variant="outlined" />
                      {item.pinned && <Chip size="small" icon={<PushPinRounded />} label="고정" color="primary" />}
                      {admin && <Chip size="small" label={item.published ? "게시" : "초안"} color={item.published ? "success" : "default"} />}
                    </Stack>
                    {admin && (
                      <Stack direction="row" onClick={(event) => event.stopPropagation()}>
                        <IconButton size="small" aria-label="가이드 수정" onClick={() => openEdit(item)}><EditOutlined fontSize="small" /></IconButton>
                        <IconButton size="small" color="error" aria-label="가이드 삭제" onClick={() => void remove(item)}><DeleteOutlineRounded fontSize="small" /></IconButton>
                      </Stack>
                    )}
                  </Stack>
                  <Typography variant="h6" sx={{ mt: 1.5 }}>{item.title}</Typography>
                  <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ mt: 1, whiteSpace: "pre-wrap", overflowWrap: "anywhere", display: "-webkit-box", WebkitLineClamp: 4, WebkitBoxOrient: "vertical", overflow: "hidden" }}
                  >
                    {item.content ?? item.excerpt}
                  </Typography>
                  <Typography variant="caption" color="text.secondary" sx={{ mt: "auto", pt: 2 }}>
                    {item.authorName} · {new Date(item.updatedAt).toLocaleDateString("ko-KR")}
                  </Typography>
                </CardContent>
              </CardActionArea>
            </Card>
          </Grid>
        ))}
        {visibleItems.length === 0 && (
          <Grid size={{ xs: 12 }}>
            <Paper variant="outlined" sx={{ p: 7, textAlign: "center" }}>
              <Typography color="text.secondary">표시할 가이드 글이 없습니다.</Typography>
            </Paper>
          </Grid>
        )}
      </Grid>

      <Dialog open={Boolean(selected)} onClose={() => setSelected(null)} fullWidth maxWidth="md">
        <DialogTitle>
          <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
            {selected?.pinned && <PushPinRounded color="primary" fontSize="small" />}
            <Typography variant="h6" component="span">{selected?.title}</Typography>
            {selected && <Chip size="small" label={selected.category} variant="outlined" />}
          </Stack>
        </DialogTitle>
        <DialogContent dividers>
          <Typography sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere", lineHeight: 1.85 }}>{selected?.content}</Typography>
        </DialogContent>
        <DialogActions>
          {admin && selected && <Button startIcon={<EditOutlined />} onClick={() => { setSelected(null); openEdit(selected); }}>수정</Button>}
          <Button onClick={() => setSelected(null)}>닫기</Button>
        </DialogActions>
      </Dialog>

      <Dialog open={editorOpen} onClose={() => !saving && setEditorOpen(false)} fullWidth maxWidth="md">
        <DialogTitle>{editingID ? "가이드 수정" : "가이드 등록"}</DialogTitle>
        <DialogContent>
          <Stack spacing={2} mt={1}>
			{error && <Alert severity="error" onClose={() => setError("")}>{error}</Alert>}
            <TextField required label="제목" value={form.title} inputProps={{ maxLength: 200 }} onChange={(event) => setForm({ ...form, title: event.target.value })} />
            <TextField required label="카테고리" value={form.category} inputProps={{ maxLength: 50 }} onChange={(event) => setForm({ ...form, category: event.target.value })} />
            <TextField required multiline minRows={12} label="내용" value={form.content} inputProps={{ maxLength: 100000 }} helperText="HTML은 실행되지 않으며 입력한 줄바꿈 그대로 표시됩니다." onChange={(event) => setForm({ ...form, content: event.target.value })} />
            <Stack direction={{ xs: "column", sm: "row" }} spacing={2}>
              <FormControlLabel control={<Switch checked={form.published} onChange={(event) => setForm({ ...form, published: event.target.checked })} />} label="사용자에게 게시" />
              <FormControlLabel control={<Switch checked={form.pinned} onChange={(event) => setForm({ ...form, pinned: event.target.checked })} />} label="상단 고정" />
            </Stack>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button disabled={saving} onClick={() => setEditorOpen(false)}>취소</Button>
          <Button variant="contained" disabled={saving || !form.title.trim() || !form.content.trim()} onClick={() => void save()}>{editingID ? "저장" : form.published ? "게시" : "초안 저장"}</Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
