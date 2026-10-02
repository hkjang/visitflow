// 방문 일정 사전 검사. datetime-local 칸을 비우면 값이 ""이 되어 submit()의
// new Date(startAt).toISOString()이 RangeError("Invalid time value")를 던지므로,
// 같은 파싱으로 미리 확인해 한국어 안내를 돌려준다. 경계는 서버
// createVisitRecord와 같다 — 종료는 시작보다 뒤여야 하고(같으면 거절), 31일까지 허용.
const maxDurationMs = 31 * 24 * 60 * 60 * 1000;

export function scheduleError(startAt: string, endAt: string): string {
  const start = new Date(startAt);
  if (Number.isNaN(start.getTime())) return "방문 시작시간을 올바르게 입력하세요";
  const end = new Date(endAt);
  if (Number.isNaN(end.getTime())) return "방문 종료시간을 올바르게 입력하세요";
  if (end.getTime() <= start.getTime()) return "방문 종료시간은 시작시간 이후여야 합니다";
  if (end.getTime() - start.getTime() > maxDurationMs) return "한 방문 일정은 31일을 초과할 수 없습니다";
  return "";
}

// VisitsPage 의 「방문 일정 수정」이 읽는 값. 저장 버튼은 같은 경계를 인라인으로
// 다시 쓰면서(`new Date(end) <= new Date(start)`) 31일 상한을 빼먹어 서버
// updateVisit 의 schedule_too_long 까지 왕복해야 알 수 있었고, 방문 목적은
// `!edit?.purpose` 로만 봐서 공백만 입력한 값이 서버 invalid_visit 으로 나갔다.
// 무엇보다 버튼이 잠긴 이유를 화면이 말하지 않았다. 신청 화면과 같은
// scheduleError 를 그대로 쓰므로 두 경로가 같은 입력을 같은 값으로 읽는다.
//
// 한 줄만 보여 주므로 순서가 계약이다 — 화면을 위에서 아래로 읽는 순서로
// 시작·종료를 먼저 보고 그다음 방문 목적을 본다.
export function visitEditError(startAt: string, endAt: string, purpose: string): string {
  const schedule = scheduleError(startAt, endAt);
  if (schedule !== "") return schedule;
  return purpose.trim() === "" ? "방문 목적을 입력하세요" : "";
}
