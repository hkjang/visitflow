// 비상 대피 명단 화면이 읽는 표시 상태. 이 화면은 인쇄해서 건물 밖으로 들고
// 나가는 종이가 결과물이므로 「건물에 0명」과 「몇 명인지 모른다」를 절대 섞어
// 말하면 안 된다. 명단을 한 번도 받지 못한 상태에서 조회가 실패하면 예전에는
// 기준 시각 `-`, 칩 `총 0명`, 본문 「현재 사내에 체류 중인 방문자가 없습니다.」가
// 그대로 나왔고 경고는 없는 명단을 「마지막으로 받은 명단을 표시합니다」라고 적었다.
//
// 출처가 셋이고(실시간 / 캐시 / 전무) 캐시는 두 경로로 온다 — localStorage 의
// 마지막 성공 응답과 서비스워커가 `offline: true` 를 붙여 주는 응답(public/sw.js).
// 화면의 네 자리(기준 시각·칩·빈 상태 문장·경고)가 모두 이 한 값을 읽으므로 두
// 경로가 같은 입력을 같은 값으로 읽는다.

export type RosterEntry = {
  site: string; lobby?: string; visitor: string; company?: string; phone?: string;
  host: string; department?: string; checkedInAt?: string; badgeNo?: string; placeDetail?: string;
};

export type Roster = { generatedAt: string; count: number; items: RosterEntry[]; offline?: boolean };

export type RosterStatusInput = {
  roster: Roster | null;
  // 마지막 조회가 실패했는지. 성공 응답을 받으면 다시 false 가 된다.
  failed: boolean;
  // api() 가 올려 준 서버·네트워크 메시지(한국어).
  errorMessage: string;
};

export type RosterStatus = {
  // live = 방금 서버에서 받은 명단, cache = 마지막으로 받은 명단(오프라인 포함),
  // none = 표시할 명단이 아예 없다(최초 조회 중이거나 그 조회가 실패했다).
  source: "live" | "cache" | "none";
  severity: "warning" | "error";
  // 빈 문자열이면 경고를 띄우지 않는다. 비어 있지 않으면 인쇄물에도 남는다.
  warning: string;
  countLabel: string;
  emptyText: string;
  // 기준 시각으로 쓸 응답의 generatedAt. null 이면 asOfFallback 을 그대로 쓴다.
  asOf: string | null;
  asOfFallback: string;
};

// 조회 실패의 원인은 서버의 한국어 메시지일 수도, 브라우저의 영어 네트워크
// 오류("Failed to fetch")일 수도 있다. 종이를 든 사람이 먼저 읽어야 하는 것은
// 명단을 믿을 수 있는지이므로 한국어 문장을 앞에 두고 원인을 괄호로 덧붙인다.
function withCause(sentence: string, errorMessage: string): string {
  return errorMessage === "" ? sentence : `${sentence} (${errorMessage})`;
}

export function rosterStatus({ roster, failed, errorMessage }: RosterStatusInput): RosterStatus {
  if (roster === null) {
    if (!failed) {
      return {
        source: "none", severity: "warning", warning: "",
        countLabel: "명단 확인 중", emptyText: "명단을 불러오는 중입니다.",
        asOf: null, asOfFallback: "확인 중",
      };
    }
    return {
      source: "none", severity: "error",
      warning: withCause("명단을 불러오지 못해 지금 건물 안에 몇 명이 있는지 확인할 수 없습니다. 아래 숫자를 0명으로 읽지 마세요.", errorMessage),
      countLabel: "명단 확인 불가",
      emptyText: "명단을 불러오지 못했습니다. 체류 중인 방문자가 없다는 뜻이 아니므로 네트워크를 확인한 뒤 다시 시도하세요.",
      asOf: null, asOfFallback: "확인 불가",
    };
  }
  // 실패했지만 보여 줄 명단이 있을 때만 「마지막으로 받은 명단」이라고 말한다.
  if (failed) {
    return {
      source: "cache", severity: "warning",
      warning: withCause("마지막으로 받은 명단을 표시합니다. 지금 건물 안 인원과 다를 수 있습니다.", errorMessage),
      countLabel: `총 ${roster.count}명`,
      emptyText: "마지막으로 받은 명단에는 체류 중인 방문자가 없습니다.",
      asOf: roster.generatedAt, asOfFallback: "확인 불가",
    };
  }
  if (roster.offline) {
    return {
      source: "cache", severity: "warning",
      warning: "오프라인 상태입니다. 마지막으로 동기화된 명단이며 지금 건물 안 인원과 다를 수 있습니다.",
      countLabel: `총 ${roster.count}명`,
      emptyText: "마지막으로 받은 명단에는 체류 중인 방문자가 없습니다.",
      asOf: roster.generatedAt, asOfFallback: "확인 불가",
    };
  }
  return {
    source: "live", severity: "warning", warning: "",
    countLabel: `총 ${roster.count}명`,
    emptyText: "현재 사내에 체류 중인 방문자가 없습니다.",
    asOf: roster.generatedAt, asOfFallback: "확인 불가",
  };
}
