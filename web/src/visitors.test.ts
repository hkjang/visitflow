import { describe, expect, it } from "vitest";
import { duplicatePhoneError, duplicatePhoneErrors, maxRecurringOccurrences, maxVisitors, minRecurringOccurrences, recurrenceError, repeatCountError, submitBlockReason, visitorCountError, visitorFieldErrors, visitorsError, type SubmitBlockInput } from "./visitors";

const visitor = (name: string, phone: string) => ({ name, phone });

describe("visitorFieldErrors", () => {
  it("accepts a visitor the server accepts", () => {
    expect(visitorFieldErrors(visitor("홍길동", "010-1234-5678"))).toEqual({ name: "", phone: "" });
  });

  // The blank row the form starts with must not turn red before anything is typed;
  // the submit button already stays disabled while a required field is empty.
  it("leaves an untouched empty row unmarked", () => {
    expect(visitorFieldErrors(visitor("", ""))).toEqual({ name: "", phone: "" });
  });

  it("marks a name the server trims away to nothing", () => {
    expect(visitorFieldErrors(visitor("   ", "01012345678")).name).toBe("이름은 공백만 입력할 수 없습니다");
  });

  it("keeps a name with inner spaces", () => {
    expect(visitorFieldErrors(visitor("홍 길동", "01012345678")).name).toBe("");
  });

  // The server counts digits with normalizePhone and requires 7 or more.
  it("marks a phone with fewer than seven digits", () => {
    expect(visitorFieldErrors(visitor("홍길동", "010")).phone).toBe("휴대전화는 숫자 7자리 이상 입력하세요");
    expect(visitorFieldErrors(visitor("홍길동", "010-123")).phone).toBe("휴대전화는 숫자 7자리 이상 입력하세요");
  });

  it("accepts exactly seven digits, which the server accepts", () => {
    expect(visitorFieldErrors(visitor("홍길동", "1234567")).phone).toBe("");
  });

  it("counts only digits, the way the server does", () => {
    expect(visitorFieldErrors(visitor("홍길동", "+82 10-1234-5678")).phone).toBe("");
    expect(visitorFieldErrors(visitor("홍길동", "연락처없음")).phone).toBe("휴대전화는 숫자 7자리 이상 입력하세요");
    expect(visitorFieldErrors(visitor("홍길동", "    ")).phone).toBe("휴대전화는 숫자 7자리 이상 입력하세요");
  });

  it("marks each field independently", () => {
    expect(visitorFieldErrors(visitor(" ", "010"))).toEqual({ name: "이름은 공백만 입력할 수 없습니다", phone: "휴대전화는 숫자 7자리 이상 입력하세요" });
  });
});

describe("visitorsError", () => {
  it("accepts a list the server accepts", () => {
    expect(visitorsError([visitor("홍길동", "01012345678"), visitor("김철수", "010 9876 5432")])).toBe("");
  });

  // Empty required fields are reported by the existing disabled check, not here,
  // so a half-filled form never shows a message about a row nobody has touched.
  it("stays silent for rows that are merely empty", () => {
    expect(visitorsError([visitor("홍길동", "01012345678"), visitor("", "")])).toBe("");
  });

  it("names the offending visitor by number", () => {
    expect(visitorsError([visitor("홍길동", "01012345678"), visitor("김철수", "010")])).toBe("방문자 2: 휴대전화는 숫자 7자리 이상 입력하세요");
  });

  it("reports the first offending visitor", () => {
    expect(visitorsError([visitor("홍길동", "1"), visitor("김철수", "2")])).toBe("방문자 1: 휴대전화는 숫자 7자리 이상 입력하세요");
  });

  it("reports the name before the phone of the same visitor", () => {
    expect(visitorsError([visitor(" ", "010")])).toBe("방문자 1: 이름은 공백만 입력할 수 없습니다");
  });

  // The field helper texts and the button/submit guard must never disagree.
  it("agrees with the field errors the same rows produce", () => {
    const rows = [visitor("", ""), visitor("홍길동", "01012345678"), visitor(" ", "010"), visitor("김철수", "12")];
    for (let i = 0; i < rows.length; i += 1) {
      const list = rows.slice(0, i + 1);
      const marked = list.findIndex((row) => visitorFieldErrors(row).name !== "" || visitorFieldErrors(row).phone !== "");
      const expected = marked < 0 ? "" : `방문자 ${marked + 1}: ${visitorFieldErrors(list[marked]).name || visitorFieldErrors(list[marked]).phone}`;
      expect(visitorsError(list)).toBe(expected);
    }
  });

  it("never leaks English text into the Korean form", () => {
    for (const list of [[visitor(" ", "")], [visitor("홍길동", "abc")], [visitor("", "12"), visitor(" ", "01012345678")]]) {
      const message = visitorsError(list);
      expect(message).not.toBe("");
      expect(message).not.toMatch(/[A-Za-z]/);
    }
  });
});

