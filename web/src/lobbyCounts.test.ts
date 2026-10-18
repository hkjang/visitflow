import { describe, expect, it } from "vitest";
import { lobbyPanel, lobbyView, type LobbyToday } from "./lobbyCounts";
import type { LobbyVisitor } from "./types";

const visitor: LobbyVisitor = {
  visitorVisitId: "vv-1", visitId: "v-1", visitor: "홍길동",
  host: "담당자", site: "본사", startAt: "2026-10-18T01:00:00Z",
  endAt: "2026-10-18T03:00:00Z", status: "CHECKED_IN",
};

const zeroed: LobbyToday = {
  counts: { scheduled: 0, current: 0, completed: 0, noShow: 0 },
  items: [], truncated: false, limit: 300,
};

const busy: LobbyToday = {
  counts: { scheduled: 4, current: 2, completed: 7, noShow: 1 },
  items: [visitor],
};

const TODAY = lobbyView("today", "", "");
const CURRENT = lobbyView("current", "", "");
const AT = "2026-10-18T01:34:00Z"; // 10:34 Asia/Seoul, but the test never assumes a zone.

function stamp(iso: string) {
  return new Date(iso).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" });
}

describe("lobbyPanel", () => {
  it("says nothing numeric while the first lookup is still running", () => {
    const panel = lobbyPanel({ state: "loading" }, TODAY);
    expect(panel.summary.kind).toBe("notice");
    if (panel.summary.kind !== "notice") throw new Error("unreachable");
    expect(panel.summary.severity).toBe("info");
    expect(panel.summary.message).not.toMatch(/\d/);
    expect(panel.currentTabLabel).toBe("현재 방문자");
    expect(panel.emptyText).not.toContain("표시할 방문자가 없습니다");
    expect(panel.truncationNote).toBe("");
  });

  it("refuses to state a single count when the first lookup failed", () => {
    const panel = lobbyPanel({ state: "failed", message: "", last: null }, TODAY);
    expect(panel.summary.kind).toBe("notice");
    if (panel.summary.kind !== "notice") throw new Error("unreachable");
    expect(panel.summary.severity).toBe("warning");
    expect(panel.summary.message).toContain("로비 현황을 불러오지 못했습니다");
    // The whole defect was a dead lookup reading as "nobody is in the building".
    expect(panel.summary.message).not.toMatch(/\d/);
    expect(panel.currentTabLabel).toBe("현재 방문자");
    expect(panel.items).toEqual([]);
    expect(panel.emptyText).not.toContain("표시할 방문자가 없습니다");
    expect(panel.truncationNote).toBe("");
  });

  // Network failures surface the browser's English "Failed to fetch", so the
  // Korean sentence comes first and the cause follows in parentheses.
  it("puts the cause after the Korean sentence", () => {
    const panel = lobbyPanel({ state: "failed", message: "Failed to fetch", last: null }, TODAY);
    if (panel.summary.kind !== "notice") throw new Error("unreachable");
    expect(panel.summary.message.startsWith("로비 현황을 불러오지 못했습니다")).toBe(true);
    expect(panel.summary.message.endsWith("(Failed to fetch)")).toBe(true);
  });

  it("keeps the counts it did receive but marks them as the last successful lookup", () => {
    const panel = lobbyPanel({ state: "failed", message: "", last: { value: busy, at: AT, view: TODAY } }, TODAY);
    expect(panel.summary.kind).toBe("counts");
    if (panel.summary.kind !== "counts") throw new Error("unreachable");
    expect(panel.summary.counts).toEqual({ scheduled: 4, current: 2, completed: 7, noShow: 1 });
    expect(panel.summary.staleNote).toContain("최신이 아닐 수 있습니다");
    expect(panel.summary.staleNote).toContain(stamp(AT));
    expect(panel.items).toEqual([visitor]);
    expect(panel.currentTabLabel).toBe("현재 방문자 2");
    // The cap belongs to the response that is actually on screen; a failed
    // refetch must not add a second claim on top of the stale one.
    expect(panel.truncationNote).toBe("");
  });

  // The counts come from a site-scope query that ignores the tab, the search
  // box and the lobby filter, so they stay true across views. The list does
  // not: showing today's schedule under 「현재 방문자」 is the same lie again.
  it("drops a cached list that belongs to a different view but keeps the counts", () => {
    const panel = lobbyPanel({ state: "failed", message: "", last: { value: busy, at: AT, view: TODAY } }, CURRENT);
    if (panel.summary.kind !== "counts") throw new Error("unreachable");
    expect(panel.summary.counts.current).toBe(2);
    expect(panel.items).toEqual([]);
    expect(panel.emptyText).not.toContain("표시할 방문자가 없습니다");
    expect(panel.emptyText).toContain("불러오지 못");
  });

  it("does not call a stale empty list an empty lobby", () => {
    const panel = lobbyPanel({ state: "failed", message: "", last: { value: zeroed, at: AT, view: TODAY } }, TODAY);
    expect(panel.items).toEqual([]);
    expect(panel.emptyText).toContain(stamp(AT));
    expect(panel.emptyText).not.toBe("표시할 방문자가 없습니다.");
  });

  it("omits the time when the recorded timestamp is unusable", () => {
    const panel = lobbyPanel({ state: "failed", message: "", last: { value: busy, at: "", view: TODAY } }, TODAY);
    if (panel.summary.kind !== "counts") throw new Error("unreachable");
    expect(panel.summary.staleNote).toContain("최신이 아닐 수 있습니다");
    expect(panel.summary.staleNote).not.toContain("Invalid Date");
  });

  // The guard against a false positive: a new or quiet site really does report
  // four zeros, and that must look nothing like a failure.
  it("reports a real zero from a successful response as counts, never as a failure", () => {
    const panel = lobbyPanel({ state: "ready", value: zeroed, at: AT, view: TODAY }, TODAY);
    expect(panel.summary.kind).toBe("counts");
    if (panel.summary.kind !== "counts") throw new Error("unreachable");
    expect(panel.summary.counts).toEqual({ scheduled: 0, current: 0, completed: 0, noShow: 0 });
    expect(panel.summary.staleNote).toBeUndefined();
    expect(panel.currentTabLabel).toBe("현재 방문자 0");
    expect(panel.emptyText).toBe("표시할 방문자가 없습니다.");
    expect(panel.truncationNote).toBe("");
  });

  it("names the display cap only for a response it actually received", () => {
    const capped = lobbyPanel({ state: "ready", value: { ...busy, truncated: true, limit: 300 }, at: AT, view: TODAY }, TODAY);
    expect(capped.truncationNote).toContain("300건");
    const stale = lobbyPanel({ state: "failed", message: "", last: { value: { ...busy, truncated: true, limit: 300 }, at: AT, view: TODAY } }, TODAY);
    expect(stale.truncationNote).toBe("");
  });

  it("separates the four display states from each other", () => {
    const loading = lobbyPanel({ state: "loading" }, TODAY);
    const failed = lobbyPanel({ state: "failed", message: "", last: null }, TODAY);
    const stale = lobbyPanel({ state: "failed", message: "", last: { value: zeroed, at: AT, view: TODAY } }, TODAY);
    const ready = lobbyPanel({ state: "ready", value: zeroed, at: AT, view: TODAY }, TODAY);
    const shown = [loading, failed, stale, ready].map((p) => JSON.stringify([p.summary, p.emptyText, p.currentTabLabel]));
    expect(new Set(shown).size).toBe(4);
  });
});

describe("lobbyView", () => {
  it("separates the tab, the search text and the lobby filter", () => {
    expect(lobbyView("today", "", "")).not.toBe(lobbyView("current", "", ""));
    expect(lobbyView("today", "a", "")).not.toBe(lobbyView("today", "", "a"));
    expect(lobbyView("today", "한", "L1")).toBe(lobbyView("today", "한", "L1"));
  });
});
