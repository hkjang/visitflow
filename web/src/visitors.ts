// 방문자 사전 검사. 서버 createVisitRecord는 이름을 TrimSpace 하고 휴대전화는
// normalizePhone 으로 숫자만 센 뒤 7자리 미만이면 방문자 전체를 하나의 400
// invalid_visitor 로 거절하는데, 그 메시지는 몇 번째 방문자가 문제인지 알려주지
// 못한다. 같은 경계를 화면에서 먼저 확인해 방문자 번호를 붙여 안내한다.
// 아직 아무것도 입력하지 않은 빈 칸은 여기서 다루지 않는다 — 제출 버튼이
// 이미 빈 필수 칸에서 잠기므로, 첫 화면을 빨갛게 칠하지 않기 위해서다.
export type VisitorCheck = { name: string; phone: string };
export type VisitorFieldErrors = { name: string; phone: string };

const minPhoneDigits = 7;
// 서버 normalizePhone과 같이 ASCII 숫자만 남긴다.
const phoneDigits = (value: string) => value.replace(/[^0-9]/g, "");

export function visitorFieldErrors(visitor: VisitorCheck): VisitorFieldErrors {
  return {
    name: visitor.name !== "" && visitor.name.trim() === "" ? "이름은 공백만 입력할 수 없습니다" : "",
    phone: visitor.phone !== "" && phoneDigits(visitor.phone).length < minPhoneDigits ? "휴대전화는 숫자 7자리 이상 입력하세요" : "",
  };
}

// 한 신청에 담을 수 있는 방문자 수. 서버 createVisitRecord 는 101명부터
// required_fields 로 거절한다.
export const maxVisitors = 100;
// 반복 예약이 만드는 전체 방문 일정 수의 상한. 서버는
// occurrences*len(Visitors) 가 이 값을 넘으면 invalid_recurrence 로 거절한다.
export const maxRecurringSchedules = 500;

export function visitorCountError(count: number): string {
  return count > maxVisitors ? `방문자는 최대 ${maxVisitors}명까지 등록할 수 있습니다` : "";
}

// 반복 횟수 자체의 2~52회 범위는 입력칸이 이미 좁혀 두므로, 여기서는 방문자 수에
// 따라 달라지는 전체 일정 상한만 본다. 화면의 "최대 52회" 안내는 방문자가 한
// 명일 때만 맞고, 10명이면 실제 상한은 50회다.
export function recurrenceError(visitorCount: number, occurrences: number): string {
  if (visitorCount < 1 || occurrences * visitorCount <= maxRecurringSchedules) return "";
  const allowed = Math.floor(maxRecurringSchedules / visitorCount);
  return `방문자 ${visitorCount}명이면 반복 예약은 최대 ${allowed}회까지 가능합니다 (전체 방문 일정 ${maxRecurringSchedules}건 상한)`;
}

// 제출 버튼과 submit() 가드가 읽는 값. 칸에 붙는 안내와 어긋나지 않도록
// 같은 visitorFieldErrors 결과에서만 만든다.
export function visitorsError(visitors: VisitorCheck[]): string {
  for (const [index, visitor] of visitors.entries()) {
    const errors = visitorFieldErrors(visitor);
    const message = errors.name || errors.phone;
    if (message) return `방문자 ${index + 1}: ${message}`;
  }
  return "";
}
