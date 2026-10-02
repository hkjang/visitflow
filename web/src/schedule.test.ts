import { describe, expect, it } from "vitest";
import { scheduleError, visitEditError } from "./schedule";

// The submit body is built with exactly this expression (VisitFormPage.submit),
// so "no message" has to mean "these two values survive toISOString()".
const toBody = (startAt: string, endAt: string) => ({ startAt: new Date(startAt).toISOString(), endAt: new Date(endAt).toISOString() });

// datetime-local strings are local wall time; the helper only reads them.
const START = "2026-09-23T10:00";
const plusMinutes = (from: string, minutes: number) => {
  const shifted = new Date(new Date(from).getTime() + minutes * 60000);
  return new Date(shifted.getTime() - shifted.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
};

describe("scheduleError", () => {
  it("accepts the default range the form starts with", () => {
    expect(scheduleError(plusMinutes(START, 60), plusMinutes(START, 180))).toBe("");
  });

  it("accepts a walk-in range that starts now", () => {
    expect(scheduleError(START, plusMinutes(START, 120))).toBe("");
  });

  it("reports the empty start the browser leaves after clearing the field", () => {
    expect(scheduleError("", plusMinutes(START, 180))).toBe("방문 시작시간을 올바르게 입력하세요");
  });

  it("reports an empty end", () => {
    expect(scheduleError(START, "")).toBe("방문 종료시간을 올바르게 입력하세요");
  });

  it("reports an unparseable start before looking at the end", () => {
    expect(scheduleError("   ", "")).toBe("방문 시작시간을 올바르게 입력하세요");
  });

  // The same parser submit() uses, rollover included, so the check and the body agree.
  it("reads values exactly as the submitted body does", () => {
    const rolledOver = "2026-02-31T10:00";
    expect(new Date(rolledOver).toISOString()).toBe(new Date("2026-03-03T10:00").toISOString());
    expect(scheduleError(rolledOver, "2026-03-03T11:00")).toBe("");
    expect(scheduleError(rolledOver, "2026-03-03T10:00")).toBe("방문 종료시간은 시작시간 이후여야 합니다");
  });

  it("reports an unparseable end", () => {
    expect(scheduleError(START, "25:00")).toBe("방문 종료시간을 올바르게 입력하세요");
  });

  it("never leaks the engine's English RangeError text", () => {
    for (const [start, end] of [["", ""], ["", START], [START, ""], ["abc", "abc"], ["   ", START]]) {
      const message = scheduleError(start, end);
      expect(message).not.toBe("");
      expect(message).not.toMatch(/[A-Za-z]/);
      expect(() => toBody(start, end)).toThrow(RangeError);
    }
  });

  // The server rejects with !in.EndAt.After(in.StartAt) — equal is not after.
  it("rejects an end equal to the start with the server's wording", () => {
    expect(scheduleError(START, START)).toBe("방문 종료시간은 시작시간 이후여야 합니다");
  });

  it("rejects an end before the start", () => {
    expect(scheduleError(START, plusMinutes(START, -1))).toBe("방문 종료시간은 시작시간 이후여야 합니다");
  });

  it("accepts a one minute visit", () => {
    expect(scheduleError(START, plusMinutes(START, 1))).toBe("");
  });

  // The server rejects only EndAt.Sub(StartAt) > 31*24*time.Hour, so exactly 31 days passes.
  it("accepts exactly 31 days, which the server accepts", () => {
    const end = plusMinutes(START, 31 * 24 * 60);
    expect(new Date(end).getTime() - new Date(START).getTime()).toBe(31 * 24 * 60 * 60000);
    expect(scheduleError(START, end)).toBe("");
  });

  it("rejects 31 days and one minute with the server's wording", () => {
    expect(scheduleError(START, plusMinutes(START, 31 * 24 * 60 + 1))).toBe("한 방문 일정은 31일을 초과할 수 없습니다");
  });

  it("accepts one minute short of 31 days", () => {
    expect(scheduleError(START, plusMinutes(START, 31 * 24 * 60 - 1))).toBe("");
  });

  it("lets every accepted range reach the server body unharmed", () => {
    for (const minutes of [1, 60, 24 * 60, 31 * 24 * 60]) {
      const end = plusMinutes(START, minutes);
      expect(scheduleError(START, end)).toBe("");
      expect(toBody(START, end).endAt).toBe(new Date(end).toISOString());
    }
  });
});

// VisitsPage 의 「방문 일정 수정」 저장 버튼은 같은 계약을 인라인으로 다시 쓰면서
// 31일 상한과 공백만 입력한 방문 목적을 빼먹었고, 잠긴 이유도 말하지 않았다.
// 안내 문구·버튼 disabled·saveEdit 가드가 읽을 한 값을 여기서 만든다.
describe("visitEditError", () => {
  it("accepts an unchanged schedule with a purpose", () => {
    expect(visitEditError(START, plusMinutes(START, 120), "협력사 미팅")).toBe("");
  });

  it("reports a cleared end time", () => {
    expect(visitEditError(START, "", "협력사 미팅")).toBe("방문 종료시간을 올바르게 입력하세요");
  });

  it("reports an end that is not after the start", () => {
    expect(visitEditError(START, START, "협력사 미팅")).toBe("방문 종료시간은 시작시간 이후여야 합니다");
  });

  // 서버 updateVisit 는 31일을 넘는 창을 schedule_too_long 으로 거절하는데,
  // 인라인 검사에는 이 경계가 없어 저장 버튼이 열린 채 왕복 오류가 났다.
  it("reports a window longer than the server accepts", () => {
    expect(visitEditError(START, plusMinutes(START, 31 * 24 * 60 + 1), "협력사 미팅")).toBe("한 방문 일정은 31일을 초과할 수 없습니다");
  });

  it("accepts the longest window the server accepts", () => {
    expect(visitEditError(START, plusMinutes(START, 31 * 24 * 60), "협력사 미팅")).toBe("");
  });

  // 서버는 strings.TrimSpace(in.Purpose)=="" 를 invalid_visit 으로 거절하지만
  // !edit?.purpose 는 공백 문자열을 통과시켰다.
  it("reports a purpose that is only whitespace", () => {
    expect(visitEditError(START, plusMinutes(START, 120), "   ")).toBe("방문 목적을 입력하세요");
  });

  it("reports an empty purpose", () => {
    expect(visitEditError(START, plusMinutes(START, 120), "")).toBe("방문 목적을 입력하세요");
  });

  // 한 줄만 보여 주므로 순서가 계약이다 — 화면을 위에서 아래로 읽는 순서로
  // 시작·종료를 먼저 보고 그다음 방문 목적을 본다.
  it("reports the schedule before the purpose when both are wrong", () => {
    expect(visitEditError(START, "", "")).toBe("방문 종료시간을 올바르게 입력하세요");
  });

  // saveEdit 는 본문을 정확히 이 식으로 만든다. "문구 없음" 은 두 값이
  // toISOString() 을 통과한다는 뜻이어야 한다.
  it("only stays silent when the PUT body survives toISOString", () => {
    for (const [start, end] of [[START, plusMinutes(START, 120)], [START, plusMinutes(START, 31 * 24 * 60)]]) {
      expect(visitEditError(start, end, "협력사 미팅")).toBe("");
      expect(() => toBody(start, end)).not.toThrow();
    }
  });
});
