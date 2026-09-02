import { useCallback, useEffect, useState } from "react";
import { Alert, Box, Card, CardContent, Chip, CircularProgress, Divider, MenuItem, Paper, Select, Stack, Typography } from "@mui/material";
import LocationOnOutlined from "@mui/icons-material/LocationOnOutlined";
import ScheduleRounded from "@mui/icons-material/ScheduleRounded";
import PersonOutlineRounded from "@mui/icons-material/PersonOutlineRounded";
import ShieldOutlined from "@mui/icons-material/ShieldOutlined";
import TranslateRounded from "@mui/icons-material/TranslateRounded";
import { useParams } from "react-router-dom";
import { api } from "../api";
import { Logo } from "../components/Logo";
import { formatDateTime, formatTime, localeNames, translations, type Locale } from "../i18n";

type Pass = {
  visitor: string; company?: string; host: string; department?: string; site: string; lobby?: string;
  purpose: string; startAt: string; endAt: string; status: string; version: number;
  locale: Locale; supportedLocales: Locale[]; qrImageUrl: string;
};

export function MobilePassPage() {
  const { token } = useParams();
  const [data, setData] = useState<Pass | null>(null);
  const [error, setError] = useState("");
  const [tick, setTick] = useState(0);
  const [locale, setLocale] = useState<Locale | "">("");
  const text = translations(locale || data?.locale);

  const load = useCallback(async (requested: Locale | "") => {
    if (!token) return;
    const query = requested ? `?lang=${requested}` : "";
    try {
      setData(await api<Pass>(`/api/v1/public/passes/${encodeURIComponent(token)}${query}`));
    } catch (e) {
      setError(e instanceof Error ? e.message : translations(requested).passNotFound);
    }
  }, [token]);

  useEffect(() => { void load(locale); }, [load, locale]);
  // The QR image is re-requested on a timer so a dynamic QR stays current.
  useEffect(() => { const timer = window.setInterval(() => setTick((x) => x + 1), 15000); return () => window.clearInterval(timer); }, []);

  const active = locale || data?.locale || "ko";
  return <Box sx={{ minHeight: "100vh", bgcolor: "#EAF1EE", p: { xs: 1.5, sm: 4 }, display: "grid", placeItems: "center" }} lang={active}>
    <Card sx={{ width: "100%", maxWidth: 480, overflow: "hidden", borderRadius: 4 }}>
      <Box sx={{ bgcolor: "primary.dark", color: "white", p: 2.5 }}>
        <Stack direction="row" justifyContent="space-between" alignItems="center">
          <Logo inverse />
          <Chip size="small" label={`PASS v${data?.version ?? "-"}`} sx={{ bgcolor: "rgba(255,255,255,.15)", color: "white" }} />
        </Stack>
      </Box>
      <CardContent sx={{ p: { xs: 2.5, sm: 4 } }}>
        {data && (data.supportedLocales?.length ?? 0) > 1 && <Stack direction="row" spacing={1} alignItems="center" justifyContent="flex-end" mb={2}>
          <TranslateRounded fontSize="small" color="action" />
          <Select size="small" value={active} onChange={(e) => setLocale(e.target.value as Locale)} aria-label={text.language} sx={{ minWidth: 130 }}>
            {data.supportedLocales.map((item) => <MenuItem key={item} value={item}>{localeNames[item] ?? item}</MenuItem>)}
          </Select>
        </Stack>}
        {error ? <Alert severity="error">{error}</Alert> : !data ? <Box sx={{ py: 12, display: "grid", placeItems: "center" }}><CircularProgress /></Box> : <>
          <Stack direction="row" justifyContent="space-between" alignItems="start">
            <Box>
              <Typography variant="overline" color="text.secondary">{text.visitor.toUpperCase()}</Typography>
              <Typography variant="h4">{data.visitor}</Typography>
              <Typography color="text.secondary">{data.company || text.companyMissing}</Typography>
            </Box>
            <Chip size="small" color={data.status === "CHECKED_IN" ? "success" : data.status === "EXPIRED" || data.status === "CANCELLED" ? "error" : "info"} label={text.statusLabels[data.status] ?? data.status} sx={{ fontWeight: 750 }} />
          </Stack>
          <Paper variant="outlined" sx={{ p: 2, my: 3, textAlign: "center", bgcolor: "white" }}>
            <Box component="img" src={`${data.qrImageUrl}&tick=${tick}`} alt={text.passTitle} sx={{ display: "block", width: "100%", maxWidth: 360, mx: "auto" }} />
            <Typography variant="caption" color="text.secondary">{text.scanHere}</Typography>
          </Paper>
          <Stack spacing={2}>
            <Info icon={<ScheduleRounded />} label={text.visitTime} value={`${formatDateTime(data.startAt, active)} – ${formatTime(data.endAt, active)}`} />
            <Info icon={<LocationOnOutlined />} label={text.place} value={`${data.site}${data.lobby ? ` · ${data.lobby}` : ""}`} />
            <Info icon={<PersonOutlineRounded />} label={text.host} value={`${data.department || text.departmentMissing} · ${data.host}`} />
          </Stack>
          <Divider sx={{ my: 3 }} />
          <Alert icon={<ShieldOutlined />} severity="info">{text.privacyNote}</Alert>
        </>}
      </CardContent>
    </Card>
  </Box>;
}

function Info({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return <Stack direction="row" spacing={1.5} alignItems="center">
    <Box sx={{ width: 38, height: 38, borderRadius: 2.5, bgcolor: "#EDF4F1", color: "primary.main", display: "grid", placeItems: "center" }}>{icon}</Box>
    <Box><Typography variant="caption" color="text.secondary">{label}</Typography><Typography variant="body2" fontWeight={750}>{value}</Typography></Box>
  </Stack>;
}
