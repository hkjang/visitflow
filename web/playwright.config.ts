import { defineConfig, devices } from "@playwright/test";

// The end-to-end suite drives a real VisitFlow instance: the lobby flows depend
// on camera permissions, keyboard-wedge scanners and server-verified QR codes,
// none of which a component test can stand in for.
export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.VISITFLOW_BASE_URL ?? "http://127.0.0.1:8080",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    locale: "ko-KR",
    timezoneId: "Asia/Seoul",
  },
  // Unset means Playwright's own bundled browser, which is what CI installs.
  // A machine that only has a system Chrome sets the channel instead; the key
  // is omitted rather than set to undefined so the default path is untouched.
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        ...(process.env.VISITFLOW_E2E_BROWSER_CHANNEL
          ? { channel: process.env.VISITFLOW_E2E_BROWSER_CHANNEL }
          : {}),
      },
    },
  ],
});
