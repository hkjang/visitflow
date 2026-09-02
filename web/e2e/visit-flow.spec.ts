import { expect, test, type Page } from "@playwright/test";

const ADMIN = process.env.VISITFLOW_E2E_ADMIN ?? "admin";
const PASSWORD = process.env.VISITFLOW_E2E_PASSWORD ?? "e2e-bootstrap-password";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("아이디").fill(ADMIN);
  await page.getByLabel("비밀번호").fill(PASSWORD);
  await page.getByRole("button", { name: "로그인", exact: true }).click();
  await expect(page.getByRole("heading", { name: /방문 일정/ })).toBeVisible();
}

// Creates one visit through the UI and returns the visitor pass URL the success
// screen shows, which the lobby tests then scan.
async function createVisit(page: Page, visitorName: string): Promise<string> {
  await page.goto("/visits/new");
  await page.getByLabel(/^방문 목적/).fill("E2E 자동화 방문");
  await page.getByLabel(/^이름/).first().fill(visitorName);
  await page.getByLabel(/^휴대전화/).first().fill("010-5555-6666");
  await page.getByLabel(/^회사명/).first().fill("E2E QA");
  await page.getByRole("button", { name: "방문 신청 제출" }).click();
  await expect(page.getByRole("heading", { name: "방문 등록 완료" })).toBeVisible();
  const body = await page.locator("body").innerText();
  const match = body.match(/https?:\/\/\S*\/q\/vfq_[A-Za-z0-9_-]+/);
  expect(match, "the success screen must show the visitor pass URL").not.toBeNull();
  return match![0];
}

test.describe("visitor lifecycle", () => {
  test("rejects a wrong password before signing in", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("아이디").fill(ADMIN);
    await page.getByLabel("비밀번호").fill("definitely-not-the-password");
    await page.getByRole("button", { name: "로그인" }).click();
    await expect(page.getByRole("alert")).toContainText("아이디 또는 비밀번호");
  });

  test("registers a visit and lists it", async ({ page }) => {
    await login(page);
    const visitor = `방문객${Date.now() % 100000}`;
    await createVisit(page, visitor);
    await page.goto("/visits");
    await expect(page.getByText(visitor).first()).toBeVisible();
  });

  test("shows the mobile pass and switches language", async ({ page }) => {
    await login(page);
    const passUrl = await createVisit(page, `패스${Date.now() % 100000}`);
    const token = passUrl.slice(passUrl.lastIndexOf("/q/") + 3);
    await page.goto(`/q/${token}`);
    await expect(page.getByRole("img", { name: /방문증|pass/i })).toBeVisible();
    await expect(page.getByText("로비에 이 QR을 제시해 주세요", { exact: false })).toBeVisible();
    await page.getByRole("combobox").click();
    await page.getByRole("option", { name: "English" }).click();
    await expect(page.getByText("Show this QR code at the lobby", { exact: false })).toBeVisible();
  });

  // A USB QR scanner behaves like a keyboard: it types the payload and presses
  // Enter. The scanner screen must complete a check-in from that alone.
  test("checks a visitor in from a keyboard-wedge scan", async ({ page }) => {
    await login(page);
    const visitor = `스캔${Date.now() % 100000}`;
    const passUrl = await createVisit(page, visitor);
    await page.goto("/lobby/scan");
    const field = page.getByLabel(/^QR URL 또는 Token/);
    await field.click();
    await field.fill(passUrl);
    await field.press("Enter");
    await expect(page.getByRole("heading", { name: "유효한 방문증" })).toBeVisible();
    page.once("dialog", (dialog) => void dialog.accept());
    await page.getByRole("button", { name: "체크인 완료" }).click();
    await page.goto("/lobby/roster");
    await expect(page.getByText(visitor).first()).toBeVisible();
  });

  test("prints the emergency roster with the current headcount", async ({ page }) => {
    await login(page);
    await page.goto("/lobby/roster");
    await expect(page.getByRole("heading", { name: "비상 대피 명단 · 현재 체류 방문자" })).toBeVisible();
    await expect(page.getByText(/총 \d+명/)).toBeVisible();
  });
});
