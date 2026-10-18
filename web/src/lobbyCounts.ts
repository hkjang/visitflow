// Lobby Dashboard 가 읽는 표시 상태. 예전에는 `data` 가 `{ counts: {}, items: [] }`
// 로 시작하고 조회 실패의 catch 가 닫을 수 있는 error Alert 하나만 띄웠다 — 한 번도
// 성공하지 못한 채 `/api/v1/lobby/today` 가 실패하면 화면은 「방문 예정 0 · 현재
// 방문중 0 · 퇴실 완료 0 · 미방문 0」과 「표시할 방문자가 없습니다.」를 그대로
// 렌더링했다. 조회가 끊긴 장애가 「아무도 안 왔고 건물에 아무도 없다」는 사실
// 주장으로 보이고, 그중 「현재 방문중」은 화면이 스스로 `재난 시 체류 기준 인원`
// 이라고 적어 둔 값이다. 한 번 성공한 뒤의 재조회 실패는 더 조용해서, 알림을 닫으면
// 최신인 척하는 옛 숫자만 남았다.
//
// 신규·한가한 사업장에서는 네 값이 **정상적으로** 0 이므로 값이 0 이라는 사실은
// 실패 근거가 될 수 없다. 그래서 조회 중 / 실패(캐시 없음) / 실패(캐시 있음) /
// 정상을 판별 가능한 한 값으로 두고 화면이 그 한 값만 읽는다 — `?? 0` 이 타입으로
// 불가능해지고, 실패는 전용 상태에서만 나온다. adminMetrics.ts 와 같은 모양이지만
// 두 화면을 한 모듈에 묶지 않도록 import 하지 않고 따로 둔다.

import type { LobbyVisitor } from "./types";

export type LobbyToday = {
  counts: Record<string, number>;
  items: LobbyVisitor[];
  truncated?: boolean;
  limit?: number;
};

// 마지막으로 성공한 조회와, 그 조회가 어느 화면(탭 · 검색어 · 로비 필터)의 것인지.
export type LobbySnapshot = { value: LobbyToday; at: string; view: string };

export type LobbyLoad =
  | { state: "loading" }
  | { state: "failed"; message: string; last: LobbySnapshot | null }
  | { state: "ready"; value: LobbyToday; at: string; view: string };

export type LobbyCountKey = "scheduled" | "current" | "completed" | "noShow";

export type LobbySummary =
  // notice 일 때 네 카드 자리에 숫자가 단 하나도 없다.
  | { kind: "notice"; severity: "info" | "warning"; message: string }
  | { kind: "counts"; counts: Record<LobbyCountKey, number>; staleNote?: string };

export type LobbyPanel = {
  summary: LobbySummary;
  items: LobbyVisitor[];
  // 목록이 비었을 때 그 자리에 쓸 문장. 빈 로비와 조회 실패가 같은 말을 하지 않는다.
  emptyText: string;
  currentTabLabel: string;
  // 「표시 한도 초과」는 실제로 받아 화면에 올린 응답에 대해서만 말할 수 있다.
  truncationNote: string;
};

const NO_ITEMS: LobbyVisitor[] = [];
const DEFAULT_LIMIT = 300;
const READY_EMPTY = "표시할 방문자가 없습니다.";
const LOOKUP_FAILED = "로비 현황을 불러오지 못해 방문자 목록을 표시할 수 없습니다. 비상 명단에서 체류 인원을 확인하세요.";

// 조회 실패의 원인은 서버의 한국어 메시지일 수도, 브라우저의 영어 네트워크
// 오류("Failed to fetch")일 수도 있다. 로비 담당자가 먼저 읽어야 하는 것은 현황을
// 믿을 수 없다는 사실이므로 한국어 문장을 앞에 두고 원인을 괄호로 덧붙인다.
function withCause(sentence: string, errorMessage: string): string {
  return errorMessage === "" ? sentence : `${sentence} (${errorMessage})`;
}

// 기준 시각이 없거나 깨졌으면 "Invalid Date" 를 보여 주는 대신 시각을 생략한다.
function stamp(at: string): string {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit" });
}

// 서버는 네 키를 모두 담아 보낸다(visits.go lobbyList). 200 응답에 한해 빠진 키를
// 0 으로 읽는 것이고, 조회 실패는 여기까지 오지 않는다.
function fourCounts(counts: Record<string, number>): Record<LobbyCountKey, number> {
  return {
    scheduled: counts.scheduled ?? 0,
    current: counts.current ?? 0,
    completed: counts.completed ?? 0,
    noShow: counts.noShow ?? 0,
  };
}

function truncation(value: LobbyToday): string {
  if (!value.truncated) return "";
  return `표시 한도 ${value.limit ?? DEFAULT_LIMIT}건을 넘었습니다. 검색으로 범위를 좁히거나 비상 명단에서 전체를 확인하세요.`;
}

// `view` 는 지금 화면이 보고 있는 탭 · 검색어 · 로비 필터다. 카운트는 탭·검색어·
// 필터와 무관한 사업장 전체 집계이므로 화면이 바뀌어도 계속 참이지만, 목록은
// 그렇지 않다 — 오늘 방문 예정자를 「현재 방문자」 탭에 남겨 두면 같은 거짓말이다.
export function lobbyPanel(load: LobbyLoad, view: string): LobbyPanel {
  if (load.state === "loading") {
    const message = "로비 현황을 불러오는 중입니다.";
    return { summary: { kind: "notice", severity: "info", message }, items: NO_ITEMS, emptyText: message, currentTabLabel: "현재 방문자", truncationNote: "" };
  }
  if (load.state === "failed") {
    const last = load.last;
    if (last === null) {
      return {
        summary: {
          kind: "notice", severity: "warning",
          message: withCause("로비 현황을 불러오지 못했습니다. 오늘 방문 인원과 사내 체류 인원을 지금은 알 수 없으니 건물에 아무도 없다고 판단하지 마세요. 새로고침하거나 비상 명단으로 확인하세요.", load.message),
        },
        items: NO_ITEMS, emptyText: LOOKUP_FAILED, currentTabLabel: "현재 방문자", truncationNote: "",
      };
    }
    const at = stamp(last.at);
    const sameView = last.view === view;
    const counts = fourCounts(last.value.counts);
    return {
      summary: {
        kind: "counts", counts,
        staleNote: withCause(`${at === "" ? "마지막으로 성공한 조회의 값입니다." : `${at} 기준으로 마지막으로 성공한 조회의 값입니다.`} 최신이 아닐 수 있습니다. 재난 대응에 쓰기 전에 비상 명단으로 확인하세요.`, load.message),
      },
      items: sameView ? last.value.items : NO_ITEMS,
      emptyText: sameView
        ? `${at === "" ? "마지막 조회" : `${at} 기준 마지막 조회`}에는 표시할 방문자가 없었습니다. 그 뒤로 현황을 다시 불러오지 못했습니다.`
        : LOOKUP_FAILED,
      currentTabLabel: `현재 방문자 ${counts.current}`,
      truncationNote: "",
    };
  }
  const counts = fourCounts(load.value.counts);
  return {
    summary: { kind: "counts", counts },
    items: load.value.items,
    emptyText: READY_EMPTY,
    currentTabLabel: `현재 방문자 ${counts.current}`,
    truncationNote: truncation(load.value),
  };
}

export function lobbyView(tab: string, query: string, lobbyFilter: string): string {
  return JSON.stringify([tab, query, lobbyFilter]);
}
