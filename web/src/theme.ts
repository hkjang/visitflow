import { alpha, createTheme } from "@mui/material/styles";

export const theme = createTheme({
  palette: {
    mode: "light",
    primary: { main: "#176B5B", dark: "#0D4B40", light: "#4A9485", contrastText: "#fff" },
    secondary: { main: "#E76F51", dark: "#B94D34" },
    background: { default: "#F3F6F4", paper: "#FFFFFF" },
    text: { primary: "#17352F", secondary: "#61736E" },
    success: { main: "#2E8B68" },
    warning: { main: "#D58A20" },
    error: { main: "#C94C53" },
    info: { main: "#3978A8" },
  },
  shape: { borderRadius: 14 },
  typography: {
    fontSize: 16,
    fontFamily: 'Inter, Pretendard, "Noto Sans KR", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    h4: { fontWeight: 800, letterSpacing: "-0.035em" },
    h5: { fontWeight: 780, letterSpacing: "-0.025em" },
    h6: { fontWeight: 740 },
    body1: { fontSize: "1rem", lineHeight: 1.65 },
    body2: { fontSize: ".9375rem", lineHeight: 1.6 },
    caption: { fontSize: ".8125rem", lineHeight: 1.5 },
    overline: { fontSize: ".8125rem", lineHeight: 1.7 },
    button: { fontWeight: 730, textTransform: "none" },
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: { minWidth: 320, background: "linear-gradient(145deg,#F7F9F7 0%,#EFF4F2 100%)" },
        "*": { boxSizing: "border-box" },
        "::selection": { backgroundColor: alpha("#176B5B", 0.2) },
        code: { fontFamily: '"SFMono-Regular", Consolas, monospace' },
      },
    },
    MuiPaper: { styleOverrides: { root: { backgroundImage: "none" } } },
    MuiButton: { defaultProps: { disableElevation: true }, styleOverrides: { root: { borderRadius: 10, minHeight: 40 } } },
    MuiCard: { styleOverrides: { root: { border: "1px solid #DFE8E4", boxShadow: "0 10px 32px rgba(19,65,55,.065)" } } },
    MuiTextField: { defaultProps: { size: "small" } },
    MuiFormControl: { defaultProps: { size: "small" } },
    MuiInputBase: { styleOverrides: { root: { fontSize: 16 } } },
    MuiTableCell: { styleOverrides: { root: { fontSize: 15, lineHeight: 1.55 } } },
    MuiTableHead: { styleOverrides: { root: { backgroundColor: "#F0F5F3" } } },
  },
});
