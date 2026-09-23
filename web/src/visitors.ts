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
