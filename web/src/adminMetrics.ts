// 관리 Dashboard 「운영 지표」 카드가 읽는 표시 상태. 예전에는 조회 실패를
// `setMetrics(null)` 로 삼키고 카드가 `metrics?.queueBacklog ?? 0` 꼴로 여섯 값을
// 전부 0 과 `v-` 로 렌더링했다 — 지표가 끊긴 장애 상태가 화면에서는 「대기열 0건 ·
// 잠긴 계정 0건 · 활성 세션 0개」라는 완벽히 건강한 시스템으로 보였다(이 화면을
// 보고 있는 관리자 자신의 세션이 있으므로 「활성 세션 0개」는 성립조차 않는다).
//
// 신규 설치에서는 `queueBacklog`·`lockedAccounts` 가 **정상적으로** 0 이므로 값이
// 0 이라는 사실은 실패 근거가 될 수 없다. 그래서 조회 중 / 실패 / 정상을 판별
// 가능한 한 값으로 두고, 카드의 여섯 행이 그 한 값만 읽는다 — `?? 0` 이 타입으로
// 불가능해지고, 실패는 전용 상태에서만 나온다.

import type { OperationalMetrics } from "./types";

export type MetricsState =
  | { state: "loading" }
  | { state: "failed"; message: string }
  | { state: "ready"; value: OperationalMetrics };

export type MetricsRow = { label: string; value: string };

export type MetricsPanel =
  // notice 일 때 카드 본문에 숫자가 단 하나도 없다.
  | { kind: "notice"; severity: "info" | "warning"; message: string }
  | { kind: "rows"; rows: MetricsRow[] };

// 조회 실패의 원인은 서버의 한국어 메시지일 수도, 브라우저의 영어 네트워크
// 오류("Failed to fetch")일 수도 있다. 운영자가 먼저 읽어야 하는 것은 지표를
// 믿을 수 없다는 사실이므로 한국어 문장을 앞에 두고 원인을 괄호로 덧붙인다.
function withCause(sentence: string, errorMessage: string): string {
  return errorMessage === "" ? sentence : `${sentence} (${errorMessage})`;
}

export function metricsPanel(metrics: MetricsState): MetricsPanel {
  if (metrics.state === "loading") {
    return { kind: "notice", severity: "info", message: "운영 지표를 불러오는 중입니다." };
  }
  if (metrics.state === "failed") {
    return {
      kind: "notice", severity: "warning",
      message: withCause("운영 지표를 불러오지 못했습니다. 알림 대기열·잠긴 계정·세션 수를 지금은 확인할 수 없으므로 정상이라고 판단하지 마세요.", metrics.message),
    };
  }
  const value = metrics.value;
  return {
    kind: "rows",
    rows: [
      { label: "알림 대기열", value: `${value.queueBacklog}건` },
      { label: "대기열 최장 지연", value: `${Math.round(value.queueOldestSeconds / 60)}분` },
      { label: "잠긴 계정·주소", value: `${value.lockedAccounts}건` },
      { label: "활성 세션", value: `${value.activeSessions}개` },
      { label: "활성 API 키", value: `${value.activeApiKeys}개` },
      { label: "스키마 버전", value: `v${value.schemaVersion}` },
    ],
  };
}
