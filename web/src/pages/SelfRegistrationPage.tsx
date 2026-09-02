import { useCallback, useEffect, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Checkbox, Divider, FormControlLabel, Grid, MenuItem, Paper, Select, Stack, TextField, Typography } from "@mui/material";
import CheckCircleRounded from "@mui/icons-material/CheckCircleRounded";
import TranslateRounded from "@mui/icons-material/TranslateRounded";
import { useParams } from "react-router-dom";
import { api, postJSON } from "../api";
import { Logo } from "../components/Logo";
import { formatDateTime, formatTime, localeNames, translations, type Locale } from "../i18n";

type Registration = {
  locale: Locale;
  supportedLocales: Locale[];
  policyVersion: string;
  companyName: string;
  visit: { host: string; department?: string; site: string; lobby?: string; purpose: string; startAt: string; endAt: string };
  visitor: { name: string; company?: string; title?: string; email?: string; vehicle?: string; equipment?: string[] };
  requires: { nda: boolean; safetyBriefing: boolean; vehicle: boolean; equipment: boolean };
  visitType: { name?: string; description?: string };
};

type Draft = { name: string; company: string; title: string; email: string; vehicle: string; equipment: string; consent: boolean; nda: boolean; safetyBriefing: boolean };

export function SelfRegistrationPage() {
  const { token } = useParams();
  const [data, setData] = useState<Registration | null>(null);
  const [draft, setDraft] = useState<Draft>({ name: "", company: "", title: "", email: "", vehicle: "", equipment: "", consent: false, nda: false, safetyBriefing: false });
  const [locale, setLocale] = useState<Locale | "">("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const active = locale || data?.locale || "ko";
  const text = translations(active);

  const load = useCallback(async (requested: Locale | "") => {
    if (!token) return;
    try {
      const result = await api<Registration>(`/api/v1/public/registrations/${encodeURIComponent(token)}${requested ? `?lang=${requested}` : ""}`);
      setData(result);
      setDraft((current) => current.name ? current : {
        name: result.visitor.name ?? "", company: result.visitor.company ?? "", title: result.visitor.title ?? "",
        email: result.visitor.email ?? "", vehicle: result.visitor.vehicle ?? "",
        equipment: (result.visitor.equipment ?? []).join(", "), consent: false, nda: false, safetyBriefing: false,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : "This registration link is no longer valid.");
    }
  }, [token]);
  useEffect(() => { void load(locale); }, [load, locale]);

  const submit = async () => {
    if (!token) return;
    setBusy(true); setError("");
    try {
      await postJSON(`/api/v1/public/registrations/${encodeURIComponent(token)}`, {
        name: draft.name.trim(), company: draft.company.trim(), title: draft.title.trim(), email: draft.email.trim(),
        vehicle: draft.vehicle.trim(), equipment: draft.equipment.split(",").map((x) => x.trim()).filter(Boolean),
        locale: active, consent: draft.consent, nda: draft.nda, safetyBriefing: draft.safetyBriefing,
      });
      setDone(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : text.required);
    } finally { setBusy(false); }
  };

  const requires = data?.requires;
  const ready = Boolean(draft.name.trim()) && draft.consent
    && (!requires?.nda || draft.nda)
    && (!requires?.safetyBriefing || draft.safetyBriefing)
    && (!requires?.vehicle || Boolean(draft.vehicle.trim()))
    && (!requires?.equipment || Boolean(draft.equipment.trim()));

  return <Box sx={{ minHeight: "100vh", bgcolor: "#EAF1EE", p: { xs: 1.5, sm: 4 }, display: "grid", placeItems: "center" }} lang={active}>
    <Card sx={{ width: "100%", maxWidth: 640, borderRadius: 4, overflow: "hidden" }}>
      <Box sx={{ bgcolor: "primary.dark", color: "white", p: 2.5 }}><Logo inverse /></Box>
      <CardContent sx={{ p: { xs: 2.5, sm: 4 } }}>
        {done ? <Stack spacing={2} alignItems="center" textAlign="center" py={4}>
          <CheckCircleRounded color="success" sx={{ fontSize: 64 }} />
          <Typography variant="h5">{text.registrationDone}</Typography>
          <Typography color="text.secondary">{text.registrationDoneNote}</Typography>
        </Stack> : <>
          {data && (data.supportedLocales?.length ?? 0) > 1 && <Stack direction="row" spacing={1} alignItems="center" justifyContent="flex-end" mb={2}>
            <TranslateRounded fontSize="small" color="action" />
            <Select size="small" value={active} onChange={(e) => setLocale(e.target.value as Locale)} aria-label={text.language} sx={{ minWidth: 130 }}>
              {data.supportedLocales.map((item) => <MenuItem key={item} value={item}>{localeNames[item] ?? item}</MenuItem>)}
            </Select>
          </Stack>}
          <Typography variant="h5">{text.registrationTitle}</Typography>
          <Typography color="text.secondary" sx={{ mt: 1 }}>{text.registrationIntro}</Typography>
          {error && <Alert severity="error" sx={{ mt: 2 }} onClose={() => setError("")}>{error}</Alert>}
          {data && <>
            <Paper variant="outlined" sx={{ p: 2, my: 3, bgcolor: "#F7FAF8" }}>
              <Stack spacing={0.6}>
                <Typography variant="body2"><strong>{text.visitTime}</strong> · {formatDateTime(data.visit.startAt, active)} – {formatTime(data.visit.endAt, active)}</Typography>
                <Typography variant="body2"><strong>{text.place}</strong> · {data.visit.site}{data.visit.lobby ? ` · ${data.visit.lobby}` : ""}</Typography>
                <Typography variant="body2"><strong>{text.host}</strong> · {data.visit.department || text.departmentMissing} · {data.visit.host}</Typography>
                <Typography variant="body2"><strong>{text.purpose}</strong> · {data.visit.purpose}</Typography>
                {data.visitType.name && <Typography variant="body2" color="text.secondary">{data.visitType.name}{data.visitType.description ? ` — ${data.visitType.description}` : ""}</Typography>}
              </Stack>
            </Paper>
            <Grid container spacing={2}>
              <Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth required label={text.name} value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} /></Grid>
              <Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth label={text.company} value={draft.company} onChange={(e) => setDraft({ ...draft, company: e.target.value })} /></Grid>
              <Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth label={text.jobTitle} value={draft.title} onChange={(e) => setDraft({ ...draft, title: e.target.value })} /></Grid>
              <Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth type="email" label={text.email} value={draft.email} onChange={(e) => setDraft({ ...draft, email: e.target.value })} /></Grid>
              <Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth required={requires?.vehicle} label={text.vehicle} value={draft.vehicle} onChange={(e) => setDraft({ ...draft, vehicle: e.target.value })} /></Grid>
              <Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth required={requires?.equipment} label={text.equipment} value={draft.equipment} onChange={(e) => setDraft({ ...draft, equipment: e.target.value })} helperText={text.equipmentHelp} /></Grid>
            </Grid>
            <Divider sx={{ my: 3 }} />
            <Stack spacing={1}>
              <FormControlLabel control={<Checkbox checked={draft.consent} onChange={(e) => setDraft({ ...draft, consent: e.target.checked })} />} label={text.consent} />
              <Typography variant="caption" color="text.secondary" sx={{ pl: 4 }}>{text.consentDetail} ({text.policyVersion} {data.policyVersion})</Typography>
              {requires?.nda && <FormControlLabel control={<Checkbox checked={draft.nda} onChange={(e) => setDraft({ ...draft, nda: e.target.checked })} />} label={text.nda} />}
              {requires?.safetyBriefing && <FormControlLabel control={<Checkbox checked={draft.safetyBriefing} onChange={(e) => setDraft({ ...draft, safetyBriefing: e.target.checked })} />} label={text.safety} />}
            </Stack>
            <Button fullWidth size="large" variant="contained" sx={{ mt: 3 }} disabled={busy || !ready} onClick={() => void submit()}>
              {busy ? text.submitting : text.submit}
            </Button>
          </>}
        </>}
      </CardContent>
    </Card>
  </Box>;
}