describe("visitorCountError", () => {
  // 서버 createVisitRecord: len(in.Visitors) > 100 이면 required_fields 로 거절한다.
  it("accepts one visitor, the form's starting state", () => {
    expect(visitorCountError(1)).toBe("");
  });

  it("accepts exactly the server limit", () => {
    expect(maxVisitors).toBe(100);
    expect(visitorCountError(100)).toBe("");
  });

  it("marks one visitor past the server limit", () => {
    expect(visitorCountError(101)).toBe("방문자는 최대 100명까지 등록할 수 있습니다");
  });

  it("never leaks English text into the Korean form", () => {
    expect(visitorCountError(140)).not.toMatch(/[A-Za-z]/);
  });
});

describe("recurrenceError", () => {
  // 서버 createVisitRecord: occurrences*len(in.Visitors) > 500 이면
  // invalid_recurrence 로 거절한다. 화면의 "최대 52회" 안내는 방문자가
  // 여러 명일 때 성립하지 않으므로 방문자 수에 따른 실제 상한을 보여준다.
  it("accepts a single visitor repeating the full 52 times", () => {
    expect(recurrenceError(1, 52)).toBe("");
  });

  it("accepts exactly 500 total visitor schedules", () => {
    expect(recurrenceError(10, 50)).toBe("");
    expect(recurrenceError(100, 5)).toBe("");
  });

  it("marks one schedule past the 500 total the server allows", () => {
    expect(recurrenceError(10, 51)).toBe("방문자 10명이면 반복 예약은 최대 50회까지 가능합니다 (전체 방문 일정 500건 상한)");
    expect(recurrenceError(100, 6)).toBe("방문자 100명이면 반복 예약은 최대 5회까지 가능합니다 (전체 방문 일정 500건 상한)");
  });

  // 화면이 실제로 보내는 값(방문자 10명 · 52회)은 지금 서버에서 거절된다.
  it("marks the form's own maximum repeat count for a ten person visit", () => {
    expect(recurrenceError(10, 52)).not.toBe("");
  });

  it("never leaks English text into the Korean form", () => {
    expect(recurrenceError(10, 52)).not.toMatch(/[A-Za-z]/);
  });
});

