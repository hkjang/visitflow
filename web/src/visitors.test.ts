import { describe, expect, it } from "vitest";
import { visitorFieldErrors, visitorsError } from "./visitors";

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
