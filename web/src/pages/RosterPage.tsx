import { useCallback, useEffect, useState } from "react";
import { Alert, Box, Button, Chip, Paper, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Typography } from "@mui/material";
import PrintRounded from "@mui/icons-material/PrintRounded";
import RefreshRounded from "@mui/icons-material/RefreshRounded";
import { api } from "../api";
import { PageHeader } from "../components/AdminUI";
import { rosterStatus, type Roster, type RosterEntry } from "../roster";

export type { RosterEntry };

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
  const [failed, setFailed] = useState(false);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    try {
      const result = await api<Roster>("/api/v1/lobby/roster");
      setRoster(result);
      setFailed(false);
      setError("");
      try { window.localStorage.setItem(ROSTER_STORAGE_KEY, JSON.stringify(result)); } catch { /* private mode */ }
    } catch (e) {
      setFailed(true);
      setError(e instanceof Error ? e.message : "명단을 불러오지 못했습니다");
    }
  }, []);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => { const timer = window.setInterval(() => void load(), 60000); return () => window.clearInterval(timer); }, [load]);

  // 기준 시각·칩·빈 상태 문장·경고가 모두 이 한 값을 읽는다.
  const status = rosterStatus({ roster, failed, errorMessage: error });
  const grouped = (roster?.items ?? []).reduce<Record<string, RosterEntry[]>>((acc, item) => {
    const key = `${item.site}${item.lobby ? ` · ${item.lobby}` : ""}`;
    (acc[key] ??= []).push(item);
    return acc;
  }, {});

  return <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1200, mx: "auto", "@media print": { p: 0 } }}>
    <Box sx={{ "@media print": { display: "none" } }}>
      <PageHeader eyebrow="EMERGENCY" title="비상 대피 명단" description="현재 사내에 체류 중인 방문자를 사업장·로비별로 인쇄할 수 있습니다."
        actions={<><Button startIcon={<RefreshRounded />} onClick={() => void load()}>새로고침</Button><Button variant="contained" startIcon={<PrintRounded />} onClick={() => window.print()}>인쇄</Button></>} />
    </Box>
    {/* 종이가 자기 신뢰도를 말해야 하므로 이 경고는 인쇄 미디어에서도 남는다.
        인쇄에서는 배경색이 빠질 수 있어 테두리와 검은 글자로 읽히게 한다. */}
    {status.warning !== "" && <Alert severity={status.severity} sx={{ mb: 2, "@media print": { border: "1px solid #000", color: "#000", bgcolor: "transparent", fontWeight: 700 } }}>{status.warning}</Alert>}
    <Paper variant="outlined" sx={{ p: { xs: 2, md: 3 } }}>
      <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "baseline" }} spacing={1} mb={2}>
        <Box>
          <Typography variant="h5">비상 대피 명단 · 현재 체류 방문자</Typography>
          <Typography variant="body2" color="text.secondary">
            기준 시각 {status.asOf ? new Date(status.asOf).toLocaleString("ko-KR") : status.asOfFallback}
          </Typography>
        </Box>
        <Chip color={status.severity === "error" && status.warning !== "" ? "error" : "primary"} label={status.countLabel} sx={{ fontWeight: 800 }} />
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
      {(roster?.items.length ?? 0) === 0 && <Typography color="text.secondary" sx={{ py: 6, textAlign: "center" }}>{status.emptyText}</Typography>}
    </Paper>
  </Box>;
}