// 반복 예약 「총 예약 횟수」 칸이 입력한 문자열 그대로를 보고 판정한다. 이전에는
// onChange 가 키 입력마다 Math.max(2, Math.min(52, Number(...))) 로 숫자 state 를
// 덮어써서 칸을 비울 수도 없고 10~19 를 입력할 수도 없었으며, 반대로 소수는
// 그대로 통과해 서버 visits.go 의 int(float64) 절단이 말없이 횟수를 바꿨다.
describe("repeatCountError", () => {
  it("accepts the default the form starts with", () => {
    expect(repeatCountError("2")).toBe("");
  });

  // 칸을 비우면 빈 칸으로 남는다 — 되돌려진 "2" 가 아니라 안내가 떠야 한다.
  it("asks for a value when the field is emptied", () => {
    expect(repeatCountError("")).toBe("총 예약 횟수를 입력하세요");
    expect(repeatCountError("  ")).toBe("총 예약 횟수를 입력하세요");
  });

  // 서버는 occurrences 를 int(float64) 로 잘라 2.5 를 조용히 2회로 만든다.
  it("rejects a fraction the server would silently truncate", () => {
    expect(repeatCountError("2.5")).toBe("총 예약 횟수는 정수로 입력하세요");
    expect(repeatCountError("10.0")).toBe("");
  });

  it("rejects text that is not a number at all", () => {
    expect(repeatCountError("abc")).toBe("총 예약 횟수는 정수로 입력하세요");
  });

  // 서버 경계: occurrences < 2 || occurrences > 52 는 invalid_recurrence 다.
  it("rejects counts outside the range the server accepts", () => {
    expect(repeatCountError("1")).toBe("총 예약 횟수는 2~52회 사이로 입력하세요");
    expect(repeatCountError("53")).toBe("총 예약 횟수는 2~52회 사이로 입력하세요");
    expect(repeatCountError("0")).toBe("총 예약 횟수는 2~52회 사이로 입력하세요");
    expect(repeatCountError("-3")).toBe("총 예약 횟수는 2~52회 사이로 입력하세요");
  });

  it("accepts both ends of the range and the counts the old clamp swallowed", () => {
    expect(minRecurringOccurrences).toBe(2);
    expect(maxRecurringOccurrences).toBe(52);
    for (const raw of ["2", "10", "19", "52"]) expect(repeatCountError(raw)).toBe("");
  });

  it("ignores surrounding whitespace the way Number does", () => {
    expect(repeatCountError(" 10 ")).toBe("");
  });

  it("never leaks English text into the Korean form", () => {
    for (const raw of ["", "2.5", "abc", "1", "53"]) {
      const message = repeatCountError(raw);
      expect(message).not.toBe("");
      expect(message).not.toMatch(/[A-Za-z]/);
    }
  });

  // 칸의 안내와 제출 가드가 한 파싱 결과만 보게 하려면, repeatCountError 가
  // "" 인 입력은 반드시 recurrenceError 에 넣을 수 있는 정수여야 한다.
  it("leaves only integers recurrenceError can use", () => {
    for (const raw of ["", " ", "2", "2.5", "abc", "1", "53", "10", "52", "1e2"]) {
      if (repeatCountError(raw) !== "") continue;
      const parsed = Number(raw.trim());
      expect(Number.isInteger(parsed)).toBe(true);
      expect(parsed).toBeGreaterThanOrEqual(minRecurringOccurrences);
      expect(parsed).toBeLessThanOrEqual(maxRecurringOccurrences);
    }
  });
});

// 제출 버튼이 말없이 잠기던 네 가지 원인. 서버가 보는 것은 consent 뿐이고
// (visits.go 의 invalid_visitor) 나머지 셋은 화면만의 게이트지만, 어느 쪽이든
// 버튼이 잠긴 이유를 화면이 말해 주어야 한다.
const block = (patch: Partial<SubmitBlockInput> = {}): SubmitBlockInput => ({
  visitors: [{ consent: true, vehicle: "", equipment: "" }],
  requiresNda: false,
  requiresSafetyBriefing: false,
  requiresVehicle: false,
  requiresEquipment: false,
  checklistNda: false,
  checklistSafetyBriefing: false,
  walkIn: false,
  hostUserId: "",
  ...patch,
});

