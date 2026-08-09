import {
  useEffect,
  useMemo,
  useState,
  type DragEvent,
  type FormEvent,
  type MouseEvent as ReactMouseEvent,
} from "react";
import {
  Alert,
  Avatar,
  Box,
  Button,
  Chip,
  CircularProgress,
  Divider,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  IconButton,
  InputAdornment,
  MenuItem,
  Paper,
  Select,
  Skeleton,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import SearchRounded from "@mui/icons-material/SearchRounded";
import ZoomInRounded from "@mui/icons-material/ZoomInRounded";
import ZoomOutRounded from "@mui/icons-material/ZoomOutRounded";
import CenterFocusStrongRounded from "@mui/icons-material/CenterFocusStrongRounded";
import PersonPinCircleRounded from "@mui/icons-material/PersonPinCircleRounded";
import ApartmentRounded from "@mui/icons-material/ApartmentRounded";
import AddRounded from "@mui/icons-material/AddRounded";
import EditRounded from "@mui/icons-material/EditRounded";
import DeleteOutlineRounded from "@mui/icons-material/DeleteOutlineRounded";
import { useNavigate } from "react-router-dom";
import { api, patchJSON, postJSON } from "../api";
import { useAuth } from "../auth";
import type { Building, Employee, Floor, FloorMap, Seat } from "../types";

const seatColor = (seat: Seat) =>
  seat.type === "unavailable" || seat.status === "unavailable"
    ? "#8796A1"
    : seat.employeeId
      ? "#087E8B"
      : seat.type === "shared"
        ? "#3478C8"
        : "#FFFFFF";
export function SeatMapPage() {
  const { user } = useAuth(),
    navigate = useNavigate();
  const manager =
    user?.role === "seat_manager" || user?.role === "system_admin";
  const [buildings, setBuildings] = useState<Building[]>([]),
    [floors, setFloors] = useState<Floor[]>([]),
    [maps, setMaps] = useState<FloorMap[]>([]),
    [seats, setSeats] = useState<Seat[]>([]),
    [employees, setEmployees] = useState<Employee[]>([]);
  const [buildingId, setBuildingId] = useState(""),
    [floorId, setFloorId] = useState(""),
    [mapId, setMapId] = useState(""),
    [query, setQuery] = useState(""),
    [selected, setSelected] = useState<Seat | null>(null),
    [loading, setLoading] = useState(true),
    [zoom, setZoom] = useState(1),
    [error, setError] = useState(""),
    [editor, setEditor] = useState<Partial<Seat> | null>(null);
  const loadBase = async () => {
    setLoading(true);
    try {
      const [b, f, m] = await Promise.all([
        api<{ items: Building[] }>("/api/v1/buildings"),
        api<{ items: Floor[] }>("/api/v1/floors"),
        api<{ items: FloorMap[] }>("/api/v1/floor-maps"),
      ]);
      setBuildings(b.items);
      setFloors(f.items);
      setMaps(m.items);
      const bid = buildingId || b.items[0]?.id || "";
      setBuildingId(bid);
      const fid =
        floorId || f.items.find((x) => x.buildingId === bid)?.id || "";
      setFloorId(fid);
      const mid =
        m.items.find((x) => x.floorId === fid && x.active)?.id ||
        m.items.find((x) => x.floorId === fid)?.id ||
        "";
      setMapId(mid);
      if (mid) {
        const data = await api<{ items: Seat[] }>(
          `/api/v1/seats?floorMapId=${mid}`,
        );
        setSeats(data.items);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "데이터를 불러오지 못했습니다");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    void loadBase();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const buildingFloors = useMemo(
    () => floors.filter((f) => f.buildingId === buildingId),
    [floors, buildingId],
  );
  const floorMaps = useMemo(
    () => maps.filter((m) => m.floorId === floorId),
    [maps, floorId],
  );
  const currentMap = maps.find((m) => m.id === mapId);
  const chooseBuilding = (id: string) => {
    setBuildingId(id);
    const fid = floors.find((f) => f.buildingId === id)?.id || "";
    chooseFloor(fid);
  };
  const chooseFloor = (id: string) => {
    setFloorId(id);
    const mid =
      maps.find((m) => m.floorId === id && m.active)?.id ||
      maps.find((m) => m.floorId === id)?.id ||
      "";
    void chooseMap(mid);
  };
  const chooseMap = async (id: string) => {
    setMapId(id);
    setSelected(null);
    if (!id) {
      setSeats([]);
      return;
    }
    try {
      const data = await api<{ items: Seat[] }>(
        `/api/v1/seats?floorMapId=${id}`,
      );
      setSeats(data.items);
    } catch (e) {
      setError(e instanceof Error ? e.message : "좌석을 불러오지 못했습니다");
    }
  };
  const search = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!query.trim()) {
      setEmployees([]);
      return;
    }
    try {
      const data = await api<{ items: Employee[] }>(
        `/api/v1/employees?q=${encodeURIComponent(query)}&limit=30`,
      );
      setEmployees(data.items);
      const first = data.items.find((x) => x.seatId);
      if (first?.seatId) {
        const found = seats.find((s) => s.id === first.seatId);
        if (found) setSelected(found);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "검색하지 못했습니다");
    }
  };
  const drop = async (event: DragEvent, seat: Seat) => {
    event.preventDefault();
    if (!manager) return;
    const employeeId = event.dataTransfer.getData(
      "application/seaton-employee",
    );
    if (!employeeId) return;
    try {
      await postJSON("/api/v1/seat-assignments", {
        employeeId,
        seatId: seat.id,
        reason: "좌석맵 Drag & Drop",
        source: "manual",
      });
      await chooseMap(mapId);
    } catch (e) {
      setError(e instanceof Error ? e.message : "좌석을 배정하지 못했습니다");
    }
  };
  const openNewSeat = (x = 0.45, y = 0.45) =>
    setEditor({
      floorMapId: mapId,
      seatNo: `NEW-${String(seats.length + 1).padStart(3, "0")}`,
      type: "fixed",
      status: "available",
      x: Math.max(0, Math.min(0.94, x)),
      y: Math.max(0, Math.min(0.92, y)),
      width: 0.04,
      height: 0.055,
      rotation: 0,
    });
  const mapDoubleClick = (event: ReactMouseEvent<SVGSVGElement>) => {
    if (!manager) return;
    const bounds = event.currentTarget.getBoundingClientRect();
    openNewSeat(
      (event.clientX - bounds.left) / bounds.width,
      (event.clientY - bounds.top) / bounds.height,
    );
  };
  const saveSeat = async () => {
    if (!editor) return;
    try {
      const body = {
        seatNo: editor.seatNo,
        type: editor.type,
        status: editor.status,
        x: Number(editor.x),
        y: Number(editor.y),
        width: Number(editor.width),
        height: Number(editor.height),
        rotation: Number(editor.rotation),
      };
      if (editor.id) await patchJSON(`/api/v1/seats/${editor.id}`, body);
      else await postJSON("/api/v1/seats", { ...body, floorMapId: mapId });
      setEditor(null);
      await chooseMap(mapId);
    } catch (e) {
      setError(e instanceof Error ? e.message : "좌석을 저장하지 못했습니다");
    }
  };
  const removeSeat = async () => {
    if (!selected || selected.employeeId) return;
    if (!confirm(`${selected.seatNo} 좌석을 삭제할까요?`)) return;
    try {
      await api(`/api/v1/seats/${selected.id}`, { method: "DELETE" });
      setSelected(null);
      await chooseMap(mapId);
    } catch (e) {
      setError(e instanceof Error ? e.message : "좌석을 삭제하지 못했습니다");
    }
  };
  const selectedEmployee = selected?.employeeId
    ? employees.find((e) => e.id === selected.employeeId)
    : undefined;
  if (loading)
    return (
      <Box sx={{ p: { xs: 2, md: 3 } }}>
        <Skeleton height={60} />
        <Skeleton variant="rounded" height="70vh" />
      </Box>
    );
  return (
    <Box
      sx={{
        p: { xs: 2, md: 3 },
        height: { md: "calc(100vh - 72px)" },
        display: "flex",
        flexDirection: "column",
        gap: 2,
      }}
    >
      {error && (
        <Alert severity="error" onClose={() => setError("")}>
          {error}
        </Alert>
      )}
      <Box
        sx={{
          display: "flex",
          alignItems: { xs: "stretch", md: "center" },
          gap: 1.5,
          flexDirection: { xs: "column", md: "row" },
        }}
      >
        <Box>
          <Typography variant="h5">좌석맵</Typography>
          <Typography variant="body2" color="text.secondary">
            사람과 조직의 현재 위치를 한눈에 확인하세요.
          </Typography>
        </Box>
        <Box sx={{ flex: 1 }} />
        {manager && currentMap && (
          <Button
            variant="outlined"
            startIcon={<AddRounded />}
            onClick={() => openNewSeat()}
          >
            좌석 추가
          </Button>
        )}
        <Stack direction="row" spacing={1} sx={{ overflowX: "auto" }}>
          <FormControl sx={{ minWidth: 145 }}>
            <Select
              value={buildingId}
              displayEmpty
              onChange={(e) => chooseBuilding(e.target.value)}
            >
              {buildings.length ? (
                buildings.map((x) => (
                  <MenuItem key={x.id} value={x.id}>
                    {x.name}
                  </MenuItem>
                ))
              ) : (
                <MenuItem value="">사업장 없음</MenuItem>
              )}
            </Select>
          </FormControl>
          <FormControl sx={{ minWidth: 120 }}>
            <Select
              value={floorId}
              displayEmpty
              onChange={(e) => chooseFloor(e.target.value)}
            >
              {buildingFloors.map((x) => (
                <MenuItem key={x.id} value={x.id}>
                  {x.name}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
          {floorMaps.length > 1 && (
            <FormControl sx={{ minWidth: 110 }}>
              <Select
                value={mapId}
                onChange={(e) => void chooseMap(e.target.value)}
              >
                {floorMaps.map((x) => (
                  <MenuItem key={x.id} value={x.id}>
                    V{x.version}
                    {x.active ? " · 게시" : ""}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          )}
        </Stack>
      </Box>
      {!currentMap ? (
        <Paper
          sx={{
            flex: 1,
            minHeight: 420,
            display: "grid",
            placeItems: "center",
            borderStyle: "dashed",
            boxShadow: "none",
          }}
        >
          <Stack alignItems="center" spacing={2}>
            <Box
              sx={{
                width: 64,
                height: 64,
                borderRadius: "50%",
                display: "grid",
                placeItems: "center",
                bgcolor: "rgba(8,126,139,.09)",
                color: "primary.main",
              }}
            >
              <ApartmentRounded fontSize="large" />
            </Box>
            <Box textAlign="center">
              <Typography variant="h6">표시할 좌석맵이 없습니다</Typography>
              <Typography color="text.secondary">
                관리자가 사업장과 도면을 등록하면 이곳에서 바로 찾을 수 있어요.
              </Typography>
            </Box>
            {manager && (
              <Button
                variant="contained"
                startIcon={<AddRounded />}
                onClick={() => navigate("/admin/maps")}
              >
                첫 도면 등록
              </Button>
            )}
          </Stack>
        </Paper>
      ) : (
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: {
              xs: "1fr",
              lg: "260px minmax(500px,1fr) 270px",
            },
            gap: 2,
            flex: 1,
            minHeight: 0,
          }}
        >
          <Paper
            sx={{
              p: 2,
              display: "flex",
              flexDirection: "column",
              minHeight: { xs: 240, lg: 0 },
              overflow: "hidden",
            }}
          >
            <Box component="form" onSubmit={search}>
              <TextField
                fullWidth
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="이름, 사번, 조직 검색"
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
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ mt: 1.5, mb: 1 }}
            >
              {employees.length
                ? `${employees.length}명 검색됨`
                : "사람 또는 조직을 검색하세요"}
            </Typography>
            <Stack spacing={0.75} sx={{ overflowY: "auto" }}>
              {employees.map((e) => (
                <Box
                  key={e.id}
                  draggable={manager}
                  onDragStart={(event) =>
                    event.dataTransfer.setData(
                      "application/seaton-employee",
                      e.id,
                    )
                  }
                  onClick={() => {
                    const seat = seats.find((s) => s.id === e.seatId);
                    if (seat) setSelected(seat);
                  }}
                  sx={{
                    display: "flex",
                    gap: 1.2,
                    p: 1,
                    borderRadius: 2,
                    cursor: e.seatId ? "pointer" : "default",
                    "&:hover": { bgcolor: "#F1F6F7" },
                  }}
                >
                  <Avatar
                    sx={{
                      width: 34,
                      height: 34,
                      fontSize: 13,
                      bgcolor: e.seatId ? "primary.main" : "grey.400",
                    }}
                  >
                    {e.name.slice(0, 1)}
                  </Avatar>
                  <Box minWidth={0}>
                    <Typography variant="body2" fontWeight={700} noWrap>
                      {e.name}{" "}
                      <Typography
                        component="span"
                        variant="caption"
                        color="text.secondary"
                      >
                        {e.employeeNo}
                      </Typography>
                    </Typography>
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      noWrap
                      display="block"
                    >
                      {e.organizationName || "소속 없음"} ·{" "}
                      {e.seatNo || "미배정"}
                    </Typography>
                  </Box>
                </Box>
              ))}
            </Stack>
          </Paper>
          <Paper
            sx={{
              position: "relative",
              overflow: "hidden",
              minHeight: { xs: 430, lg: 0 },
              bgcolor: "#E9EFF2",
            }}
          >
            <Box
              sx={{
                position: "absolute",
                top: 12,
                left: 12,
                zIndex: 2,
                display: "flex",
                gap: 0.5,
                p: 0.5,
                bgcolor: "rgba(255,255,255,.92)",
                borderRadius: 2,
                boxShadow: 2,
              }}
            >
              <Tooltip title="축소">
                <IconButton
                  size="small"
                  onClick={() => setZoom((z) => Math.max(0.7, z - 0.2))}
                >
                  <ZoomOutRounded />
                </IconButton>
              </Tooltip>
              <Tooltip title="맞춤">
                <IconButton size="small" onClick={() => setZoom(1)}>
                  <CenterFocusStrongRounded />
                </IconButton>
              </Tooltip>
              <Tooltip title="확대">
                <IconButton
                  size="small"
                  onClick={() => setZoom((z) => Math.min(2.2, z + 0.2))}
                >
                  <ZoomInRounded />
                </IconButton>
              </Tooltip>
            </Box>
            <Box
              sx={{
                width: "100%",
                height: "100%",
                overflow: "auto",
                display: "grid",
                placeItems: "center",
                p: 2,
              }}
            >
              {currentMap.contentType === "application/pdf" ? (
                <Box
                  sx={{ width: "100%", height: "100%", position: "relative" }}
                >
                  <object
                    data={currentMap.contentUrl}
                    type="application/pdf"
                    width="100%"
                    height="100%"
                    aria-label="PDF 도면"
                  />
                  <Typography
                    variant="caption"
                    sx={{
                      position: "absolute",
                      bottom: 8,
                      left: 8,
                      bgcolor: "white",
                      px: 1,
                    }}
                  >
                    PDF 도면의 좌석 오버레이는 이미지 변환 분석 후 표시됩니다.
                  </Typography>
                </Box>
              ) : (
                <svg
                  role="img"
                  aria-label={`${currentMap.floorName} 좌석 배치도`}
                  viewBox="0 0 1000 700"
                  onDoubleClick={mapDoubleClick}
                  style={{
                    width: `${zoom * 100}%`,
                    height: `${zoom * 100}%`,
                    minWidth: 720,
                    minHeight: 500,
                    background: "#fff",
                    borderRadius: 12,
                    boxShadow: "0 8px 24px rgba(14,45,62,.1)",
                  }}
                >
                  <image
                    href={currentMap.contentUrl}
                    x="0"
                    y="0"
                    width="1000"
                    height="700"
                    preserveAspectRatio="xMidYMid meet"
                    opacity=".9"
                  />
                  {seats.map((seat) => (
                    <g
                      key={seat.id}
                      transform={`rotate(${seat.rotation} ${seat.x * 1000 + seat.width * 500} ${seat.y * 700 + seat.height * 350})`}
                      onClick={() => setSelected(seat)}
                      onDoubleClick={(event) => {
                        event.stopPropagation();
                        if (manager) setEditor(seat);
                      }}
                      onDragOver={(e) => manager && e.preventDefault()}
                      onDrop={(e) => void drop(e, seat)}
                      style={{ cursor: "pointer" }}
                    >
                      <rect
                        x={seat.x * 1000}
                        y={seat.y * 700}
                        width={seat.width * 1000}
                        height={seat.height * 700}
                        rx="6"
                        fill={seatColor(seat)}
                        stroke={
                          selected?.id === seat.id
                            ? "#FFB703"
                            : seat.confidence && seat.confidence < 0.95
                              ? "#E79418"
                              : "#263E4D"
                        }
                        strokeWidth={selected?.id === seat.id ? 5 : 2}
                      />
                      <text
                        x={(seat.x + seat.width / 2) * 1000}
                        y={(seat.y + seat.height / 2) * 700}
                        textAnchor="middle"
                        dominantBaseline="middle"
                        fontSize="11"
                        fontWeight="700"
                        fill={seat.employeeId ? "white" : "#203846"}
                        style={{ pointerEvents: "none" }}
                      >
                        {seat.employeeName || seat.seatNo}
                      </text>
                    </g>
                  ))}
                </svg>
              )}
            </Box>
            <Box
              sx={{
                position: "absolute",
                right: 12,
                bottom: 12,
                display: "flex",
                gap: 0.75,
                bgcolor: "rgba(255,255,255,.92)",
                borderRadius: 2,
                p: 1,
              }}
            >
              {[
                ["#087E8B", "배정"],
                ["#FFF", "빈 좌석"],
                ["#3478C8", "공용"],
                ["#8796A1", "사용불가"],
              ].map(([color, label]) => (
                <Stack
                  key={label}
                  direction="row"
                  spacing={0.5}
                  alignItems="center"
                >
                  <Box
                    sx={{
                      width: 10,
                      height: 10,
                      borderRadius: 0.5,
                      bgcolor: color,
                      border: "1px solid #8796A1",
                    }}
                  />
                  <Typography variant="caption">{label}</Typography>
                </Stack>
              ))}
            </Box>
          </Paper>
          <Paper sx={{ p: 2.5, minHeight: { xs: 220, lg: 0 } }}>
            {selected ? (
              <>
                <Stack
                  direction="row"
                  justifyContent="space-between"
                  alignItems="center"
                >
                  <Chip
                    label={
                      selected.type === "shared"
                        ? "공용 좌석"
                        : selected.status === "unavailable"
                          ? "사용 불가"
                          : selected.employeeId
                            ? "배정됨"
                            : "빈 좌석"
                    }
                    size="small"
                    color={selected.employeeId ? "primary" : "default"}
                  />
                  {selected.confidence != null && (
                    <Typography
                      variant="caption"
                      color={
                        selected.confidence < 0.95
                          ? "warning.main"
                          : "text.secondary"
                      }
                    >
                      AI {Math.round(selected.confidence * 100)}%
                    </Typography>
                  )}
                </Stack>
                <Typography variant="h5" sx={{ mt: 2 }}>
                  {selected.seatNo}
                </Typography>
                <Divider sx={{ my: 2 }} />
                {manager && (
                  <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
                    <Button
                      size="small"
                      variant="outlined"
                      startIcon={<EditRounded />}
                      onClick={() => setEditor(selected)}
                    >
                      편집
                    </Button>
                    <Button
                      size="small"
                      color="error"
                      startIcon={<DeleteOutlineRounded />}
                      disabled={Boolean(selected.employeeId)}
                      onClick={() => void removeSeat()}
                    >
                      삭제
                    </Button>
                  </Stack>
                )}
                {selected.employeeId ? (
                  <Stack spacing={1.4}>
                    <Stack direction="row" spacing={1.2} alignItems="center">
                      <Avatar sx={{ bgcolor: "primary.main" }}>
                        {selected.employeeName?.slice(0, 1) ?? "?"}
                      </Avatar>
                      <Box>
                        <Typography fontWeight={750}>
                          {selected.employeeName}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {selected.employeeNo}
                        </Typography>
                      </Box>
                    </Stack>
                    <Info
                      label="조직"
                      value={
                        selected.organizationName ||
                        selectedEmployee?.organizationName ||
                        "정보 없음"
                      }
                    />
                    <Info
                      label="근무지"
                      value={
                        selectedEmployee?.workplace || currentMap.buildingName
                      }
                    />
                  </Stack>
                ) : (
                  <Stack alignItems="center" spacing={1.2} sx={{ pt: 2 }}>
                    <PersonPinCircleRounded
                      sx={{ fontSize: 42, color: "text.disabled" }}
                    />
                    <Typography color="text.secondary">
                      배정된 직원이 없습니다.
                    </Typography>
                    {manager && (
                      <Typography variant="caption" textAlign="center">
                        왼쪽 검색 결과의 직원을
                        <br />이 좌석으로 끌어 놓으세요.
                      </Typography>
                    )}
                  </Stack>
                )}
              </>
            ) : (
              <Stack
                sx={{ height: "100%" }}
                alignItems="center"
                justifyContent="center"
                spacing={1}
              >
                <CenterFocusStrongRounded
                  sx={{ fontSize: 40, color: "text.disabled" }}
                />
                <Typography color="text.secondary" textAlign="center">
                  좌석을 선택하면
                  <br />
                  상세 정보가 표시됩니다.
                </Typography>
              </Stack>
            )}
          </Paper>
        </Box>
      )}
      <SeatEditor
        value={editor}
        onChange={setEditor}
        onClose={() => setEditor(null)}
        onSave={() => void saveSeat()}
      />
    </Box>
  );
}
function SeatEditor({
  value,
  onChange,
  onClose,
  onSave,
}: {
  value: Partial<Seat> | null;
  onChange: (value: Partial<Seat> | null) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const number = (key: keyof Seat, raw: string) =>
    value && onChange({ ...value, [key]: Number(raw) });
  return (
    <Dialog open={Boolean(value)} onClose={onClose} fullWidth maxWidth="sm">
      <DialogTitle>{value?.id ? "좌석 보정" : "좌석 추가"}</DialogTitle>
      <DialogContent>
        {value && (
          <Stack spacing={2} sx={{ mt: 1 }}>
            <Stack direction={{ xs: "column", sm: "row" }} spacing={2}>
              <TextField
                fullWidth
                label="좌석 번호"
                value={value.seatNo ?? ""}
                onChange={(event) =>
                  onChange({ ...value, seatNo: event.target.value })
                }
              />
              <FormControl fullWidth>
                <Select
                  value={value.type ?? "fixed"}
                  onChange={(event) =>
                    onChange({ ...value, type: event.target.value })
                  }
                >
                  <MenuItem value="fixed">고정 좌석</MenuItem>
                  <MenuItem value="shared">공용 좌석</MenuItem>
                  <MenuItem value="unavailable">사용 불가</MenuItem>
                  <MenuItem value="meeting_room">회의실</MenuItem>
                  <MenuItem value="executive">임원실</MenuItem>
                  <MenuItem value="utility">기타 공간</MenuItem>
                </Select>
              </FormControl>
            </Stack>
            <Alert severity="info">
              좌표와 크기는 도면 대비 0~1 비율입니다. 도면을 더블 클릭하면 해당
              위치로 새 좌석이 만들어집니다.
            </Alert>
            <Stack direction={{ xs: "column", sm: "row" }} spacing={2}>
              {(["x", "y", "width", "height"] as const).map((key) => (
                <TextField
                  key={key}
                  label={key}
                  type="number"
                  value={value[key] ?? 0}
                  onChange={(event) => number(key, event.target.value)}
                  slotProps={{ htmlInput: { min: 0, max: 1, step: 0.005 } }}
                />
              ))}
            </Stack>
            <TextField
              label="회전 각도"
              type="number"
              value={value.rotation ?? 0}
              onChange={(event) => number("rotation", event.target.value)}
              slotProps={{ htmlInput: { min: -360, max: 360, step: 5 } }}
            />
          </Stack>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>취소</Button>
        <Button
          variant="contained"
          disabled={!value?.seatNo || !value.width || !value.height}
          onClick={onSave}
        >
          저장
        </Button>
      </DialogActions>
    </Dialog>
  );
}
function Info({ label, value }: { label: string; value: string }) {
  return (
    <Box>
      <Typography variant="caption" color="text.secondary">
        {label}
      </Typography>
      <Typography variant="body2" fontWeight={650}>
        {value}
      </Typography>
    </Box>
  );
}
