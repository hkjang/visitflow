import { describe, expect, it } from "vitest";
import { metricsPanel } from "./adminMetrics";
import type { OperationalMetrics } from "./types";

const zeroed: OperationalMetrics = {
  visitsToday: 0, visitorsCurrent: 0, pendingApproval: 0,
  activeSessions: 0, activeApiKeys: 0, lockedAccounts: 0,
  notifications: {}, queueBacklog: 0, queueOldestSeconds: 0,
  schemaVersion: 7, uptimeSeconds: 12, counters: {},
};

describe("metricsPanel", () => {
  it("says nothing numeric while the first lookup is still running", () => {
    const panel = metricsPanel({ state: "loading" });
    expect(panel.kind).toBe("notice");
    if (panel.kind !== "notice") throw new Error("unreachable");
    expect(panel.severity).toBe("info");
    expect(panel.message).toBe("운영 지표를 불러오는 중입니다.");
  });

  it("reports a failed lookup without naming a single number", () => {
    const panel = metricsPanel({ state: "failed", message: "" });
    expect(panel.kind).toBe("notice");
    if (panel.kind !== "notice") throw new Error("unreachable");
    expect(panel.severity).toBe("warning");
    expect(panel.message).toContain("운영 지표를 불러오지 못했습니다");
    // The whole defect was a dead lookup reading as a healthy system.
    expect(panel.message).not.toMatch(/\d/);
  });

  // Network failures surface the browser's English "Failed to fetch", so the
  // Korean sentence comes first and the cause follows in parentheses.
  it("puts the cause after the Korean sentence", () => {
    const panel = metricsPanel({ state: "failed", message: "Failed to fetch" });
    if (panel.kind !== "notice") throw new Error("unreachable");
    expect(panel.message.endsWith("(Failed to fetch)")).toBe(true);
    expect(panel.message.startsWith("운영 지표를 불러오지 못했습니다")).toBe(true);
  });

  it("reports a real zero from a successful response as rows, never as a failure", () => {
    const panel = metricsPanel({ state: "ready", value: zeroed });
    expect(panel.kind).toBe("rows");
    if (panel.kind !== "rows") throw new Error("unreachable");
    expect(panel.rows).toEqual([
      { label: "알림 대기열", value: "0건" },
      { label: "대기열 최장 지연", value: "0분" },
      { label: "잠긴 계정·주소", value: "0건" },
      { label: "활성 세션", value: "0개" },
      { label: "활성 API 키", value: "0개" },
      { label: "스키마 버전", value: "v7" },
    ]);
  });

  it("keeps the queue delay in whole minutes", () => {
    const panel = metricsPanel({ state: "ready", value: { ...zeroed, queueOldestSeconds: 185, queueBacklog: 4 } });
    if (panel.kind !== "rows") throw new Error("unreachable");
    expect(panel.rows[0].value).toBe("4건");
    expect(panel.rows[1].value).toBe("3분");
  });
});
