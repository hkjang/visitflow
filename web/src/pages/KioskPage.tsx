import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Box, Button, Card, CardContent, Chip, CircularProgress, Paper, Stack, TextField, Typography } from "@mui/material";
import CheckCircleRounded from "@mui/icons-material/CheckCircleRounded";
import ErrorOutlineRounded from "@mui/icons-material/ErrorOutlineRounded";
import QrCodeScannerRounded from "@mui/icons-material/QrCodeScannerRounded";
import VideocamRounded from "@mui/icons-material/VideocamRounded";
import StopCircleOutlined from "@mui/icons-material/StopCircleOutlined";
import { useSearchParams } from "react-router-dom";
import { api, postJSON, setKioskCSRF } from "../api";
import { Logo } from "../components/Logo";

type VerifyResult = { visitor: string; company?: string; host: string; department?: string; site: string; lobby?: string; startAt: string; endAt: string };
type BarcodeDetectorLike = { detect: (source: HTMLVideoElement) => Promise<{ rawValue: string }[]> };

const KIOSK_CSRF_COOKIE = "visitflow_kiosk_csrf";

function readKioskCSRF(): string {
  const match = document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith(`${KIOSK_CSRF_COOKIE}=`));
  return match ? decodeURIComponent(match.slice(KIOSK_CSRF_COOKIE.length + 1)) : "";
}

