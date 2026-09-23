import { describe, expect, it } from "vitest";
import { scheduleError } from "./schedule";

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
