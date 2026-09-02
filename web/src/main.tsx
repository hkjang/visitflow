import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { CssBaseline, ThemeProvider } from "@mui/material";
import { CacheProvider } from "@emotion/react";
import createCache from "@emotion/cache";
import { AuthProvider } from "./auth";
import { theme } from "./theme";
import App from "./App";

// The server serves index.html with a per-request CSP nonce. Handing it to the
// Emotion cache lets every stylesheet Material UI injects carry the nonce, so
// the policy can drop the blanket 'unsafe-inline' for style elements.
const nonce = document.querySelector<HTMLMetaElement>('meta[property="csp-nonce"]')?.content;
const cache = createCache({ key: "vf", nonce, prepend: true });

// The evacuation roster has to work when the network does not, so the app
// registers a service worker that keeps the last roster available offline.
if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("/sw.js").catch(() => {
      /* offline caching is a bonus; the app works without it */
    });
  });
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <CacheProvider value={cache}>
      <ThemeProvider theme={theme}>
        <CssBaseline />
        <BrowserRouter>
          <AuthProvider>
            <App />
          </AuthProvider>
        </BrowserRouter>
      </ThemeProvider>
    </CacheProvider>
  </StrictMode>,
);
