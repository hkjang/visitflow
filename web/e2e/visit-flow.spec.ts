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
