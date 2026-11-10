import {
  test,
  expect,
  type Page,
  type APIRequestContext,
} from "../../web/node_modules/@playwright/test";
interface Session {
  token: string;
  expiresAt: number;
  user: { id: string; username: string; displayName: string };
}
interface Meeting {
  id: string;
  meetingNo: string;
  title: string;
}
const password = "E2e_password_2026";
let a: Session, b: Session, m: Meeting;
async function account(
  request: APIRequestContext,
  prefix: string,
): Promise<Session> {
  const username =
    prefix + crypto.randomUUID().replaceAll("-", "").slice(0, 12);
  let r = await request.post("/api/v1/auth/register", {
    data: { username, displayName: username, password },
  });
  expect(r.status()).toBe(201);
  r = await request.post("/api/v1/auth/login", {
    data: { username, password },
  });
  expect(r.status()).toBe(200);
  return (await r.json()).data;
}
async function seed(page: Page, session: Session) {
  await page.addInitScript(
    (s) =>
      sessionStorage.setItem(
        "comeet.auth.v1",
        JSON.stringify({ state: { session: s }, version: 0 }),
      ),
    session,
  );
}
test.beforeAll(async ({ request }) => {
  a = await account(request, "e2e_a_");
  b = await account(request, "e2e_b_");
  const r = await request.post("/api/v1/meetings", {
    headers: { Authorization: `Bearer ${a.token}` },
    data: { title: "真实接口测试会议", clientRequestId: crypto.randomUUID() },
  });
  expect(r.status()).toBe(201);
  m = (await r.json()).data;
});
for (const width of [1440, 1024, 768, 390])
  for (const route of [
    "/",
    "/login",
    "/register",
    "/prepare",
    "/room",
    "/history",
    "/history/preview",
  ])
    test(`${width}px ${route} load/refresh`, async ({ page }) => {
      if (route !== "/login" && route !== "/register") await seed(page, a);
      await page.setViewportSize({ width, height: 900 });
      const errors: string[] = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const path =
        route === "/prepare"
          ? `/meetings/${m.id}/prepare`
          : route === "/room"
            ? `/meetings/${m.id}/room`
            : route;
      await page.goto(path);
      await expect(page.locator("h1").first()).toBeVisible();
      await page.reload();
      await expect(page.locator("h1").first()).toBeVisible();
      if (route === "/prepare")
        await expect(
          page.getByRole("heading", { name: m.title }),
        ).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      expect(errors).toEqual([]);
    });