describe("submitBlockReason", () => {
  it("stays silent when nothing blocks submission", () => {
    expect(submitBlockReason(block())).toBe("");
  });

  // 4) 아직 손대지 않은 빈 필수 칸(이름·전화·방문 목적)은 여기서 다루지 않는다 —
  // 첫 화면을 빨갛게 칠하지 않는 visitors.ts 의 기존 방침.
  it("stays silent for the untouched first screen", () => {
    expect(submitBlockReason(block({ visitors: [{ consent: true, vehicle: "", equipment: "" }] }))).toBe("");
  });

  it("names the visitor whose consent checkbox is cleared", () => {
    expect(submitBlockReason(block({ visitors: [{ consent: true, vehicle: "", equipment: "" }, { consent: false, vehicle: "", equipment: "" }] }))).toBe("방문자 2: 개인정보 수집·이용 동의를 확인해 주세요");
  });

  it("asks for the security pledge checkbox the visit type requires", () => {
    expect(submitBlockReason(block({ requiresNda: true }))).toBe("선택한 방문 유형에는 보안서약 안내 확인이 필요합니다");
    expect(submitBlockReason(block({ requiresNda: true, checklistNda: true }))).toBe("");
  });

  it("asks for the safety briefing checkbox the visit type requires", () => {
    expect(submitBlockReason(block({ requiresSafetyBriefing: true }))).toBe("선택한 방문 유형에는 안전교육 이수 확인이 필요합니다");
    expect(submitBlockReason(block({ requiresSafetyBriefing: true, checklistSafetyBriefing: true }))).toBe("");
  });

  it("names the visitor missing a vehicle number the visit type requires", () => {
    expect(submitBlockReason(block({ requiresVehicle: true, visitors: [{ consent: true, vehicle: "12가3456", equipment: "" }, { consent: true, vehicle: "  ", equipment: "" }] }))).toBe("방문자 2: 선택한 방문 유형에는 차량번호가 필요합니다");
  });

  it("names the visitor missing the equipment the visit type requires", () => {
    expect(submitBlockReason(block({ requiresEquipment: true, visitors: [{ consent: true, vehicle: "", equipment: " 노트북 " }, { consent: true, vehicle: "", equipment: "" }] }))).toBe("방문자 2: 선택한 방문 유형에는 반입 장비가 필요합니다");
  });

  it("asks a walk-in registration to pick a host", () => {
    expect(submitBlockReason(block({ walkIn: true }))).toBe("현장 방문 등록에는 방문 담당자를 선택해야 합니다");
    expect(submitBlockReason(block({ walkIn: true, hostUserId: "u1" }))).toBe("");
  });

  // 담당자 미선택은 현장 등록 화면에만 있는 게이트다.
  it("ignores an empty host outside walk-in registration", () => {
    expect(submitBlockReason(block({ walkIn: false, hostUserId: "" }))).toBe("");
  });

  // 한 줄만 보여 주므로 순서가 계약이다: 화면 위에서 아래로 —
  // 담당자 → 체크리스트 → 방문자별(차량 → 장비 → 동의).
  it("shows only the first cause when several apply", () => {
    const all = block({ walkIn: true, requiresNda: true, requiresSafetyBriefing: true, requiresVehicle: true, requiresEquipment: true, visitors: [{ consent: false, vehicle: "", equipment: "" }] });
    expect(submitBlockReason(all)).toBe("현장 방문 등록에는 방문 담당자를 선택해야 합니다");
    expect(submitBlockReason({ ...all, hostUserId: "u1" })).toBe("선택한 방문 유형에는 보안서약 안내 확인이 필요합니다");
    expect(submitBlockReason({ ...all, hostUserId: "u1", checklistNda: true })).toBe("선택한 방문 유형에는 안전교육 이수 확인이 필요합니다");
    expect(submitBlockReason({ ...all, hostUserId: "u1", checklistNda: true, checklistSafetyBriefing: true })).toBe("방문자 1: 선택한 방문 유형에는 차량번호가 필요합니다");
    expect(submitBlockReason({ ...all, hostUserId: "u1", checklistNda: true, checklistSafetyBriefing: true, visitors: [{ consent: false, vehicle: "12가3456", equipment: "" }] })).toBe("방문자 1: 선택한 방문 유형에는 반입 장비가 필요합니다");
    expect(submitBlockReason({ ...all, hostUserId: "u1", checklistNda: true, checklistSafetyBriefing: true, visitors: [{ consent: false, vehicle: "12가3456", equipment: "노트북" }] })).toBe("방문자 1: 개인정보 수집·이용 동의를 확인해 주세요");
  });

  // 앞선 방문자의 원인이 뒤 방문자의 원인보다 먼저다.
  it("reports the earlier visitor first", () => {
    expect(submitBlockReason(block({ requiresVehicle: true, visitors: [{ consent: false, vehicle: "", equipment: "" }, { consent: false, vehicle: "", equipment: "" }] }))).toBe("방문자 1: 선택한 방문 유형에는 차량번호가 필요합니다");
  });

  // 2) 버튼 disabled 가 읽는 값이 하나가 되려면, 새 함수가 비어 있다는 것이
  // 지금의 네 항을 모두 만족한다는 것과 정확히 같아야 한다.
  it("matches the disabled expression it replaces", () => {
    const flags = [false, true];
    for (const requiresNda of flags) for (const requiresSafetyBriefing of flags) for (const requiresVehicle of flags) for (const requiresEquipment of flags) for (const checklistNda of flags) for (const checklistSafetyBriefing of flags) for (const walkIn of flags) for (const hostUserId of ["", "u1"]) for (const consent of flags) for (const vehicle of ["", "12가3456"]) for (const equipment of ["", "노트북"]) {
      const input = block({ requiresNda, requiresSafetyBriefing, requiresVehicle, requiresEquipment, checklistNda, checklistSafetyBriefing, walkIn, hostUserId, visitors: [{ consent: true, vehicle: "12가3456", equipment: "노트북" }, { consent, vehicle, equipment }] });
      const checklistSatisfied = (!requiresNda || checklistNda) && (!requiresSafetyBriefing || checklistSafetyBriefing);
      const declarationsSatisfied = input.visitors.every((v) => (!requiresVehicle || v.vehicle.trim() !== "") && (!requiresEquipment || v.equipment.trim() !== ""));
      const old = !checklistSatisfied || !declarationsSatisfied || input.visitors.some((x) => !x.consent) || (walkIn && hostUserId === "");
      expect(submitBlockReason(input) !== "").toBe(old);
    }
  });

  it("never leaks English text into the Korean form", () => {
    for (const patch of [{ walkIn: true }, { requiresNda: true }, { requiresSafetyBriefing: true }, { requiresVehicle: true }, { requiresEquipment: true }, { visitors: [{ consent: false, vehicle: "", equipment: "" }] }]) {
      const message = submitBlockReason(block(patch));
      expect(message).not.toBe("");
      expect(message).not.toMatch(/[A-Za-z]/);
    }
  });
});

