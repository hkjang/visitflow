import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Grid,
  Paper,
  Stack,
  Typography,
} from "@mui/material";
import CheckCircleRounded from "@mui/icons-material/CheckCircleRounded";
import PersonOffRounded from "@mui/icons-material/PersonOffRounded";
import EventSeatRounded from "@mui/icons-material/EventSeatRounded";
import SyncProblemRounded from "@mui/icons-material/SyncProblemRounded";
import AutoAwesomeRounded from "@mui/icons-material/AutoAwesomeRounded";
import { api } from "../api";

type Counts = {
  unassignedEmployees: number;
  unusedSeats: number;
  retiredAssignments: number;
  organizationMismatch: number;
  lowConfidenceSeats: number;
  actionRequired: number;
};
export function DashboardPage() {
  const [data, setData] = useState<Counts | null>(null),
    [error, setError] = useState("");
  useEffect(() => {
    api<Counts>("/api/v1/dashboard")
      .then(setData)
      .catch((e) => setError(e.message));
  }, []);
  const cards = data
    ? [
        {
          label: "미배정 직원",
          value: data.unassignedEmployees,
          icon: <PersonOffRounded />,
          tone: "#D64D55",
          help: "재직 중이나 좌석이 없는 직원",
        },
        {
          label: "사용 가능 좌석",
          value: data.unusedSeats,
          icon: <EventSeatRounded />,
          tone: "#2E9D67",
          help: "게시 도면의 빈 좌석",
        },
        {
          label: "퇴직자 좌석",
          value: data.retiredAssignments,
          icon: <SyncProblemRounded />,
          tone: "#E79418",
          help: "자동 해제가 필요한 좌석",
        },
        {
          label: "조직 영역 불일치",
          value: data.organizationMismatch,
          icon: <SyncProblemRounded />,
          tone: "#8E5FC7",
          help: "현재 조직과 좌석 영역이 다름",
        },
        {
          label: "AI 확인 필요",
          value: data.lowConfidenceSeats,
          icon: <AutoAwesomeRounded />,
          tone: "#3478C8",
          help: "신뢰도 80% 미만 좌석",
        },
      ]
    : [];
  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1400, mx: "auto" }}>
      <Stack
        direction={{ xs: "column", sm: "row" }}
        justifyContent="space-between"
        spacing={2}
        mb={3}
      >
        <Box>
          <Typography variant="h5">처리필요</Typography>
          <Typography color="text.secondary">
            정상 현황은 숨기고, 사람이 판단할 항목만 모았습니다.
          </Typography>
        </Box>
        {data && (
          <Chip
            color={data.actionRequired ? "warning" : "success"}
            label={
              data.actionRequired
                ? `${data.actionRequired}건 확인 필요`
                : "모두 정상"
            }
            sx={{ alignSelf: "flex-start" }}
          />
        )}
      </Stack>
      {error && <Alert severity="error">{error}</Alert>}
      {!data && !error ? (
        <CircularProgress />
      ) : (
        <>
          {data?.actionRequired === 0 && (
            <Paper
              sx={{
                p: 4,
                mb: 3,
                textAlign: "center",
                background: "linear-gradient(120deg,#E9F8F2,#F6FBF9)",
              }}
            >
              <CheckCircleRounded color="success" sx={{ fontSize: 48 }} />
              <Typography variant="h6" mt={1}>
                처리 필요한 항목이 없습니다.
              </Typography>
              <Typography color="text.secondary">
                좌석과 직원 데이터가 정상 상태입니다.
              </Typography>
            </Paper>
          )}
          <Grid container spacing={2}>
            {cards.map((card) => (
              <Grid key={card.label} size={{ xs: 12, sm: 6, lg: 4 }}>
                <Card>
                  <CardContent>
                    <Stack direction="row" justifyContent="space-between">
                      <Box>
                        <Typography color="text.secondary" variant="body2">
                          {card.label}
                        </Typography>
                        <Typography variant="h4" mt={1}>
                          {card.value}
                        </Typography>
                      </Box>
                      <Box
                        sx={{
                          width: 48,
                          height: 48,
                          borderRadius: 3,
                          display: "grid",
                          placeItems: "center",
                          color: card.tone,
                          bgcolor: `${card.tone}15`,
                        }}
                      >
                        {card.icon}
                      </Box>
                    </Stack>
                    <Typography variant="caption" color="text.secondary">
                      {card.help}
                    </Typography>
                  </CardContent>
                </Card>
              </Grid>
            ))}
          </Grid>
        </>
      )}
    </Box>
  );
}
