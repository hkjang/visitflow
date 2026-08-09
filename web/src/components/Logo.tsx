import { Box, Typography } from "@mui/material";

export function Logo({ compact = false, inverse = false }: { compact?: boolean; inverse?: boolean }) {
  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 1.15 }}>
      <Box sx={{ width: 38, height: 38, borderRadius: "12px 12px 12px 4px", bgcolor: inverse ? "#fff" : "primary.main", color: inverse ? "primary.main" : "#fff", display: "grid", placeItems: "center", fontWeight: 900, fontSize: 18, boxShadow: inverse ? "none" : "0 8px 20px rgba(23,107,91,.22)" }}>VF</Box>
      {!compact && <Box><Typography sx={{ fontWeight: 850, fontSize: 20, lineHeight: 1, color: inverse ? "#fff" : "text.primary", letterSpacing: "-.035em" }}>VisitFlow</Typography><Typography variant="caption" sx={{ color: inverse ? "rgba(255,255,255,.72)" : "text.secondary", letterSpacing: ".08em" }}>VISITOR OPERATIONS</Typography></Box>}
    </Box>
  );
}
