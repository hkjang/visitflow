import { useEffect, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Grid, List, ListItem, ListItemText, Skeleton, Stack, Typography } from "@mui/material";
import TodayRounded from "@mui/icons-material/TodayRounded";
import EventAvailableRounded from "@mui/icons-material/EventAvailableRounded";
import NotificationsActiveRounded from "@mui/icons-material/NotificationsActiveRounded";
import HourglassTopRounded from "@mui/icons-material/HourglassTopRounded";
import AddRounded from "@mui/icons-material/AddRounded";
import ArrowForwardRounded from "@mui/icons-material/ArrowForwardRounded";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import type { Visit } from "../types";
import { MetricCard, PageHeader } from "../components/AdminUI";
import { StatusChip } from "../components/StatusChip";
import { useAuth } from "../auth";

export function PersonalDashboardPage() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const [data, setData] = useState<{ counts: Record<string, number>; items: Visit[] } | null>(null);
  const [error, setError] = useState("");
  useEffect(() => { api<{ counts: Record<string, number>; items: Visit[] }>("/api/v1/dashboard").then(setData).catch((e) => setError(e.message)); }, []);
  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1320, mx: "auto" }}>
    <PageHeader eyebrow="MY VISITS" title={`${user?.displayName ?? ""}님의 방문 일정`} description="방문 신청부터 도착·퇴실 상태까지 한눈에 확인하세요." actions={<Button variant="contained" startIcon={<AddRounded />} onClick={() => navigate("/visits/new")}>방문 신청</Button>} />
    {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
    {!data ? <Skeleton variant="rounded" height={160} /> : <Grid container spacing={2}>{[
      ["오늘 방문 예정", data.counts.today ?? 0, "오늘 등록된 전체 방문", <TodayRounded />, "#3978A8"],
      ["예정 방문", data.counts.upcoming ?? 0, "오늘 이후 확정 일정", <EventAvailableRounded />, "#176B5B"],
      ["도착 · 방문중", data.counts.arrived ?? 0, "담당자 확인 필요", <NotificationsActiveRounded />, "#E76F51"],
      ["승인 대기", data.counts.pending ?? 0, "승인 Workflow 대기", <HourglassTopRounded />, "#D58A20"],
    ].map(([label, value, helper, icon, tone]) => <Grid key={String(label)} size={{ xs: 6, lg: 3 }}><MetricCard label={String(label)} value={Number(value)} helper={String(helper)} icon={icon} tone={String(tone)} /></Grid>)}</Grid>}
    <Card sx={{ mt: 3 }}><CardContent><Stack direction="row" justifyContent="space-between" alignItems="center" mb={1}><Box><Typography variant="h6">오늘 방문</Typography><Typography variant="body2" color="text.secondary">가까운 일정부터 표시합니다.</Typography></Box><Button endIcon={<ArrowForwardRounded />} onClick={() => navigate("/visits?period=today")}>전체 보기</Button></Stack>{!data ? <Skeleton height={220} /> : data.items.length === 0 ? <Box sx={{ py: 8, textAlign: "center" }}><Typography color="text.secondary">오늘 예정된 방문이 없습니다.</Typography><Button sx={{ mt: 2 }} onClick={() => navigate("/visits/new")}>첫 방문 신청하기</Button></Box> : <List disablePadding>{data.items.map((visit) => <ListItem key={visit.id} divider secondaryAction={<StatusChip status={visit.status} />} sx={{ px: 0, py: 1.5 }}><Box sx={{ width: 64, textAlign: "center", mr: 2 }}><Typography fontWeight={800}>{new Date(visit.startAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" })}</Typography></Box><ListItemText primary={`${visit.primaryVisitor}${visit.visitorCount > 1 ? ` 외 ${visit.visitorCount - 1}명` : ""} · ${visit.company || "회사 미입력"}`} secondary={`${visit.siteName}${visit.lobbyName ? ` / ${visit.lobbyName}` : ""} · ${visit.purpose}`} /></ListItem>)}</List>}</CardContent></Card>
  </Box>;
}
