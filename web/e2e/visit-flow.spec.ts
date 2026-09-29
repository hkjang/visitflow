import { expect, test, type Page } from "@playwright/test";
import type { LobbyVisitor, ReferenceData } from "../src/types";

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
async function createVisit(page: Page, visitorName: string, siteName?: string): Promise<string> {
  await page.goto("/visits/new");
  if (siteName) {
    await page.getByRole("combobox", { name: "사업장" }).click();
    await page.getByRole("option", { name: `${siteName} ·`, exact: true }).click();
  }
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

  // The kiosk never signs a person in, so its CSRF token must survive the app's
  // failed /auth/me probe; this drives the full enrol → scan → check-in path.
  test("enrols a kiosk tablet and checks a visitor in without a login", async ({ page, context }) => {
    await login(page);
    const visitor = `키오스크${Date.now() % 100000}`;
    const passUrl = await createVisit(page, visitor);
    const enrolled = await page.evaluate(async () => {
      const me = await fetch("/api/v1/auth/me").then((r) => r.json());
      const reference = await fetch("/api/v1/reference-data").then((r) => r.json());
      const response = await fetch("/api/v1/admin/kiosk-devices", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": me.csrfToken },
        body: JSON.stringify({ name: "E2E 키오스크", siteId: reference.sites[0].id, validDays: 1 }),
      });
      return response.json() as Promise<{ enrollPath: string }>;
    });
    const kiosk = await context.newPage();
    await kiosk.context().clearCookies();
    await kiosk.goto(enrolled.enrollPath);
    await expect(kiosk.getByRole("heading", { name: "방문증 QR을 스캔해 주세요" })).toBeVisible();
    const field = kiosk.getByLabel(/^QR URL 또는 Token/);
    await field.fill(passUrl);
    await field.press("Enter");
    await expect(kiosk.getByRole("alert")).toContainText("체크인되었습니다");
  });

  // Master data is saved through per-resource endpoints. Deriving those names in
  // the client once produced /admin/lobbys and every lobby save answered 404, so
  // this drives the real create dialogs.
  test("registers a site, a lobby and an organization from the admin console", async ({ page }) => {
    await login(page);
    await page.goto("/admin/resources");
    const suffix = String(Date.now() % 100000);

    await page.getByRole("button", { name: "추가" }).nth(0).click();
    await expect(page.getByRole("heading", { name: "사업장 추가" })).toBeVisible();
    await page.getByLabel(/^코드/).fill(`S${suffix}`);
    await page.getByLabel(/^이름/).fill(`사업장${suffix}`);
    await page.getByRole("button", { name: "저장" }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByText(`사업장${suffix}`).first()).toBeVisible();

    await page.getByRole("button", { name: "추가" }).nth(1).click();
    await expect(page.getByRole("heading", { name: "로비 추가" })).toBeVisible();
    await page.getByLabel(/^코드/).fill(`L${suffix}`);
    await page.getByLabel(/^이름/).fill(`로비${suffix}`);
    await page.getByRole("button", { name: "저장" }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByText(`로비${suffix}`).first()).toBeVisible();

    await page.getByRole("button", { name: "추가" }).nth(2).click();
    await expect(page.getByRole("heading", { name: "조직 추가" })).toBeVisible();
    await page.getByLabel(/^이름/).fill(`조직${suffix}`);
    await page.getByRole("button", { name: "저장" }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByText(`조직${suffix}`).first()).toBeVisible();

    // Editing an existing lobby uses the same endpoint.
    await page.getByText(`로비${suffix}`).first().click();
    await expect(page.getByRole("heading", { name: "로비 수정" })).toBeVisible();
    await page.getByLabel(/^이름/).fill(`로비${suffix}-수정`);
    await page.getByRole("button", { name: "저장" }).click();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByText(`로비${suffix}-수정`).first()).toBeVisible();
  });

  // The rule editor is seeded from the server's own rule object; sending that
  // back verbatim used to fail the endpoint's strict field check.
  test("creates and then edits a delivery rule", async ({ page }) => {
    await login(page);
    await page.goto("/admin/notification-settings");
    const name = `규칙${Date.now() % 100000}`;
    await page.getByRole("button", { name: "규칙 추가" }).click();
    await page.getByLabel(/^규칙 이름/).fill(name);
    await page.getByLabel(/^Template Key/).fill("e2e_rule");
    await page.getByLabel(/^메시지 본문 템플릿/).fill("{{visitor}}님 {{start}} 방문 안내");
    await page.getByRole("button", { name: "저장" }).click();
    await expect(page.getByText(name).first()).toBeVisible();

    await page.getByRole("row", { name: new RegExp(name) }).getByRole("button", { name: "수정" }).click();
    await page.getByLabel(/^규칙 이름/).fill(`${name}-수정`);
    await page.getByRole("button", { name: "저장" }).click();
    await expect(page.getByText("발송 규칙을 저장했습니다.")).toBeVisible();
    await expect(page.getByText(`${name}-수정`).first()).toBeVisible();
  });

  test("prints the emergency roster with the current headcount", async ({ page }) => {
    await login(page);
    await page.goto("/lobby/roster");
    await expect(page.getByRole("heading", { name: "비상 대피 명단 · 현재 체류 방문자" })).toBeVisible();
    await expect(page.getByText(/총 \d+명/)).toBeVisible();
  });
});

