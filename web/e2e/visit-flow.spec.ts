import { expect, test, type Page, type Request } from "@playwright/test";

const ADMIN = process.env.VISITFLOW_E2E_ADMIN ?? "admin";
const PASSWORD = process.env.VISITFLOW_E2E_PASSWORD ?? "e2e-bootstrap-password";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("아이디").fill(ADMIN);
  await page.getByLabel("비밀번호").fill(PASSWORD);
  await page.getByRole("button", { name: "로그인", exact: true }).click();
  await expect(page.getByRole("heading", { name: /방문 일정/ })).toBeVisible();
}

// CONTRACTOR requires approval without changing the site's approval policy.
// Pending visits have no pass URL, so they cannot use createVisit below.
async function createPendingVisit(page: Page) {
  const suffix = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const purpose = `E2E 승인 ${suffix}`;
  const created = await page.evaluate(async ({ purpose, phone }) => {
    const me = await fetch("/api/v1/auth/me").then((r) => r.json());
    const reference = await fetch("/api/v1/reference-data").then((r) => r.json());
    const types = await fetch("/api/v1/admin/visit-types").then((r) => r.json());
    const contractor = types.items.find((item: { code: string }) => item.code === "CONTRACTOR");
    if (!contractor) throw new Error("CONTRACTOR visit type is missing");
    const response = await fetch("/api/v1/visits", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": me.csrfToken },
      body: JSON.stringify({
        siteId: reference.sites[0].id,
        visitTypeId: contractor.id,
        checklist: { nda: true, safetyBriefing: true },
        startAt: new Date(Date.now() + 30 * 60_000).toISOString(),
        endAt: new Date(Date.now() + 120 * 60_000).toISOString(),
        purpose,
        visitors: [{ name: "승인 테스트 방문자", phone, company: "E2E QA", consent: true }],
      }),
    });
    return { status: response.status, body: await response.json() };
  }, { purpose, phone: `010${String(Date.now()).slice(-8)}` });
  expect(created.status).toBe(201);
  expect(created.body.status).toBe("PENDING_APPROVAL");
  expect(created.body.id).toEqual(expect.any(String));
  return { id: created.body.id as string, purpose };
}

async function readVisit(page: Page, id: string) {
  const detail = await page.evaluate(async (visitId) => {
    const response = await fetch(`/api/v1/visits/${visitId}`);
    return { status: response.status, body: await response.json() };
  }, id);
  expect(detail.status).toBe(200);
  return detail.body.visit;
}

// Creates one visit through the UI and returns both the visitor pass URL the
// lobby tests scan and the visit number, which is the only thing that picks
// this one visit out of the list again.
async function createVisit(page: Page, visitorName: string): Promise<{ passUrl: string; requestNo: string }> {
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
  const number = body.match(/VF-[A-Za-z0-9-]+/);
  expect(number, "the success screen must show the visit number").not.toBeNull();
  return { passUrl: match![0], requestNo: number![0] };
}

