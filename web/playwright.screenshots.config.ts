import { defineConfig } from "@playwright/test";

// Guide screenshots run against a throwaway VisitFlow instance and write PNGs
// into docs/assets/guide. They are kept out of the e2e project on purpose: the
// e2e suite must stay runnable against any instance, whereas this run seeds
// demo visits, users and posts that nobody wants in a real deployment.
export default defineConfig({
  testDir: "./screenshots",
  timeout: 120_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: process.env.VISITFLOW_SCREENSHOT_URL,
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1,
    locale: "ko-KR",
    timezoneId: "Asia/Seoul",
    // Playwright's bundled Chromium is not always installed on the machine that
    // writes the guide; VISITFLOW_SCREENSHOT_CHANNEL=chrome uses the system one.
    channel: process.env.VISITFLOW_SCREENSHOT_CHANNEL || undefined,
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
});