for (const path of ["/visits/new", "/lobby/walk-in"]) {
  test(`company policy blocks missing companies and permits registration on ${path}`, async ({ page }) => {
    await login(page);
    const original = await page.evaluate(async () => {
      const response = await fetch("/api/v1/settings");
      if (!response.ok) throw new Error("settings read failed");
      const data = await response.json();
      return data.items.find((item: { key: string }) => item.key === "visit.company_required").value as string;
    });
    const setPolicy = async (value: string) => {
      const status = await page.evaluate(async (value) => {
        const me = await fetch("/api/v1/auth/me").then((r) => r.json());
        const response = await fetch("/api/v1/settings", {
          method: "PUT", headers: { "Content-Type": "application/json", "X-CSRF-Token": me.csrfToken },
          body: JSON.stringify({ settings: { "visit.company_required": value } }),
        });
        return response.status;
      }, value);
      expect(status).toBe(200);
    };
    const openForm = async () => {
      const referenceResponse = page.waitForResponse((r) => r.url().endsWith("/api/v1/reference-data"));
      await page.goto(path);
      const reference = await (await referenceResponse).json();
      await page.getByLabel(/^방문 목적/).fill("회사 정책 E2E");
      if (path === "/lobby/walk-in") {
        await page.getByLabel(/^방문 담당자 검색/).fill(reference.hosts[0].name);
        await page.getByRole("option").first().click();
      }
      await page.getByLabel(/^이름/).first().fill("정책방문자");
      await page.getByLabel(/^휴대전화/).first().fill("01012345678");
      return reference;
    };
    const submit = page.getByRole("main").getByRole("button", { name: path === "/visits/new" ? "방문 신청 제출" : "현장 방문 등록", exact: true });
    try {
      await setPolicy("true");
      expect((await openForm()).companyRequired).toBe(true);
      const companies = page.getByLabel(/^회사명/);
      await expect(companies.first()).toHaveAttribute("required", "");
      await expect(page.getByText("현재 정책상 회사명은 필수입니다").first()).toBeVisible();
      await expect(submit).toBeDisabled();
      await companies.first().fill("   ");
      await expect(submit).toBeDisabled();
      await companies.first().fill("정상회사");
      await expect(submit).toBeEnabled();
      await page.getByRole("button", { name: "방문자 추가" }).click();
      await page.getByLabel(/^이름/).nth(1).fill("동행방문자");
      await page.getByLabel(/^휴대전화/).nth(1).fill("01098765432");
      await expect(companies.nth(1)).toHaveAttribute("required", "");
      await expect(submit).toBeDisabled();
      await companies.nth(1).fill("동행회사");
      await expect(submit).toBeEnabled();
      const imported = page.waitForResponse((r) => r.url().endsWith("/api/v1/visits/import/preview"));
      await page.locator('input[type="file"]').setInputFiles({
        name: "company-policy.csv", mimeType: "text/csv",
        buffer: Buffer.from("이름,휴대전화,회사명,개인정보동의\n파일대표,01012345678,파일회사,예\n파일동행,01098765432,,예\n"),
      });
      expect((await imported).status()).toBe(200);
      await expect(page.getByLabel(/^이름/).nth(1)).toHaveValue("파일동행");
      await expect(companies.first()).toHaveValue("파일회사");
      await expect(companies.nth(1)).toHaveValue("");
      await expect(submit).toBeDisabled();
      await companies.nth(1).fill("파일동행회사");
      await expect(submit).toBeEnabled();
      await submit.click();
      await expect(page.getByRole("heading", { name: "방문 등록 완료" })).toBeVisible();
      if (path === "/visits/new") {
        const templateName = `회사 정책 템플릿 ${Date.now()}`;
        await page.evaluate(async (name) => {
          const me = await fetch("/api/v1/auth/me").then((r) => r.json());
          const create = async (url: string, body: unknown) => {
            const response = await fetch(url, {
              method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": me.csrfToken },
              body: JSON.stringify(body),
            });
            if (response.status !== 201) throw new Error(`fixture creation failed: ${response.status}`);
            return response.json();
          };
          const visitor = await create("/api/v1/frequent-visitors", {
            name: "템플릿방문자", phone: `010${Date.now().toString().slice(-8)}`, company: "", consent: true, equipment: [],
          });
          await create("/api/v1/visit-templates", {
            name, payload: { purpose: "템플릿 회사 정책" }, frequentVisitorIds: [visitor.id],
          });
        }, templateName);
        await page.goto("/templates");
        await page.locator(".MuiCard-root").filter({ hasText: templateName })
          .getByRole("button", { name: "이 템플릿으로 신청" }).click();
        await expect(page.getByLabel(/^이름/).first()).toHaveValue("템플릿방문자");
        await expect(companies.first()).toHaveValue("");
        await expect(submit).toBeDisabled();
        await companies.first().fill("템플릿회사");
        await expect(submit).toBeEnabled();
        await submit.click();
        await expect(page.getByRole("heading", { name: "방문 등록 완료" })).toBeVisible();
      }
      await setPolicy("false");
      expect((await openForm()).companyRequired).toBe(false);
      await expect(companies.first()).not.toHaveAttribute("required");
      await expect(page.getByText("현재 정책상 회사명은 필수입니다")).toHaveCount(0);
      await expect(submit).toBeEnabled();
      await submit.click();
      await expect(page.getByRole("heading", { name: "방문 등록 완료" })).toBeVisible();
    } finally {
      await setPolicy(original);
    }
  });
}

