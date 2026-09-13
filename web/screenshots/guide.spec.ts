import { expect, test, type Page } from "@playwright/test";
import path from "node:path";
import { fileURLToPath } from "node:url";

// Captures the screens the user and administrator guides show. Everything on
// screen comes from the demo data seeded below (fake names, example.com
// addresses, 010-0000-xxxx numbers), so a capture never leaks a real visitor.
//
// This run creates visits, users, a visit type, a guide post and a kiosk
// device. It does not touch system settings, but the seeded rows stay behind,
// so it refuses to run against anything but an explicitly named throwaway
// instance:
//   VISITFLOW_SCREENSHOT_URL       base URL of the instance (required)
//   VISITFLOW_SCREENSHOT_ADMIN     bootstrap administrator username (required)
//   VISITFLOW_SCREENSHOT_PASSWORD  its password (required)
//   VISITFLOW_SCREENSHOT_ALLOW_REMOTE=1  allow a non-loopback URL
//   VISITFLOW_SCREENSHOT_OUT       output directory (default docs/assets/guide)

const BASE = process.env.VISITFLOW_SCREENSHOT_URL ?? "";
const ADMIN = process.env.VISITFLOW_SCREENSHOT_ADMIN ?? "";
const PASSWORD = process.env.VISITFLOW_SCREENSHOT_PASSWORD ?? "";
const OUT = process.env.VISITFLOW_SCREENSHOT_OUT ?? fileURLToPath(new URL("../../docs/assets/guide", import.meta.url));

function guard() {
  const missing = [
    ["VISITFLOW_SCREENSHOT_URL", BASE],
    ["VISITFLOW_SCREENSHOT_ADMIN", ADMIN],
    ["VISITFLOW_SCREENSHOT_PASSWORD", PASSWORD],
  ].filter(([, value]) => !value).map(([name]) => name);
  if (missing.length) throw new Error(`guide screenshots need ${missing.join(", ")}; refusing to guess a target`);
  const host = new URL(BASE).hostname;
  if (!["127.0.0.1", "localhost", "::1"].includes(host) && process.env.VISITFLOW_SCREENSHOT_ALLOW_REMOTE !== "1")
    throw new Error(`${BASE} is not a loopback address; set VISITFLOW_SCREENSHOT_ALLOW_REMOTE=1 only for a throwaway instance`);
}

async function shot(page: Page, name: string, fullPage = false) {
  await page.waitForLoadState("networkidle");
  // Only spinners mean "still loading"; the admin dashboard draws meters as progress bars.
  await expect(page.locator(".MuiCircularProgress-root")).toHaveCount(0);
  await page.waitForTimeout(400);
  await page.screenshot({ path: path.join(OUT, `${name}.png`), fullPage });
}

