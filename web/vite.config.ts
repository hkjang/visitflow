import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: { "/api": "http://localhost:8080", "/mcp": "http://localhost:8080" },
  },
  build: {
    sourcemap: false,
    target: "es2022",
    chunkSizeWarningLimit: 600,
    rollupOptions: {
      output: {
        // Vendor code changes far less often than the app, so it gets its own
        // long-lived, immutable chunk.
        manualChunks: {
          react: ["react", "react-dom", "react-router-dom"],
          mui: ["@mui/material", "@emotion/react", "@emotion/styled", "@emotion/cache"],
        },
      },
    },
  },
});
