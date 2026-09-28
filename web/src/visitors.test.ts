import { describe, expect, it } from "vitest";
import { maxVisitors, recurrenceError, submitBlockReason, visitorCountError, visitorFieldErrors, visitorsError, type SubmitBlockInput } from "./visitors";

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
