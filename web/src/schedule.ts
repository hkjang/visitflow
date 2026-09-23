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