// 서버 upsertVisitor 는 휴대전화 해시 하나로만 방문자를 찾으므로 한 신청에 같은
// 번호가 두 번 들어오면 두 방문자가 같은 visitor_id 를 받고 visitor_visits 의
// UNIQUE(visit_id,visitor_id) 가 이유를 알 수 없는 500 으로 터진다. 화면이 같은
// 정규화(phoneDigits)로 먼저 잡아 어느 칸을 고쳐야 하는지 알려준다.
describe("duplicatePhoneErrors", () => {
  it("stays quiet when every phone differs", () => {
    expect(duplicatePhoneErrors([visitor("홍길동", "010-1234-5678"), visitor("김철수", "010-1234-5679")])).toEqual(["", ""]);
  });

  // 서버 normalizePhone 과 같은 규칙이라 하이픈·공백이 달라도 같은 번호다.
  it("treats differently formatted digits as the same phone", () => {
    expect(duplicatePhoneErrors([visitor("홍길동", "010-1234-5678"), visitor("김철수", "01012345678")])).toEqual(["", "방문자 1 과 휴대전화가 같습니다. 방문자마다 다른 번호를 입력하세요"]);
    expect(duplicatePhoneErrors([visitor("홍길동", "010 1234 5678"), visitor("김철수", "010-1234-5678")])[1]).not.toBe("");
  });

  // 먼저 입력한 칸은 그대로 두고 뒤에 겹친 칸만 표시한다 — 고칠 곳이 하나여야 한다.
  it("marks only the later duplicate and points at the first one", () => {
    const errors = duplicatePhoneErrors([visitor("일", "010-1111-2222"), visitor("이", "010-3333-4444"), visitor("삼", "010-1111-2222")]);
    expect(errors[0]).toBe("");
    expect(errors[1]).toBe("");
    expect(errors[2]).toBe("방문자 1 과 휴대전화가 같습니다. 방문자마다 다른 번호를 입력하세요");
  });

  // 7자리 미만은 이미 visitorFieldErrors.phone 이 잡는다. 같은 칸에 두 안내를
  // 겹쳐 내보내지 않는다.
  it("does not double up on a phone visitorFieldErrors already rejects", () => {
    expect(visitorFieldErrors(visitor("김철수", "010")).phone).not.toBe("");
    expect(duplicatePhoneErrors([visitor("홍길동", "010"), visitor("김철수", "010")])).toEqual(["", ""]);
  });

  // 아직 아무것도 입력하지 않은 빈 칸은 조용하다 — 첫 화면을 빨갛게 칠하지 않는다.
  it("stays quiet for untouched empty fields", () => {
    expect(duplicatePhoneErrors([visitor("", ""), visitor("", "")])).toEqual(["", ""]);
    expect(duplicatePhoneErrors([visitor("홍길동", "010-1234-5678"), visitor("", "")])).toEqual(["", ""]);
  });

  it("stays quiet for a single visitor", () => {
    expect(duplicatePhoneErrors([visitor("홍길동", "010-1234-5678")])).toEqual([""]);
  });
});