// Calls the JSON API from inside the signed-in page so the session cookie and
// CSRF token are the browser's own.
async function call<T>(page: Page, method: string, apiPath: string, body?: unknown): Promise<T> {
  return page.evaluate(async ({ method, apiPath, body }) => {
    const me = await fetch("/api/v1/auth/me").then((r) => r.json());
    const response = await fetch(`/api/v1${apiPath}`, {
      method,
      headers: { "Content-Type": "application/json", "X-CSRF-Token": me.csrfToken },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!response.ok) throw new Error(`${method} ${apiPath} → ${response.status} ${await response.text()}`);
    return response.status === 204 ? null : response.json();
  }, { method, apiPath, body }) as Promise<T>;
}

const at = (hoursFromNow: number, minutes = 0) => {
  const t = new Date(Date.now() + hoursFromNow * 3_600_000);
  t.setMinutes(minutes, 0, 0);
  return t.toISOString();
};

type Reference = { sites: { id: string; name: string }[]; lobbies: { id: string; siteId: string }[]; departments?: { name: string }[] };

test("captures the guide screens", async ({ page, context }) => {
  guard();

  // ── Login ───────────────────────────────────────────────────────────────
  await page.goto("/login");
  await page.getByLabel("아이디").fill(ADMIN);
  await shot(page, "login");
  await page.getByLabel("비밀번호").fill(PASSWORD);
  await page.getByRole("button", { name: "로그인", exact: true }).click();
  await expect(page.getByRole("heading", { name: /방문 일정/ })).toBeVisible();

  // ── Seed demo data ──────────────────────────────────────────────────────
  const reference = await call<Reference>(page, "GET", "/reference-data");
  const site = reference.sites[0];
  const lobby = reference.lobbies.find((l) => l.siteId === site.id);

  // The seed migration ships a "협력사 작업" type that forces approval; reuse
  // it so the approval queue has something to show.
  const types = await call<{ items?: { id: string; code: string }[] } | { id: string; code: string }[]>(page, "GET", "/admin/visit-types");
  const typeList = Array.isArray(types) ? types : (types.items ?? []);
  const visitType = typeList.find((t) => t.code === "CONTRACTOR") ?? typeList[0];

  const visitor = (name: string, phone: string, company: string, extra: Record<string, unknown> = {}) =>
    ({ name, phone, company, consent: true, locale: "ko", ...extra });

  const base = { siteId: site.id, lobbyId: lobby?.id ?? "", checklist: {} };
  const arrived = await call<{ id: string; passUrls?: string[] }>(page, "POST", "/visits", {
    ...base, startAt: at(-0.5), endAt: at(3), purpose: "분기 협력 회의", placeDetail: "본관 3층 회의실 A",
    visitors: [visitor("홍길동", "010-0000-1001", "데모물산", { title: "과장", email: "hong@example.com" }), visitor("김영희", "010-0000-1002", "데모물산")],
  });
  const later = await call<{ id: string; passUrls?: string[] }>(page, "POST", "/visits", {
    ...base, startAt: at(2), endAt: at(4), purpose: "채용 면접", placeDetail: "본관 1층 면접실",
    visitors: [visitor("이철수", "010-0000-1003", "개인", { locale: "en" })],
  });
  await call(page, "POST", "/visits", {
    ...base, startAt: at(26), endAt: at(28), purpose: "장비 점검", placeDetail: "2공장 설비동",
    visitors: [visitor("박민수", "010-0000-1004", "데모테크", { vehicle: "12가3456" })],
  });
  await call(page, "POST", "/visits", {
    ...base, visitTypeId: visitType.id, startAt: at(50), endAt: at(54), purpose: "서버실 랙 설치",
    placeDetail: "본관 지하 전산실", notes: "지게차 반입 예정",
    checklist: { nda: true, safetyBriefing: true },
    visitors: [visitor("최지우", "010-0000-1005", "데모엔지니어링", { equipment: ["노트북", "계측기"] })],
  });

  // Posts and watch-list rows are keyed by nothing, so re-running the capture
  // against the same instance would stack duplicates; seed them once.
  const guides = await call<{ items?: { title: string }[] }>(page, "GET", "/admin/guides");
  if (!(guides.items ?? []).some((g) => g.title.startsWith("방문자 안내"))) {
    await call(page, "POST", "/admin/guides", {
      title: "방문자 안내 — 주차와 출입증", category: "출입 안내", published: true, pinned: true,
      content: "방문 차량은 본관 지하 1층 방문자 주차 구역을 이용합니다.\n\n출입증은 로비에서 QR 확인 후 발급되며 퇴실 시 반납합니다.",
    });
    await call(page, "POST", "/admin/guides", {
      title: "비상 대피 경로", category: "안전", published: true, pinned: false,
      content: "화재 경보 시 가장 가까운 비상 계단으로 이동해 1층 정문 앞 집결지로 모입니다.",
    });
  }
  const watchlist = await call<{ items?: { company: string }[] }>(page, "GET", "/admin/watchlist");
  if (!(watchlist.items ?? []).some((w) => w.company === "거래정지상사"))
    await call(page, "POST", "/admin/watchlist", { name: "", phone: "", company: "거래정지상사", reason: "계약 위반으로 출입 제한" });

  if (!(reference.departments ?? []).some((d) => d.name === "총무팀"))
    await call(page, "POST", "/admin/organizations", { name: "총무팀", parentId: "", color: "#176B5B" });

  // One fake SMS gateway and one rule so the notification screen is not a pair
  // of empty tables. The gateway host does not exist; nothing is ever sent.
  const apis = await call<{ items?: { name: string; id: string }[] }>(page, "GET", "/admin/notification-apis");
  let gateway = (apis.items ?? []).find((a) => a.name === "데모 문자 Gateway");
  if (!gateway) {
    gateway = await call<{ id: string; name: string }>(page, "POST", "/admin/notification-apis", {
      name: "데모 문자 Gateway", channel: "sms", baseUrl: "https://sms.example.com", path: "/v1/send", method: "POST",
      requestFormat: "json", headers: {}, secretKeys: [],
      parameters: { to: "{{recipient}}", text: "{{message}}", ref: "{{idempotencyKey}}" }, timeoutSeconds: 10, enabled: true,
    });
    await call(page, "POST", "/admin/notification-rules", {
      name: "방문 확정 안내 (방문자)", event: "visit_confirmed", audience: "visitor", channel: "sms", apiConfigId: gateway!.id,
      offsetMinutes: 0, templateKey: "visit_confirmed_ko", locale: "ko", enabled: true,
      bodyTemplate: "[{{company}}] {{visitor}}님, {{start}} {{place}} 방문이 확정되었습니다. 방문증: {{passUrl}}",
    });
  }

  // A throwaway lobby account so the role table has more than one row. The
  // password is random and never used again.
  const lobbyPassword = `Lobby-${Math.random().toString(36).slice(2, 10)}-${Date.now()}`;
  await call(page, "POST", "/admin/users", {
    username: "lobby01", displayName: "로비 담당자", email: "lobby@example.com", role: "lobby",
    departmentId: "", siteScope: [site.id], password: lobbyPassword,
  }).catch((error: Error) => {
    if (!error.message.includes("409")) throw error;
  });

  // Check the first party in so the lobby, roster and admin screens show a
  // visitor who is actually on site.
  const passUrl = arrived.passUrls?.[0] ?? "";
  const token = passUrl.slice(passUrl.lastIndexOf("/q/") + 3);
  await call(page, "POST", "/qr/verify", { token });
  await call(page, "POST", "/checkins", { token, lobbyId: lobby?.id ?? "", badgeNo: "V-0117" });

  const detail = await call<{ visitors: { id: string }[] }>(page, "GET", `/visits/${later.id}`);
  const invitation = await call<{ registrationUrl: string }>(page, "POST", `/visitor-visits/${detail.visitors[0].id}/invitation`, {});

  // ── Personal screens ────────────────────────────────────────────────────
  await page.goto("/");
  await shot(page, "dashboard");

  await page.goto("/visits/new");
  await page.getByLabel(/^방문 목적/).fill("신제품 시연");
  await page.getByLabel(/^이름/).first().fill("홍길동");
  await page.getByLabel(/^휴대전화/).first().fill("010-0000-1001");
  await page.getByLabel(/^회사명/).first().fill("데모물산");
  // Filling the last field scrolls the form; capture the whole page from the top.
  await page.evaluate(() => window.scrollTo(0, 0));
  await shot(page, "visits-new", true);

  await page.goto("/visits");
  await expect(page.getByText("분기 협력 회의").first()).toBeVisible();
  await shot(page, "visits");
  await page.getByRole("button", { name: "상세" }).first().click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await shot(page, "visits-detail");
  await page.keyboard.press("Escape");

  await page.goto("/templates");
  await shot(page, "templates");

  await page.goto("/guides");
  await expect(page.getByText("방문자 안내 — 주차와 출입증").first()).toBeVisible();
  await shot(page, "guides");

  await page.goto("/approvals");
  await expect(page.getByText("서버실 랙 설치").first()).toBeVisible();
  await shot(page, "approvals");
  await page.getByRole("button", { name: "검토" }).first().click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await shot(page, "approvals-review");
  await page.keyboard.press("Escape");

  await page.goto("/profile/keys");
  await shot(page, "profile-keys");

  // ── Lobby screens ───────────────────────────────────────────────────────
  await page.goto("/lobby");
  await expect(page.getByText("홍길동").first()).toBeVisible();
  await shot(page, "lobby");

  await page.goto("/lobby/scan");
  const field = page.getByLabel(/^QR URL 또는 Token/);
  // The second member of the party that already arrived has not scanned yet.
  await field.fill(arrived.passUrls?.[1] ?? "");
  await field.press("Enter");
  await expect(page.getByRole("heading", { name: "유효한 방문증" })).toBeVisible();
  await shot(page, "lobby-scan-valid");

  await page.goto("/lobby/walk-in");
  await shot(page, "lobby-walk-in");

  await page.goto("/lobby/roster");
  await expect(page.getByText("홍길동").first()).toBeVisible();
  await shot(page, "lobby-roster");

  // ── Administrator screens ───────────────────────────────────────────────
  // Enrol the kiosk first so the device table is not empty on the operations screen.
  const enrolled = await call<{ enrollPath: string }>(page, "POST", "/admin/kiosk-devices", {
    name: "본관 로비 태블릿", siteId: site.id, lobbyId: lobby?.id ?? "", validDays: 30,
  });

  await page.goto("/admin/dashboard");
  await shot(page, "admin-dashboard");
  await page.goto("/admin/visits");
  await shot(page, "admin-visits");
  await page.goto("/admin/resources");
  await shot(page, "admin-resources");
  await page.goto("/admin/statistics");
  await shot(page, "admin-statistics");
  await page.goto("/admin/notification-settings");
  await shot(page, "admin-notification-settings");
  await page.goto("/admin/operations");
  await shot(page, "admin-operations");
  await page.goto("/admin/guides");
  await shot(page, "admin-guides");
  await page.goto("/admin/audit");
  await shot(page, "admin-audit");
  await page.goto("/admin/settings");
  await shot(page, "admin-settings-general");
  await page.getByRole("tab", { name: "방문 · QR 정책" }).click();
  await shot(page, "admin-settings-visit");
  await page.getByRole("tab", { name: "보안 · 키" }).click();
  await shot(page, "admin-settings-security");
  await page.goto("/admin/api");
  await shot(page, "admin-api");

  // ── Public and device screens (no session) ──────────────────────────────
  const anonymous = await context.browser()!.newContext({ viewport: { width: 1440, height: 900 }, locale: "ko-KR", timezoneId: "Asia/Seoul" });
  const pub = await anonymous.newPage();
  await pub.goto(passUrl);
  await expect(pub.getByRole("img", { name: /방문증|pass/i })).toBeVisible();
  await shot(pub, "mobile-pass");
  await pub.goto(invitation.registrationUrl);
  await shot(pub, "self-registration");
  await pub.goto(new URL(enrolled.enrollPath, BASE).toString());
  await expect(pub.getByRole("heading", { name: "방문증 QR을 스캔해 주세요" })).toBeVisible();
  await shot(pub, "kiosk");
  await anonymous.close();
});
