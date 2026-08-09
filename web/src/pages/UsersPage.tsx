import { useEffect, useState } from "react";
import {
  Alert,
  Avatar,
  Box,
  Chip,
  MenuItem,
  Paper,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { api, patchJSON } from "../api";
import type { Role, User } from "../types";
const labels: Record<Role, string> = {
  employee: "직원",
  department_manager: "부서 관리자",
  seat_manager: "좌석 관리자",
  system_admin: "시스템 관리자",
};
export function UsersPage() {
  const [items, setItems] = useState<User[]>([]),
    [error, setError] = useState("");
  const load = () =>
    api<{ items: User[] }>("/api/v1/users")
      .then((x) => setItems(x.items))
      .catch((e) => setError(e.message));
  useEffect(() => {
    void load();
  }, []);
  const change = async (id: string, role: Role) => {
    try {
      await patchJSON(`/api/v1/users/${id}`, { role });
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "권한을 변경하지 못했습니다");
    }
  };
  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1100, mx: "auto" }}>
      <Typography variant="h5">사용자 권한</Typography>
      <Typography color="text.secondary" mb={3}>
        SSO 사용자는 최초 로그인 시 자동 생성되고 Keycloak 그룹으로 기본 권한이
        결정됩니다.
      </Typography>
      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}
      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>사용자</TableCell>
              <TableCell>로그인 방식</TableCell>
              <TableCell>최근 로그인</TableCell>
              <TableCell>권한</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {items.map((u) => (
              <TableRow key={u.id}>
                <TableCell>
                  <Stack direction="row" alignItems="center" spacing={1}>
                    <Avatar
                      sx={{
                        width: 34,
                        height: 34,
                        bgcolor: "primary.main",
                        fontSize: 13,
                      }}
                    >
                      {u.displayName.slice(0, 1)}
                    </Avatar>
                    <Box>
                      <Typography variant="body2" fontWeight={700}>
                        {u.displayName}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        {u.email || u.username}
                      </Typography>
                    </Box>
                  </Stack>
                </TableCell>
                <TableCell>
                  <Chip
                    size="small"
                    variant="outlined"
                    label={u.source === "oidc" ? "Keycloak SSO" : "Local"}
                  />
                </TableCell>
                <TableCell>
                  {u.lastLoginAt
                    ? new Date(u.lastLoginAt).toLocaleString("ko-KR")
                    : "-"}
                </TableCell>
                <TableCell>
                  <Select
                    size="small"
                    value={u.role}
                    onChange={(e) => void change(u.id, e.target.value as Role)}
                  >
                    {Object.entries(labels).map(([value, label]) => (
                      <MenuItem key={value} value={value}>
                        {label}
                      </MenuItem>
                    ))}
                  </Select>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
}