// KioskPage is the unattended lobby tablet. It never signs a person in: the
// device enrols once with an administrator-issued token and then works entirely
// through the lobby endpoints, so the tablet cannot reach personal or admin data.
export function KioskPage() {
  const [params, setParams] = useSearchParams();
  const [enrolled, setEnrolled] = useState(() => Boolean(readKioskCSRF()));
  const [device, setDevice] = useState("");
  const [token, setToken] = useState("");
  const [status, setStatus] = useState<{ tone: "success" | "error"; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [camera, setCamera] = useState(false);
  const [counts, setCounts] = useState<Record<string, number>>({});
  const videoRef = useRef<HTMLVideoElement>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const timerRef = useRef<number | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const enroll = useCallback(async (value: string) => {
    setBusy(true);
    try {
      const result = await postJSON<{ device: { name: string }; csrfToken: string }>("/api/v1/kiosk/enroll", { token: value.trim() });
      setKioskCSRF(result.csrfToken);
      setDevice(result.device.name);
      setEnrolled(true);
      setStatus({ tone: "success", text: `${result.device.name} 기기로 등록했습니다.` });
    } catch (e) {
      setStatus({ tone: "error", text: e instanceof Error ? e.message : "키오스크를 등록하지 못했습니다" });
    } finally { setBusy(false); }
  }, []);

  useEffect(() => {
    const queryToken = params.get("token");
    if (queryToken) {
      void enroll(queryToken).finally(() => {
        const next = new URLSearchParams(params);
        next.delete("token");
        setParams(next, { replace: true });
      });
      return;
    }
    const csrf = readKioskCSRF();
    if (csrf) setKioskCSRF(csrf);
  }, [enroll, params, setParams]);

  const refresh = useCallback(async () => {
    if (!enrolled) return;
    try {
      const data = await api<{ counts: Record<string, number> }>("/api/v1/lobby/today");
      setCounts(data.counts);
    } catch { /* the tablet keeps scanning even if the summary fails */ }
  }, [enrolled]);
  useEffect(() => { void refresh(); }, [refresh]);

  const stopCamera = useCallback(() => {
    if (timerRef.current != null) window.clearInterval(timerRef.current);
    timerRef.current = null;
    streamRef.current?.getTracks().forEach((track) => track.stop());
    streamRef.current = null;
    setCamera(false);
  }, []);
  useEffect(() => () => stopCamera(), [stopCamera]);

  const checkIn = useCallback(async (value: string) => {
    if (!value.trim() || busy) return;
    setBusy(true);
    try {
      const verified = await postJSON<VerifyResult>("/api/v1/qr/verify", { token: value.trim() });
      await postJSON("/api/v1/checkins", { token: value.trim(), method: "kiosk" });
      setStatus({ tone: "success", text: `${verified.visitor} 님, 체크인되었습니다. ${verified.host} 담당자에게 안내 중입니다.` });
      void refresh();
    } catch (e) {
      setStatus({ tone: "error", text: e instanceof Error ? e.message : "체크인하지 못했습니다" });
    } finally {
      setBusy(false);
      setToken("");
      inputRef.current?.focus();
    }
  }, [busy, refresh]);

  const startCamera = async () => {
    const Detector = (window as unknown as { BarcodeDetector?: new (options: { formats: string[] }) => BarcodeDetectorLike }).BarcodeDetector;
    if (!Detector) {
      setStatus({ tone: "error", text: "이 브라우저는 카메라 QR 판독을 지원하지 않습니다. USB 스캐너를 사용하세요." });
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: "environment" } }, audio: false });
      streamRef.current = stream;
      if (videoRef.current) { videoRef.current.srcObject = stream; await videoRef.current.play(); }
      setCamera(true);
      const detector = new Detector({ formats: ["qr_code"] });
      timerRef.current = window.setInterval(async () => {
        if (!videoRef.current) return;
        try {
          const found = await detector.detect(videoRef.current);
          if (found[0]?.rawValue) await checkIn(found[0].rawValue);
        } catch { /* transient camera frame */ }
      }, 450);
    } catch (e) {
      setStatus({ tone: "error", text: e instanceof Error ? e.message : "카메라를 시작하지 못했습니다" });
    }
  };

  if (!enrolled) {
    return <Box sx={{ minHeight: "100vh", display: "grid", placeItems: "center", bgcolor: "#EAF1EE", p: 2 }}>
      <Card sx={{ width: "100%", maxWidth: 460, borderRadius: 4 }}><CardContent sx={{ p: 4 }}>
        <Logo />
        <Typography variant="h5" sx={{ mt: 3 }}>키오스크 등록</Typography>
        <Typography color="text.secondary" sx={{ mt: 1, mb: 3 }}>관리자 화면에서 발급한 기기 토큰을 입력하면 이 태블릿이 로비 전용 모드로 전환됩니다.</Typography>
        {status && <Alert severity={status.tone} sx={{ mb: 2 }}>{status.text}</Alert>}
        <TextField fullWidth autoFocus label="기기 토큰" value={token} onChange={(e) => setToken(e.target.value)} placeholder="vfk_…" />
        <Button fullWidth size="large" variant="contained" sx={{ mt: 2 }} disabled={busy || !token.trim()} onClick={() => void enroll(token)}>등록</Button>
      </CardContent></Card>
    </Box>;
  }

  return <Box sx={{ minHeight: "100vh", bgcolor: "#0C473E", color: "white", p: { xs: 2, md: 5 } }}>
    <Stack direction="row" justifyContent="space-between" alignItems="center" mb={4}>
      <Logo inverse />
      <Stack direction="row" spacing={1}>
        {device && <Chip label={device} sx={{ bgcolor: "rgba(255,255,255,.14)", color: "white" }} />}
        <Chip label={`현재 방문중 ${counts.current ?? 0}명`} sx={{ bgcolor: "rgba(255,255,255,.14)", color: "white" }} />
      </Stack>
    </Stack>
    <Box sx={{ maxWidth: 780, mx: "auto", textAlign: "center" }}>
      <Typography variant="h3" sx={{ fontWeight: 850, letterSpacing: "-.03em" }}>방문증 QR을 스캔해 주세요</Typography>
      <Typography sx={{ mt: 1.5, fontSize: 19, color: "rgba(255,255,255,.75)" }}>스캐너에 QR을 비추거나 아래 입력창에 방문증 코드를 붙여 넣으세요.</Typography>
      {status && <Alert icon={status.tone === "success" ? <CheckCircleRounded /> : <ErrorOutlineRounded />} severity={status.tone} sx={{ mt: 3, textAlign: "left", fontSize: 18 }} onClose={() => setStatus(null)}>{status.text}</Alert>}
      <Paper sx={{ mt: 3, p: 2.5, borderRadius: 4 }}>
        <TextField inputRef={inputRef} autoFocus fullWidth label="QR URL 또는 Token" value={token} onChange={(e) => setToken(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void checkIn(token); } }} />
        <Stack direction={{ xs: "column", sm: "row" }} spacing={1} mt={2}>
          <Button fullWidth size="large" variant="contained" startIcon={busy ? <CircularProgress size={18} /> : <QrCodeScannerRounded />} disabled={busy || !token.trim()} onClick={() => void checkIn(token)}>체크인</Button>
          <Button fullWidth size="large" variant="outlined" startIcon={camera ? <StopCircleOutlined /> : <VideocamRounded />} onClick={() => camera ? stopCamera() : void startCamera()}>{camera ? "카메라 중지" : "카메라 시작"}</Button>
        </Stack>
        <Box sx={{ mt: 2, borderRadius: 3, overflow: "hidden", bgcolor: "#10201D", aspectRatio: "4/3", display: camera ? "block" : "none" }}>
          <video ref={videoRef} muted playsInline style={{ width: "100%", height: "100%", objectFit: "cover" }} />
        </Box>
      </Paper>
      <Typography variant="caption" sx={{ display: "block", mt: 3, color: "rgba(255,255,255,.55)" }}>
        이 기기는 로비 기능만 사용할 수 있으며, 관리자 화면에서 언제든 폐기할 수 있습니다.
      </Typography>
    </Box>
  </Box>;
}
