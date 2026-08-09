import { Chip } from "@mui/material";

const labels: Record<string, string> = { REQUESTED: "신청", PENDING_APPROVAL: "승인 대기", APPROVED: "승인", SCHEDULED: "방문 예정", ARRIVED: "도착", CHECKED_IN: "방문 중", CHECKED_OUT: "퇴실 완료", CANCELLED: "취소", REJECTED: "반려", NO_SHOW: "미방문", EXPIRED: "기간 만료" };
const colors: Record<string, "default" | "info" | "warning" | "success" | "error" | "primary"> = { REQUESTED: "default", PENDING_APPROVAL: "warning", APPROVED: "info", SCHEDULED: "info", ARRIVED: "primary", CHECKED_IN: "success", CHECKED_OUT: "default", CANCELLED: "error", REJECTED: "error", NO_SHOW: "warning", EXPIRED: "error" };

export function StatusChip({ status }: { status: string }) {
  return <Chip size="small" color={colors[status] ?? "default"} variant={status === "CHECKED_IN" ? "filled" : "outlined"} label={labels[status] ?? status} sx={{ fontWeight: 750 }} />;
}
export const statusLabel = (status: string) => labels[status] ?? status;
