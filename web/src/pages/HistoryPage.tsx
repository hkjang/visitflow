import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Chip,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import ArrowForwardRounded from "@mui/icons-material/ArrowForwardRounded";
import { api } from "../api";
type Item = {
  id: string;
  changedAt: string;
  employeeNo: string;
  employeeName: string;
  previousSeat: string;
  newSeat: string;
  actor: string;
  reason: string;
  source: string;
};
export function HistoryPage() {
  const [items, setItems] = useState<Item[]>([]),
    [error, setError] = useState("");
  useEffect(() => {
    api<{ items: Item[] }>("/api/v1/seat-history?limit=500")
      .then((x) => setItems(x.items))
      .catch((e) => setError(e.message));
  }, []);
  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1400, mx: "auto" }}>
      <Typography variant="h5">변경 이력</Typography>
      <Typography color="text.secondary" mb={3}>
        좌석 이동은 별도 입력 없이 처리 주체와 사유까지 자동 기록됩니다.
      </Typography>
      {error && <Alert severity="error">{error}</Alert>}
      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>변경일시</TableCell>
              <TableCell>직원</TableCell>
              <TableCell>좌석 변경</TableCell>
              <TableCell>처리자</TableCell>
              <TableCell>사유</TableCell>
              <TableCell>방식</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {items.map((x) => (
              <TableRow key={x.id} hover>
                <TableCell>
                  {new Date(x.changedAt).toLocaleString("ko-KR")}
                </TableCell>
                <TableCell>
                  <Typography variant="body2" fontWeight={700}>
                    {x.employeeName}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {x.employeeNo}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Chip
                    size="small"
                    variant="outlined"
                    label={x.previousSeat || "미배정"}
                  />{" "}
                  <ArrowForwardRounded
                    sx={{ fontSize: 16, verticalAlign: "middle", mx: 0.5 }}
                  />{" "}
                  <Chip
                    size="small"
                    color="primary"
                    label={x.newSeat || "해제"}
                  />
                </TableCell>
                <TableCell>{x.actor}</TableCell>
                <TableCell>{x.reason || "-"}</TableCell>
                <TableCell>{x.source}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {items.length === 0 && (
        <Typography color="text.secondary" textAlign="center" py={6}>
          아직 좌석 변경 이력이 없습니다.
        </Typography>
      )}
    </Box>
  );
}