// Opens the detail dialog of exactly one visit. The list's search box matches
// the visit number, the company and the host — never the visitor's name — and
// several visits share a company here, so the number is the only input that
// narrows the table to a single row.
async function openVisitDetail(page: Page, requestNo: string) {
  await page.goto("/visits");
  const listed = page.waitForResponse((r) => r.url().includes(`q=${requestNo}`));
  await page.getByPlaceholder("방문번호 / 회사 / 담당자 검색").fill(requestNo);
  await (await listed).finished();
  const row = page.getByRole("row").filter({ hasText: requestNo });
  await expect(row).toHaveCount(1);
  await row.getByRole("button", { name: "상세" }).click();
  await expect(page.getByRole("dialog").getByText(`${requestNo} · 방문 상세`)).toBeVisible();
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

  // The resend button is the one detail action that used to discard the whole
  // response: a refusal left the screen completely unchanged, and a success was
  // announced without reading how many messages were actually registered. The
  // dialog stays open while the visit is cancelled from outside, which is how a
  // second operator or the automatic checkout really takes the visit out of the
  // notifiable statuses while this button is still on screen.
  test("reports what the server said about a notification resend", async ({ page }) => {
    await login(page);
    // The open dialog is on top of the list, so the page-level alerts are inside
    // the modal's aria-hidden subtree; they are located by their text.
    const resendButton = page.getByRole("dialog").getByRole("button", { name: "알림 재발송" });

    const succeeding = await createVisit(page, `재발송${Date.now() % 100000}`);
    await openVisitDetail(page, succeeding.requestNo);
    const queued = page.waitForResponse((r) => r.url().includes("/notifications/resend"));
    await resendButton.click();
    expect((await queued).status()).toBe(200);
    const count = (await (await queued).json()) as { queued: number };
    expect(count.queued).toBeGreaterThan(0);
    await expect(page.getByText(`알림 ${count.queued}건을 다시 등록했습니다`)).toBeVisible();

    // A second visit, so the failure is judged on a freshly mounted list with no
    // alert left over from the success above.
    const refusing = await createVisit(page, `재발송실패${Date.now() % 100000}`);
    await openVisitDetail(page, refusing.requestNo);
    await expect(resendButton).toBeVisible();
    const cancelled = await page.evaluate(async (no) => {
      const me = await fetch("/api/v1/auth/me").then((r) => r.json());
      const list = await fetch(`/api/v1/visits?limit=100&q=${encodeURIComponent(no)}`).then((r) => r.json());
      const visit = list.items.find((item: { requestNo: string }) => item.requestNo === no);
      if (!visit) throw new Error(`visit ${no} is not in its own search result`);
      const response = await fetch(`/api/v1/visits/${visit.id}/cancel`, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": me.csrfToken },
        body: "{}",
      });
      return response.status;
    }, refusing.requestNo);
    expect(cancelled).toBe(204);

    const refused = page.waitForResponse((r) => r.url().includes("/notifications/resend"));
    await resendButton.click();
    expect((await refused).status()).toBe(409);
    await expect(page.getByText("진행 중인 방문만 방문 안내를 재발송할 수 있습니다")).toBeVisible();
    await expect(page.getByText("다시 등록했습니다")).toHaveCount(0);
  });

  test("shows the mobile pass and switches language", async ({ page }) => {
    await login(page);
    const { passUrl } = await createVisit(page, `패스${Date.now() % 100000}`);
    const token = passUrl.slice(passUrl.lastIndexOf("/q/") + 3);
    await page.goto(`/q/${token}`);
    await expect(page.getByRole("img", { name: /방문증|pass/i })).toBeVisible();
    await expect(page.getByText("로비에 이 QR을 제시해 주세요", { exact: false })).toBeVisible();
    await page.getByRole("combobox").click();
    await page.getByRole("option", { name: "English" }).click();
    await expect(page.getByText("Show this QR code at the lobby", { exact: false })).toBeVisible();
  });

  test("keeps a cancelled approval pending and allows an empty approval memo", async ({ page }) => {
    await login(page);
    const visit = await createPendingVisit(page);
    const path = `/api/v1/visits/${visit.id}/approve`;
    const requests: Request[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && new URL(request.url()).pathname === path) requests.push(request);
    });
    await page.goto("/approvals");
    const row = page.getByRole("row").filter({ hasText: visit.purpose });
    await expect(row).toHaveCount(1);
    page.once("dialog", (dialog) => void dialog.dismiss());
    await row.getByRole("button", { name: "승인", exact: true }).click();
    const detail = await readVisit(page, visit.id);
    expect.soft(requests, "cancelling the memo must not POST an approval").toHaveLength(0);
    expect.soft(detail.status).toBe("PENDING_APPROVAL");

    // Re-enter to prove the server still offers this visit for approval.
    await page.goto("/visits");
    await page.goto("/approvals");
    await expect(row).toHaveCount(1);
    expect((await readVisit(page, visit.id)).status).toBe("PENDING_APPROVAL");
    expect(requests).toHaveLength(0);
    await expect(row.getByRole("button", { name: "승인", exact: true })).toBeEnabled();
    const approved = page.waitForResponse((r) => r.request().method() === "POST" && new URL(r.url()).pathname === path);
    page.once("dialog", (dialog) => void dialog.accept(""));
    await row.getByRole("button", { name: "승인", exact: true }).click();
    expect((await approved).status()).toBe(204);
    expect(requests).toHaveLength(1);
    expect(requests[0].postDataJSON()).toEqual({ reason: "" });
    expect((await readVisit(page, visit.id)).status).toBe("SCHEDULED");
    await expect(row).toHaveCount(0);
  });

  test("preserves a confirmed approval memo in the request and visit detail", async ({ page }) => {
    await login(page);
    const visit = await createPendingVisit(page);
    const memo = "  작업 일정 확인 완료  ";
    const path = `/api/v1/visits/${visit.id}/approve`;
    await page.goto("/approvals");
    const row = page.getByRole("row").filter({ hasText: visit.purpose });
    await expect(row).toHaveCount(1);
    const approved = page.waitForResponse((r) => r.request().method() === "POST" && new URL(r.url()).pathname === path);
    page.once("dialog", (dialog) => void dialog.accept(memo));
    await row.getByRole("button", { name: "승인", exact: true }).click();
    const response = await approved;
    expect(response.status()).toBe(204);
    expect(response.request().postDataJSON()).toEqual({ reason: memo });
    expect(await readVisit(page, visit.id)).toMatchObject({ status: "SCHEDULED", approvalReason: memo });
    await expect(row).toHaveCount(0);
  });

  test("requires a confirmed nonblank reason to reject a pending visit", async ({ page }) => {
    await login(page);
    const visit = await createPendingVisit(page);
    const path = `/api/v1/visits/${visit.id}/reject`;
    const requests: Request[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && new URL(request.url()).pathname === path) requests.push(request);
    });
    await page.goto("/approvals");
    const row = page.getByRole("row").filter({ hasText: visit.purpose });
    await expect(row).toHaveCount(1);
    for (const reason of [null, "", "   "]) {
      page.once("dialog", (dialog) => void (reason === null ? dialog.dismiss() : dialog.accept(reason)));
      await row.getByRole("button", { name: "반려", exact: true }).click();
      expect((await readVisit(page, visit.id)).status).toBe("PENDING_APPROVAL");
      expect(requests).toHaveLength(0);
      await expect(row.getByRole("button", { name: "반려", exact: true })).toBeEnabled();
    }
    const reason = "작업 일정 재협의 필요";
    const rejected = page.waitForResponse((r) => r.request().method() === "POST" && new URL(r.url()).pathname === path);
    page.once("dialog", (dialog) => void dialog.accept(reason));
    await row.getByRole("button", { name: "반려", exact: true }).click();
    expect((await rejected).status()).toBe(204);
    expect(requests).toHaveLength(1);
    expect(requests[0].postDataJSON()).toEqual({ reason });
    expect(await readVisit(page, visit.id)).toMatchObject({ status: "REJECTED", approvalReason: reason });
    await expect(row).toHaveCount(0);
  });

  // A USB QR scanner behaves like a keyboard: it types the payload and presses
  // Enter. The scanner screen must complete a check-in from that alone.
  test("checks a visitor in from a keyboard-wedge scan", async ({ page }) => {
    await login(page);
    const visitor = `스캔${Date.now() % 100000}`;
    const { passUrl } = await createVisit(page, visitor);
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
    const { passUrl } = await createVisit(page, visitor);
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

// The evacuation roster is printed and carried out of the building, so the paper
// has to say how much it can be trusted. These specs block the service worker:
// it answers /api/v1/lobby/roster from Cache Storage and would hide the very
// failure under test (web/public/sw.js), and blocking it only inside this
// describe leaves every other spec on the default registration.
test.describe("emergency roster trust", () => {
  test.use({ serviceWorkers: "block" });

  test("refuses to report an unreachable roster as an empty building", async ({ page }) => {
    await login(page);
    await page.evaluate(() => window.localStorage.removeItem("visitflow_last_roster"));
    await page.route("**/api/v1/lobby/roster", (route) => route.abort());
    await page.goto("/lobby/roster");

    await expect(page.getByRole("heading", { name: "비상 대피 명단 · 현재 체류 방문자" })).toBeVisible();
    // Never a headcount, and never the sentence that means "nobody is inside".
    await expect(page.getByText("명단 확인 불가")).toBeVisible();
    await expect(page.getByText(/총 \d+명/)).toHaveCount(0);
    await expect(page.getByText("현재 사내에 체류 중인 방문자가 없습니다.")).toHaveCount(0);
    // It must not claim to be showing a roster it never received.
    await expect(page.getByText(/마지막으로 받은 명단을 표시합니다/)).toHaveCount(0);

    const warning = page.getByText(/명단을 불러오지 못해 지금 건물 안에 몇 명이 있는지 확인할 수 없습니다/);
    await expect(warning).toBeVisible();
    await expect(page.getByText(/체류 중인 방문자가 없다는 뜻이 아니므로/)).toBeVisible();
    await page.emulateMedia({ media: "print" });
    await expect(warning).toBeVisible();
    // The paper still must not carry the on-screen controls.
    await expect(page.getByRole("button", { name: "새로고침" })).toBeHidden();
    await expect(page.getByRole("button", { name: "인쇄" })).toBeHidden();
    await expect(page.getByRole("heading", { name: "비상 대피 명단", exact: true })).toBeHidden();
  });

  // The other half of the contract: with a roster in local storage a failed
  // refresh must still print the last list, its own 기준 시각 and a warning that
  // says exactly that — distinguishable from having no roster at all.
  test("keeps the last roster it received when a refresh fails", async ({ page }) => {
    await login(page);
    await page.goto("/lobby/roster");
    await expect(page.getByText(/총 \d+명/)).toBeVisible();
    const chip = page.locator(".MuiChip-label").first();
    const count = await chip.innerText();
    const asOf = await page.getByText(/^기준 시각/).innerText();

    await page.route("**/api/v1/lobby/roster", (route) => route.abort());
    await page.reload();

    await expect(page.getByText(/마지막으로 받은 명단을 표시합니다/)).toBeVisible();
    await expect(chip).toHaveText(count);
    await expect(page.getByText(/^기준 시각/)).toHaveText(asOf);
    await expect(page.getByText("명단 확인 불가")).toHaveCount(0);
  });

  test("prints a live roster without any trust warning", async ({ page }) => {
    await login(page);
    await page.goto("/lobby/roster");
    await expect(page.getByText(/총 \d+명/)).toBeVisible();
    await expect(page.locator(".MuiAlert-root")).toHaveCount(0);
    await page.emulateMedia({ media: "print" });
    await expect(page.locator(".MuiAlert-root")).toHaveCount(0);
  });
});

// The third source: the service worker answers from Cache Storage and marks the
// body `offline: true` (web/public/sw.js). This spec leaves the worker on and
// drops the real network, so the page reads the worker's own response.
test.describe("emergency roster offline fallback", () => {
  test("shows the service worker's cached roster as offline, not as a failure", async ({ page, context }) => {
    await login(page);
    await page.goto("/lobby/roster");
    await expect(page.getByText(/총 \d+명/)).toBeVisible();
    // The worker only caches requests it handles, which means it has to be
    // controlling the page before the roster request that gets cached.
    await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
    await page.reload();
    await expect(page.getByText(/총 \d+명/)).toBeVisible();

    await context.setOffline(true);
    try {
      await page.getByRole("button", { name: "새로고침" }).click();
      await expect(page.getByText(/오프라인 상태입니다/)).toBeVisible();
      // An offline roster is still a roster: the headcount stays and the screen
      // never falls back to the "no roster at all" wording.
      await expect(page.getByText(/총 \d+명/)).toBeVisible();
      await expect(page.getByText("명단 확인 불가")).toHaveCount(0);
      await expect(page.getByText(/마지막으로 받은 명단을 표시합니다/)).toHaveCount(0);
    } finally {
      await context.setOffline(false);
    }
  });
});