// Only reference-data failures are injected. Fixtures and successful recovery use
// the real server so authorization, QR verification and SSE remain in the path.
async function recoveryPost(page: Page, path: string, data: unknown) {
  const me = await (await page.request.get("/api/v1/auth/me")).json();
  const response = await page.request.post(path, { data, headers: { "X-CSRF-Token": me.csrfToken } });
  expect(response.ok(), `${path}: ${response.status()}`).toBe(true);
  return response.status() === 204 ? undefined : response.json();
}

async function recoveryFixture(page: Page, lobbyCount = 2) {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
  const site = { code: `R${suffix}`, name: `힣복구사업장${suffix}` };
  const { id: siteId } = await recoveryPost(page, "/api/v1/admin/sites", site);
  for (let index = 0; index < lobbyCount; index++) {
    await recoveryPost(page, "/api/v1/admin/lobbies", {
      siteId, code: `R${index}`, name: `힣복구로비${suffix}-${index}`,
    });
  }
  const username = `recovery-${suffix}`;
  await recoveryPost(page, "/api/v1/admin/users", {
    username, displayName: "복구 로비 담당자", role: "lobby", siteScope: [siteId], password: PASSWORD,
  });
  return { siteId, siteName: site.name, username };
}

async function loginRecoveryUser(page: Page, username: string) {
  await page.context().clearCookies();
  await page.goto("/login");
  await page.getByLabel("아이디").fill(username);
  await page.getByLabel("비밀번호", { exact: true }).fill(PASSWORD);
  await page.getByRole("button", { name: "로그인", exact: true }).click();
  await page.getByLabel("임시 비밀번호", { exact: true }).fill(PASSWORD);
  await page.getByLabel("새 비밀번호 (12자 이상)", { exact: true }).fill(`${PASSWORD}-changed`);
  await page.getByLabel("새 비밀번호 확인", { exact: true }).fill(`${PASSWORD}-changed`);
  await page.getByRole("button", { name: "비밀번호 변경", exact: true }).click();
  await expect(page.getByRole("heading", { name: "새 비밀번호를 설정하세요" })).toHaveCount(0);
}

async function interceptReferenceRecovery(page: Page) {
  let requests = 0;
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/api/v1/reference-data", async (route) => {
    requests++;
    if (requests <= 2) {
      await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: { message: "기준 정보를 불러오지 못했습니다" } }) });
    } else {
      await gate;
      await route.continue();
    }
  });
  return { count: () => requests, release };
}

