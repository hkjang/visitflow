import type { ReactNode } from "react";
import {
  Box,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  LinearProgress,
  Skeleton,
  Stack,
  Typography,
} from "@mui/material";

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow?: string;
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <Stack
      direction={{ xs: "column", md: "row" }}
      justifyContent="space-between"
      alignItems={{ md: "center" }}
      spacing={2}
      sx={{ mb: 3 }}
    >
      <Box>
        {eyebrow && (
          <Typography
            variant="overline"
            sx={{ color: "primary.main", fontWeight: 800, letterSpacing: 1.2 }}
          >
            {eyebrow}
          </Typography>
        )}
        <Typography variant="h5">{title}</Typography>
        <Typography color="text.secondary" sx={{ mt: 0.4 }}>
          {description}
        </Typography>
      </Box>
      {actions && (
        <Stack direction="row" spacing={1}>
          {actions}
        </Stack>
      )}
    </Stack>
  );
}

export function MetricCard({
  label,
  value,
  helper,
  icon,
  tone = "#087E8B",
  progress,
}: {
  label: string;
  value: string | number;
  helper: string;
  icon: ReactNode;
  tone?: string;
  progress?: number;
}) {
  return (
    <Card sx={{ height: "100%" }}>
      <CardContent>
        <Stack direction="row" justifyContent="space-between" spacing={2}>
          <Box>
            <Typography variant="body2" color="text.secondary">
              {label}
            </Typography>
            <Typography variant="h4" sx={{ mt: 0.6, mb: 0.4 }}>
              {value}
            </Typography>
          </Box>
          <Box
            sx={{
              width: 44,
              height: 44,
              borderRadius: 3,
              display: "grid",
              placeItems: "center",
              bgcolor: `${tone}16`,
              color: tone,
              flexShrink: 0,
            }}
          >
            {icon}
          </Box>
        </Stack>
        {progress != null && (
          <LinearProgress
            variant="determinate"
            value={Math.max(0, Math.min(100, progress))}
            sx={{
              my: 1,
              height: 6,
              borderRadius: 9,
              bgcolor: `${tone}16`,
              "& .MuiLinearProgress-bar": { bgcolor: tone },
            }}
          />
        )}
        <Typography variant="caption" color="text.secondary">
          {helper}
        </Typography>
      </CardContent>
    </Card>
  );
}

export function SeverityChip({ severity }: { severity: string }) {
  const config: Record<
    string,
    { label: string; color: "error" | "warning" | "info" | "default" }
  > = {
    critical: { label: "긴급", color: "error" },
    warning: { label: "확인", color: "warning" },
    info: { label: "검토", color: "info" },
  };
  const item = config[severity] ?? {
    label: severity,
    color: "default" as const,
  };
  return (
    <Chip
      size="small"
      color={item.color}
      label={item.label}
      sx={{ fontWeight: 750 }}
    />
  );
}

export function LoadingCards() {
  return (
    <Stack spacing={2}>
      <Skeleton variant="rounded" height={150} />
      <Stack direction={{ xs: "column", md: "row" }} spacing={2}>
        {[1, 2, 3, 4].map((item) => (
          <Skeleton
            key={item}
            variant="rounded"
            height={140}
            sx={{ flex: 1 }}
          />
        ))}
      </Stack>
      <Skeleton variant="rounded" height={320} />
    </Stack>
  );
}

export function InlineBusy({ label = "처리 중" }: { label?: string }) {
  return (
    <Stack direction="row" spacing={1} alignItems="center">
      <CircularProgress size={15} />
      <Typography variant="caption">{label}</Typography>
    </Stack>
  );
}