test("anonymous protection, wrong password, preserved return route", async ({
  page,
}) => {
  await page.goto(`/meetings/${m.id}/prepare`);
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("用户名", { exact: true }).fill(a.user.username);
  await page.getByLabel("密码", { exact: true }).fill("wrong-password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("用户名或密码错误");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "显示密码" }).click();
  await expect(page.getByLabel("密码", { exact: true })).toHaveAttribute(
    "type",
    "text",
  );
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/meetings/${m.id}/prepare$`));
  await expect(page.getByRole("heading", { name: m.title })).toBeVisible();
});
test("real UI registration and login", async ({ page }) => {
  const name = "ui_" + crypto.randomUUID().replaceAll("-", "").slice(0, 12);
  await page.goto("/register");
  await page.getByLabel("用户名", { exact: true }).fill(name);
  await page.getByLabel("显示名称", { exact: true }).fill("UI 测试账号");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "注册", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("status")).toContainText("注册成功");
  await page.getByLabel("用户名", { exact: true }).fill(name);
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "工作台", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "暂无会议" })).toBeVisible();
  expect(
    await page.evaluate(() => localStorage.getItem("comeet.auth.v1")),
  ).toBeNull();
});
test("create, database-backed list, refresh and host detail", async ({
  page,
}) => {
  await seed(page, a);
  const title = "UI会议_" + crypto.randomUUID().slice(0, 8);
  await page.goto("/");
  await page.getByLabel("会议标题（可选）").fill(title);
  await page.getByRole("button", { name: "创建会议", exact: true }).click();
  await expect(page).toHaveURL(/\/meetings\/[\w-]+\/prepare$/);
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
  await expect(
    page.locator("strong").filter({ hasText: /\d{3}-\d{3}-\d{3}/ }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: /进入会议/ })).toBeDisabled();
  await page.getByRole("link", { name: "返回工作台" }).click();
  const row = page.locator(".meeting-row").filter({ hasText: title });
  await expect(row).toBeVisible();
  await page.reload();
  await expect(row).toBeVisible();
  await row.getByRole("link", { name: "查看详情" }).click();
  await expect(
    page.getByRole("heading", { name: "会议详情", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: title })).toBeVisible();
});
test("meeting number validation, resolution and cross-user detail forbidden", async ({
  page,
}) => {
  await seed(page, b);
  await page.goto("/");
  await page.getByLabel("会议号", { exact: true }).fill("123");
  await page.getByRole("button", { name: "查询会议" }).click();
  await expect(page.getByRole("alert")).toContainText("9 位数字");
  await page.getByLabel("会议号", { exact: true }).fill("000000000");
  await page.getByRole("button", { name: "查询会议" }).click();
  await expect(page.getByRole("alert")).toContainText("会议不存在");
  await page
    .getByLabel("会议号", { exact: true })
    .fill(m.meetingNo.replace(/(\d{3})(\d{3})(\d{3})/, "$1-$2-$3"));
  await page.getByRole("button", { name: "查询会议" }).click();
  await expect(page.getByRole("heading", { name: m.title })).toBeVisible();
  await page.goto("/meetings/" + m.id);
  await expect(page.getByRole("alert")).toContainText(
    "只有主持人或实际参会成员",
  );
  await page.goto("/");
  await expect(
    page.locator(".meeting-row").filter({ hasText: m.title }),
  ).toHaveCount(0);
});
test("logout revokes actual server session", async ({ page, request }) => {
  const s = await account(request, "logout_");
  await seed(page, s);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "暂无会议" })).toBeVisible();
  await page.getByRole("button", { name: "退出登录" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("status")).toContainText("已退出登录");
  const r = await request.get("/api/v1/me", {
    headers: { Authorization: `Bearer ${s.token}` },
  });
  expect(r.status()).toBe(401);
});
test("revoked session on refresh shows expiry and redirects", async ({
  page,
  request,
}) => {
  const s = await account(request, "revoked_");
  await seed(page, s);
  await page.goto(`/meetings/${m.id}/prepare`);
  await expect(page.getByRole("heading", { name: m.title })).toBeVisible();
  await request.post("/api/v1/auth/logout", {
    headers: { Authorization: `Bearer ${s.token}` },
  });
  await page.reload();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("status")).toContainText("登录已过期或已撤销");
});
test("client expiry timer clears identity", async ({ page }) => {
  await seed(page, { ...b, expiresAt: Date.now() + 2200 });
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "工作台", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("status")).toContainText("登录已过期");
});
test("list network error and retry use real service", async ({ page }) => {
  await seed(page, a);
  await page.route("**/api/v1/meetings?**", (r) => r.abort());
  await page.goto("/");
  await expect(page.getByRole("alert")).toContainText("无法连接服务");
  await page.unroute("**/api/v1/meetings?**");
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(
    page.locator(".meeting-row").filter({ hasText: m.title }),
  ).toBeVisible();
});
test("real health after migration and retry", async ({ page, request }) => {
  await seed(page, a);
  await page.route("**/api/v1/health/ready", (r) => r.abort());
  await page.goto("/");
  await expect(page.getByRole("alert")).toContainText("无法连接业务服务");
  await page.unroute("**/api/v1/health/ready");
  await page.getByRole("button", { name: "重新检查" }).click();
  await expect(page.getByText("● 数据库初始化正常")).toBeVisible();
  const r = await request.get("/api/v1/health/ready");
  const d = (await r.json()).data;
  expect(d.schemaVersion).toBe("002_accounts_meetings.sql");
  expect(d.stage).toBe(2);
  expect(d.mediaConfigured).toBe(false);
  expect(d.aiConfigured).toBe(false);
});