describe("duplicatePhoneError", () => {
  // 버튼·가드가 읽는 한 줄은 칸에 붙는 문구와 같은 계산에서 나온다.
  it("prefixes the first field message with its visitor number", () => {
    expect(duplicatePhoneError([visitor("홍길동", "010-1234-5678"), visitor("김철수", "01012345678")])).toBe("방문자 2: 방문자 1 과 휴대전화가 같습니다. 방문자마다 다른 번호를 입력하세요");
  });

  it("is empty exactly when no field is marked", () => {
    const cases = [
      [visitor("홍길동", "010-1234-5678")],
      [visitor("홍길동", "010-1234-5678"), visitor("김철수", "010-1234-5679")],
      [visitor("홍길동", "010-1234-5678"), visitor("김철수", "010-1234-5678")],
      [visitor("홍길동", ""), visitor("김철수", "")],
      [visitor("홍길동", "010"), visitor("김철수", "010")],
      [visitor("일", "010-1111-2222"), visitor("이", "010-1111-2222"), visitor("삼", "010-1111-2222")],
    ];
    for (const visitors of cases) {
      expect(duplicatePhoneError(visitors) !== "").toBe(duplicatePhoneErrors(visitors).some((x) => x !== ""));
    }
  });

  it("reports the earliest marked field when several collide", () => {
    expect(duplicatePhoneError([visitor("일", "010-1111-2222"), visitor("이", "010-1111-2222"), visitor("삼", "010-1111-2222")])).toBe("방문자 2: 방문자 1 과 휴대전화가 같습니다. 방문자마다 다른 번호를 입력하세요");
  });

  it("never leaks English text into the Korean form", () => {
    const message = duplicatePhoneError([visitor("홍길동", "010-1234-5678"), visitor("김철수", "010-1234-5678")]);
    expect(message).not.toBe("");
    expect(message).not.toMatch(/[A-Za-z]/);
  });
});
