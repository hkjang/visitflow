import { useEffect, useState, type FormEvent } from "react";
import {
  Alert,
  Avatar,
  Box,
  Button,
  Chip,
  InputAdornment,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from "@mui/material";
import SearchRounded from "@mui/icons-material/SearchRounded";
import UploadFileRounded from "@mui/icons-material/UploadFileRounded";
import { api } from "../api";
import type { Employee } from "../types";

export function EmployeesPage() {
  const [items, setItems] = useState<Employee[]>([]),
    [q, setQ] = useState(""),
    [message, setMessage] = useState(""),
    [error, setError] = useState("");
  const load = async (query = "") => {
    try {
      const data = await api<{ items: Employee[] }>(
        `/api/v1/employees?limit=500&q=${encodeURIComponent(query)}`,
      );
      setItems(data.items);
    } catch (e) {
      setError(e instanceof Error ? e.message : "직원을 불러오지 못했습니다");
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const search = (e: FormEvent) => {
    e.preventDefault();
    void load(q);
  };
  const upload = async (file?: File) => {
    if (!file) return;
    const form = new FormData();
    form.append("file", file);
    try {
      const result = await api<{ success: number; failed: number }>(
        "/api/v1/employees/import",
        { method: "POST", body: form },
      );
      setMessage(`${result.success}명 반영, ${result.failed}건 확인 필요`);
      await load(q);
    } catch (e) {
      setError(e instanceof Error ? e.message : "가져오기에 실패했습니다");
    }
  };
  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1400, mx: "auto" }}>
      <Stack
        direction={{ xs: "column", md: "row" }}
        justifyContent="space-between"
        alignItems={{ md: "center" }}
        spacing={2}
        mb={3}
      >
        <Box>
          <Typography variant="h5">직원</Typography>
          <Typography color="text.secondary">
            인사 연동과 CSV/XLSX 가져오기로 직원 마스터를 유지합니다.
          </Typography>
        </Box>
        <Button
          component="label"
          variant="contained"
          startIcon={<UploadFileRounded />}
        >
          직원 파일 가져오기
          <input
            hidden
            type="file"
            accept=".csv,.xlsx"
            onChange={(e) => void upload(e.target.files?.[0])}
          />
        </Button>
      </Stack>
      {message && (
        <Alert severity="success" onClose={() => setMessage("")} sx={{ mb: 2 }}>
          {message}
        </Alert>
      )}
      {error && (
        <Alert severity="error" onClose={() => setError("")} sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}
      <Paper sx={{ p: 2, mb: 2 }}>
        <Box component="form" onSubmit={search}>
          <TextField
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="이름, 사번, 이메일, 조직 검색"
            sx={{ width: { xs: "100%", md: 420 } }}
            slotProps={{
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchRounded />
                  </InputAdornment>
                ),
              },
            }}
          />
        </Box>
      </Paper>
      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>직원</TableCell>
              <TableCell>사번</TableCell>
              <TableCell>조직</TableCell>
              <TableCell>직책/직급</TableCell>
              <TableCell>좌석</TableCell>
              <TableCell>상태</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {items.map((e) => (
              <TableRow key={e.id} hover>
                <TableCell>
                  <Stack direction="row" spacing={1.2} alignItems="center">
                    <Avatar
                      sx={{
                        width: 34,
                        height: 34,
                        bgcolor: "primary.main",
                        fontSize: 13,
                      }}
                    >
                      {e.name.slice(0, 1)}
                    </Avatar>
                    <Box>
                      <Typography variant="body2" fontWeight={700}>
                        {e.name}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {e.email}
                      </Typography>
                    </Box>
                  </Stack>
                </TableCell>
                <TableCell>{e.employeeNo}</TableCell>
                <TableCell>{e.organizationName || "-"}</TableCell>
                <TableCell>
                  {[e.position, e.title].filter(Boolean).join(" · ") || "-"}
                </TableCell>
                <TableCell>
                  <Chip
                    size="small"
                    variant={e.seatNo ? "filled" : "outlined"}
                    color={e.seatNo ? "primary" : "default"}
                    label={e.seatNo || "미배정"}
                  />
                </TableCell>
                <TableCell>
                  {e.status === "active"
                    ? "재직"
                    : e.status === "leave"
                      ? "휴직"
                      : "퇴직"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      <Typography
        variant="caption"
        color="text.secondary"
        display="block"
        mt={2}
      >
        파일 헤더: 사번, 이름, 이메일, 조직코드, 조직명, 직급, 직책, 근무지,
        재직상태
      </Typography>
    </Box>
  );
}
