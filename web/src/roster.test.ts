import { describe, expect, it } from "vitest";
import { rosterStatus, type Roster } from "./roster";

const roster = (over: Partial<Roster> = {}): Roster => ({
  generatedAt: "2026-10-05T01:02:03.000Z",
  count: 2,
  items: [
    { site: "본사", visitor: "홍길동", host: "김담당" },
    { site: "본사", visitor: "이방문", host: "김담당" },
  ],
  ...over,
});

describe("rosterStatus", () => {
  it("reports a fresh response without any warning", () => {
    const status = rosterStatus({ roster: roster(), failed: false, errorMessage: "" });
    expect(status.source).toBe("live");
    expect(status.warning).toBe("");
    expect(status.countLabel).toBe("총 2명");
    expect(status.asOf).toBe("2026-10-05T01:02:03.000Z");
  });

  // 실제로 아무도 없는 것은 숫자를 단정해도 되는 유일한 경우다.
  it("states an empty building only when the server said so", () => {
    const status = rosterStatus({ roster: roster({ count: 0, items: [] }), failed: false, errorMessage: "" });
    expect(status.source).toBe("live");
    expect(status.countLabel).toBe("총 0명");
    expect(status.emptyText).toBe("현재 사내에 체류 중인 방문자가 없습니다.");
    expect(status.warning).toBe("");
  });

  it("keeps showing the last roster when a refresh fails", () => {
    const status = rosterStatus({ roster: roster(), failed: true, errorMessage: "명단을 불러오지 못했습니다" });
    expect(status.source).toBe("cache");
    expect(status.severity).toBe("warning");
    expect(status.countLabel).toBe("총 2명");
    expect(status.asOf).toBe("2026-10-05T01:02:03.000Z");
    expect(status.warning).toBe("마지막으로 받은 명단을 표시합니다. 지금 건물 안 인원과 다를 수 있습니다. (명단을 불러오지 못했습니다)");
  });

  // 브라우저의 네트워크 오류는 영어("Failed to fetch")로 온다. 종이를 든 사람이
  // 먼저 읽어야 하는 한국어 문장이 앞에 오고 원인은 괄호로 붙는다.
  it("leads with Korean and keeps the cause in parentheses", () => {
    const status = rosterStatus({ roster: null, failed: true, errorMessage: "Failed to fetch" });
    expect(status.warning.startsWith("명단을 불러오지 못해")).toBe(true);
    expect(status.warning).toContain("(Failed to fetch)");
  });

  it("omits the parenthetical when there is no cause to report", () => {
    expect(rosterStatus({ roster: null, failed: true, errorMessage: "" }).warning).not.toContain("(");
  });

  // 서비스워커(public/sw.js)가 캐시 응답에 offline: true 를 붙여 준다.
  it("tells the lobby it is offline when the service worker served the cache", () => {
    const status = rosterStatus({ roster: roster({ offline: true }), failed: false, errorMessage: "" });
    expect(status.source).toBe("cache");
    expect(status.warning).toContain("오프라인 상태입니다");
    expect(status.countLabel).toBe("총 2명");
  });

  describe("without any roster to show", () => {
    const failed = { roster: null, failed: true, errorMessage: "명단을 불러오지 못했습니다" };

    it("never claims the building is empty", () => {
      const status = rosterStatus(failed);
      expect(status.source).toBe("none");
      expect(status.severity).toBe("error");
      expect(status.countLabel).not.toBe("총 0명");
      expect(status.countLabel).toBe("명단 확인 불가");
      expect(status.emptyText).not.toBe("현재 사내에 체류 중인 방문자가 없습니다.");
      expect(status.emptyText).toContain("불러오지 못했습니다");
    });

    // 없는 명단을 표시한다고 적으면 종이를 든 사람이 빈 표를 명단으로 읽는다.
    it("does not say it is showing a roster it never received", () => {
      const status = rosterStatus(failed);
      expect(status.warning).toContain("지금 건물 안에 몇 명이 있는지 확인할 수 없습니다");
      expect(status.warning).not.toContain("마지막으로 받은 명단을 표시합니다");
      expect(status.asOf).toBeNull();
      expect(status.asOfFallback).toBe("확인 불가");
    });

    it("stays quiet while the first request is still in flight", () => {
      const status = rosterStatus({ roster: null, failed: false, errorMessage: "" });
      expect(status.source).toBe("none");
      expect(status.warning).toBe("");
      expect(status.countLabel).not.toBe("총 0명");
      expect(status.asOf).toBeNull();
    });
  });

  it("separates the four states from one another", () => {
    const live = rosterStatus({ roster: roster(), failed: false, errorMessage: "" });
    const cached = rosterStatus({ roster: roster(), failed: true, errorMessage: "조회 실패" });
    const offline = rosterStatus({ roster: roster({ offline: true }), failed: false, errorMessage: "" });
    const none = rosterStatus({ roster: null, failed: true, errorMessage: "조회 실패" });
    const warnings = [live.warning, cached.warning, offline.warning, none.warning];
    expect(new Set(warnings).size).toBe(4);
    expect([live.source, cached.source, offline.source, none.source]).toEqual(["live", "cache", "cache", "none"]);
  });
});
