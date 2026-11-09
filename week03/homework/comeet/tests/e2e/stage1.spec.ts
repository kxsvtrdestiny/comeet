import { test, expect } from "../../web/node_modules/@playwright/test";
const routes = [
  ["/", "留一点空间，给好好沟通。"],
  ["/login", "欢迎回来"],
  ["/register", "创建 Comeet 账号"],
  ["/meetings/preview/prepare", "入会前，准备一下。"],
  ["/meetings/preview/room", "等待连接会议"],
  ["/history", "历史会议"],
  ["/history/preview", "会议记录详情"],
];
for (const width of [1440, 1024, 768, 390]) {
  for (const [path, text] of routes) {
    test(`${width}px ${path} direct load and refresh`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      const errors: string[] = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.goto(path);
      await expect(
        page.getByText(text, { exact: false }).first(),
      ).toBeAttached();
      await page.reload();
      await expect(page.locator("main, .room").first()).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      ).toBe(true);
      expect(errors).toEqual([]);
    });
  }
}
test("skeleton controls and password visibility", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("密码", { exact: true }).fill("example-password");
  await expect(page.getByLabel("密码", { exact: true })).toHaveAttribute(
    "type",
    "password",
  );
  await page.getByRole("button", { name: "显示密码" }).click();
  await expect(page.getByLabel("密码", { exact: true })).toHaveAttribute(
    "type",
    "text",
  );
  await expect(page.getByRole("button", { name: /登录.*第/ })).toBeDisabled();
  await page.goto("/meetings/preview/room");
  await page.getByRole("button", { name: "聊天", exact: true }).click();
  await expect(page.getByText("尚未连接聊天")).toBeVisible();
  await expect(page.getByRole("button", { name: /麦克风/ })).toBeDisabled();
});
test("real Go health via Vite proxy", async ({ page }) => {
  test.skip(
    process.env.SKIP_GO_HEALTH === "1",
    "Go dependency network access blocked",
  );
  await page.goto("/");
  await expect(page.getByText("● 数据库初始化正常")).toBeVisible();
  const r = await page.request.get("/api/v1/health/ready");
  expect(r.status()).toBe(200);
  const b = await r.json();
  expect(b.data.database).toBe("ready");
  expect(b.data.mediaConfigured).toBe(false);
  expect(b.data.aiConfigured).toBe(false);
  expect(b.requestId).toMatch(/^[0-9a-f-]{36}$/);
});
test("health failure and retry recovery against real Go", async ({ page }) => {
  test.skip(
    process.env.SKIP_GO_HEALTH === "1",
    "Go dependency network access blocked",
  );
  await page.route("**/api/v1/health/ready", (route) => route.abort());
  await page.goto("/");
  await expect(page.getByRole("alert")).toContainText("无法连接");
  await page.unroute("**/api/v1/health/ready");
  await page.getByRole("button", { name: "重新检查" }).click();
  await expect(page.getByText("● 数据库初始化正常")).toBeVisible();
});