async function retryReferenceRecovery(page: Page, recovery: Awaited<ReturnType<typeof interceptReferenceRecovery>>) {
  const retry = page.getByRole("button", { name: "다시 불러오기", exact: true });
  const alert = page.getByRole("alert").filter({ has: retry });
  await expect(alert).toContainText("기준 정보를 불러오지 못했습니다");
  await expect(alert.getByRole("button")).toHaveCount(1); // no dismiss button
  expect(recovery.count()).toBe(1);
  await retry.click();
  await expect.poll(recovery.count).toBe(2);
  await expect(retry).toBeEnabled();
  await expect(alert).toContainText("기준 정보를 불러오지 못했습니다");
  await retry.click();
  await expect.poll(recovery.count).toBe(3);
  await expect(retry).toBeDisabled();
  await expect(alert).toContainText("다시 불러오는 중입니다");
  // Real pointer events on a disabled button must not enqueue another request.
  const bounds = (await retry.boundingBox())!;
  await page.mouse.click(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2, { clickCount: 3 });
  expect(recovery.count()).toBe(3);
  const response = page.waitForResponse((r) => r.url().endsWith("/api/v1/reference-data") && r.status() === 200);
  recovery.release();
  const reference = await (await response).json() as ReferenceData;
  await expect(alert).toHaveCount(0);
  expect(recovery.count()).toBe(3);
  return reference;
}

test("scanner reference-data recovery preserves a verified QR and selects the first scoped lobby", async ({ page }) => {
  await login(page);
  const fixture = await recoveryFixture(page);
  const visitor = `복구스캔${Date.now()}`;
  const passUrl = await createVisit(page, visitor, fixture.siteName);
  await loginRecoveryUser(page, fixture.username);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const recovery = await interceptReferenceRecovery(page);
  await page.goto("/lobby/scan");
  const retry = page.getByRole("button", { name: "다시 불러오기" });
  await expect(retry).toBeVisible();
  const field = page.getByLabel(/^QR URL 또는 Token/);
  await field.fill("invalid-qr");
  await field.press("Enter");
  const workError = page.getByRole("alert").filter({ has: page.getByRole("button", { name: "Close" }) });
  await expect(workError).toBeVisible();
  await workError.getByRole("button").click();
  await expect(retry).toBeVisible();
  await field.fill(passUrl);
  await expect(page.getByRole("button", { name: "QR 확인", exact: true })).toBeEnabled();
  await field.press("Enter");
  await expect(page.getByRole("heading", { name: "유효한 방문증" })).toBeVisible();
  const lobby = page.getByRole("combobox", { name: "처리 로비" });
  await expect(lobby.locator("..").locator("input")).toHaveValue("");
  await expect(page.getByRole("button", { name: "체크인 완료" })).toBeEnabled();
  await page.getByLabel("임시 출입증 번호 (선택)").fill("RECOVERY-42");
  const reference = await retryReferenceRecovery(page, recovery);
  expect(reference.sites[0].id).not.toBe(fixture.siteId);
  expect(reference.lobbies[0].siteId).not.toBe(fixture.siteId);
  const allowed = reference.lobbies.filter((item) => item.siteId === fixture.siteId);
  expect(allowed).toHaveLength(2);
  await expect(lobby).toHaveText(allowed[0].name);
  await lobby.click();
  await expect(page.getByRole("option")).toHaveText(allowed.map((item) => item.name));
  await page.keyboard.press("Escape");
  await expect(field).toHaveValue(passUrl);
  await expect(page.getByLabel("임시 출입증 번호 (선택)")).toHaveValue("RECOVERY-42");
  await expect(page.getByRole("heading", { name: "유효한 방문증" })).toBeVisible();
  await expect(page.getByRole("heading", { name: visitor })).toBeVisible();
  const checkin = page.waitForRequest("**/api/v1/checkins");
  page.once("dialog", (dialog) => void dialog.accept());
  await page.getByRole("button", { name: "체크인 완료" }).click();
  expect((await checkin).postDataJSON()).toMatchObject({ lobbyId: allowed[0].id, badgeNo: "RECOVERY-42", token: passUrl });
  await expect(page.getByRole("heading", { name: "유효한 방문증" })).toHaveCount(0);
  expect(errors).toEqual([]);
});

