import { useCallback, useEffect, useState } from "react";
import { Alert, Box, Button, Chip, Paper, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Typography } from "@mui/material";
import PrintRounded from "@mui/icons-material/PrintRounded";
import RefreshRounded from "@mui/icons-material/RefreshRounded";
import { api } from "../api";
import { PageHeader } from "../components/AdminUI";

export type RosterEntry = {
  site: string; lobby?: string; visitor: string; company?: string; phone?: string;
  host: string; department?: string; checkedInAt?: string; badgeNo?: string; placeDetail?: string;
};
type Roster = { generatedAt: string; count: number; items: RosterEntry[]; offline?: boolean };

const ROSTER_STORAGE_KEY = "visitflow_last_roster";

// The evacuation roster is printed and carried out of the building, so it keeps
// the last successful response in local storage: an outage during an emergency
// must not leave the lobby without a list.
function readCachedRoster(): Roster | null {
  try {
    const raw = window.localStorage.getItem(ROSTER_STORAGE_KEY);
    return raw ? (JSON.parse(raw) as Roster) : null;
  } catch {
    return null;
  }
}

export function RosterPage() {
  const [roster, setRoster] = useState<Roster | null>(() => readCachedRoster());
  const [stale, setStale] = useState(false);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    try {
      const result = await api<Roster>("/api/v1/lobby/roster");
      setRoster(result);
      setStale(Boolean(result.offline));
      setError("");
      try { window.localStorage.setItem(ROSTER_STORAGE_KEY, JSON.stringify(result)); } catch { /* private mode */ }
    } catch (e) {
      setStale(true);
      setError(e instanceof Error ? e.message : "명단을 불러오지 못했습니다");
    }
  }, []);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => { const timer = window.setInterval(() => void load(), 60000); return () => window.clearInterval(timer); }, [load]);

  const grouped = (roster?.items ?? []).reduce<Record<string, RosterEntry[]>>((acc, item) => {
    const key = `${item.site}${item.lobby ? ` · ${item.lobby}` : ""}`;
    (acc[key] ??= []).push(item);
    return acc;
  }, {});

  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1200, mx: "auto", "@media print": { p: 0 } }}>
    <Box sx={{ "@media print": { display: "none" } }}>
      <PageHeader eyebrow="EMERGENCY" title="비상 대피 명단" description="현재 사내에 체류 중인 방문자를 사업장·로비별로 인쇄할 수 있습니다."
        actions={<><Button startIcon={<RefreshRounded />} onClick={() => void load()}>새로고침</Button><Button variant="contained" startIcon={<PrintRounded />} onClick={() => window.print()}>인쇄</Button></>} />
      {error && <Alert severity="warning" sx={{ mb: 2 }}>{error} · 마지막으로 받은 명단을 표시합니다.</Alert>}
      {stale && !error && <Alert severity="warning" sx={{ mb: 2 }}>오프라인 상태입니다. 마지막으로 동기화된 명단입니다.</Alert>}
    </Box>
    <Paper variant="outlined" sx={{ p: { xs: 2, md: 3 } }}>
      <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "baseline" }} spacing={1} mb={2}>
        <Box>
          <Typography variant="h5">비상 대피 명단 · 현재 체류 방문자</Typography>
          <Typography variant="body2" color="text.secondary">
            기준 시각 {roster ? new Date(roster.generatedAt).toLocaleString("ko-KR") : "-"}
          </Typography>
        </Box>
        <Chip color="primary" label={`총 ${roster?.count ?? 0}명`} sx={{ fontWeight: 800 }} />
      </Stack>
      {Object.entries(grouped).map(([group, entries]) => <Box key={group} sx={{ mb: 3, breakInside: "avoid" }}>
        <Typography variant="subtitle1" fontWeight={800} sx={{ mb: 1 }}>{group} · {entries.length}명</Typography>
        <TableContainer><Table size="small"><TableHead><TableRow>
          <TableCell>방문자</TableCell><TableCell>회사</TableCell><TableCell>연락처</TableCell><TableCell>담당자 · 부서</TableCell><TableCell>입실</TableCell><TableCell>출입증</TableCell><TableCell>확인</TableCell>
        </TableRow></TableHead><TableBody>
          {entries.map((item, index) => <TableRow key={`${group}-${index}`}>
            <TableCell sx={{ fontWeight: 700 }}>{item.visitor}</TableCell>
            <TableCell>{item.company || "-"}</TableCell>
            <TableCell>{item.phone || "-"}</TableCell>
            <TableCell>{item.host}{item.department ? ` · ${item.department}` : ""}</TableCell>
            <TableCell>{item.checkedInAt ? new Date(item.checkedInAt).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" }) : "-"}</TableCell>
            <TableCell>{item.badgeNo || "-"}</TableCell>
            <TableCell sx={{ minWidth: 70 }}>☐</TableCell>
          </TableRow>)}
        </TableBody></Table></TableContainer>
      </Box>)}
      {(roster?.items.length ?? 0) === 0 && <Typography color="text.secondary" sx={{ py: 6, textAlign: "center" }}>현재 사내에 체류 중인 방문자가 없습니다.</Typography>}
    </Paper>
  </Box>;
}