test("lobby reference-data recovery preserves search, tab, rows and the SSE connection", async ({ page }) => {
  await login(page);
  const fixture = await recoveryFixture(page);
  const visitor = `복구현황${Date.now()}`;
  const passUrl = await createVisit(page, visitor, fixture.siteName);
  await loginRecoveryUser(page, fixture.username);
  const errors: string[] = [];
  let streams = 0;
  const queries: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (request.url().endsWith("/api/v1/lobby/stream")) streams++;
    if (/\/api\/v1\/lobby\/(today|current)\?/.test(request.url())) queries.push(request.url());
  });
  const recovery = await interceptReferenceRecovery(page);
  await page.goto("/lobby");
  await expect(page.getByRole("button", { name: "다시 불러오기" })).toBeVisible();
  await expect(page.getByText("실시간 연결됨", { exact: true })).toBeVisible();
  const row = page.locator(".MuiPaper-root").filter({ has: page.getByText(`${visitor} · E2E QA`, { exact: true }) }).filter({ has: page.getByRole("button", { name: "직접 체크인", exact: true }) }).last();
  await row.getByRole("button", { name: "직접 체크인", exact: true }).click();
  await page.getByLabel("신분 확인 방법 / 사유").fill("신분증 확인");
  // Another real check-in makes this open dialog stale and produces a work
  // error without replacing the reference-data error or mocking another API.
  await recoveryPost(page, "/api/v1/checkins", { token: passUrl });
  await page.getByRole("dialog").getByRole("button", { name: "체크인", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "취소" }).click();
  const workError = page.getByRole("alert").filter({ hasText: "예정 상태의 방문자만 직접 체크인할 수 있습니다" });
  await expect(workError).toBeVisible();
  await workError.getByRole("button").click();
  await expect(page.getByRole("button", { name: "다시 불러오기" })).toBeVisible();
  await page.getByRole("tab", { name: /현재 방문자/ }).click();
  const search = page.getByPlaceholder("이름 / 회사 / 담당자 / 부서 / 전화번호 검색");
  await search.fill(visitor);
  await expect(page.getByText(`${visitor} · E2E QA`, { exact: true })).toBeVisible();
  await expect.poll(() => queries.at(-1)).toContain(encodeURIComponent(visitor));
  const before = queries.length;
  const reference = await retryReferenceRecovery(page, recovery);
  await expect(search).toHaveValue(visitor);
  await expect(page.getByRole("tab", { name: /현재 방문자/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByText(`${visitor} · E2E QA`, { exact: true })).toBeVisible();
  expect(queries).toHaveLength(before);
  expect(streams).toBe(1);
  await expect(page.getByText("실시간 연결됨", { exact: true })).toBeVisible();
  const lobby = page.getByRole("combobox", { name: /^로비/ });
  await expect(lobby).toBeVisible();
  await expect(lobby.locator("..").locator("input")).toHaveValue("");
  const allowed = reference.lobbies.filter((item) => item.siteId === fixture.siteId);
  await lobby.click();
  await expect(page.getByRole("option")).toHaveText(["전체 로비", ...allowed.map((item) => item.name)]);
  await page.getByRole("option", { name: allowed[1].name, exact: true }).click();
  await expect.poll(() => queries.at(-1)).toContain(`lobby=${allowed[1].id}`);
  await expect(lobby).toHaveText(allowed[1].name);
  const current = await (await page.request.get(`/api/v1/lobby/current?q=${encodeURIComponent(visitor)}`)).json() as { items: LobbyVisitor[] };
  expect(current.items).toHaveLength(1);
  const refreshed = page.waitForRequest((request) => request.url().includes("/api/v1/lobby/current?") && request.url().includes(`lobby=${allowed[1].id}`));
  await recoveryPost(page, "/api/v1/checkouts", { visitorVisitId: current.items[0].visitorVisitId, method: "lobby" });
  expect((await refreshed).url()).toContain(encodeURIComponent(visitor));
  await expect(lobby).toHaveText(allowed[1].name);
  expect(streams).toBe(1);
  expect(errors).toEqual([]);
});

for (const count of [0, 1]) {
  test(`lobby reference-data recovery accepts a real response with ${count} scoped lobbies`, async ({ page }) => {
    await login(page);
    const fixture = await recoveryFixture(page, count);
    await loginRecoveryUser(page, fixture.username);
    const recovery = await interceptReferenceRecovery(page);
    await page.goto("/lobby");
    const reference = await retryReferenceRecovery(page, recovery);
    expect(reference.lobbies.filter((item) => item.siteId === fixture.siteId)).toHaveLength(count);
    await expect(page.getByRole("combobox", { name: /^로비/ })).toHaveCount(0);
    await expect(page.getByRole("alert")).toHaveCount(0);
  });
}
