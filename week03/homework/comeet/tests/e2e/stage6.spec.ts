import { test, expect } from "../../web/node_modules/@playwright/test";
let sessions: any[] = [];
test.beforeAll(async ({ request }) => {
  for (const displayName of ["记录 A", "记录 B", "未参会 C"]) {
    const username =
        "s6_" + crypto.randomUUID().replaceAll("-", "").slice(0, 10),
      password = "Stage6_test_password";
    expect(
      (
        await request.post("/api/v1/auth/register", {
          data: { username, displayName, password },
        })
      ).status(),
    ).toBe(201);
    const r = await request.post("/api/v1/auth/login", {
      data: { username, password },
    });
    expect(r.status()).toBe(200);
    sessions.push((await r.json()).data);
  }
});
async function signed(browser: any, index: number) {
  const c = await browser.newContext();
  await c.addInitScript(
    (s: any) =>
      sessionStorage.setItem(
        "comeet.auth.v1",
        JSON.stringify({ state: { session: s }, version: 0 }),
      ),
    sessions[index],
  );
  return c;
}
const auth = (i: number) => ({ Authorization: "Bearer " + sessions[i].token });
async function create(request: any) {
  const r = await request.post("/api/v1/meetings", {
    headers: auth(0),
    data: {
      title: "第六阶段历史_" + crypto.randomUUID().slice(0, 6),
      clientRequestId: crypto.randomUUID(),
    },
  });
  expect(r.status()).toBe(201);
  return (await r.json()).data;
}
test("real records, chat, private notes, conflict resolution, search and outsider denial", async ({
  browser,
  request,
}) => {
  const m = await create(request),
    ca = await signed(browser, 0),
    cb = await signed(browser, 1),
    cc = await signed(browser, 2);
  try {
    const a = await ca.newPage(),
      b = await cb.newPage();
    for (const p of [a, b]) {
      await p.goto(`/meetings/${m.id}/room`);
      await expect(p.locator(".member-tile").first()).toBeVisible();
    }
    await expect(a.locator(".member-tile")).toHaveCount(2);
    await a.locator(".toolbar").getByRole("button", { name: /^聊天/ }).click();
    await a.getByLabel("消息", { exact: true }).fill("第六阶段真实聊天记录");
    await a.getByRole("button", { name: "发送", exact: true }).click();
    await expect(a.locator(".chat-message small")).toHaveText("已发送");
    await a.getByRole("button", { name: "会议笔记", exact: true }).click();
    await a
      .getByLabel("我的笔记", { exact: true })
      .fill("会议中保存，不断开媒体");
    await a.getByRole("button", { name: "保存笔记", exact: true }).click();
    await expect(
      a.getByRole("status").filter({ hasText: "已保存" }),
    ).toBeVisible();
    await a.getByRole("button", { name: "关闭对话框" }).click();
    a.once("dialog", (d) => d.accept());
    await a.getByRole("button", { name: "结束会议", exact: true }).click();
    await expect(a).toHaveURL("/");
    await expect(b).toHaveURL("/");
    await a.goto("/history");
    await a.getByLabel("搜索历史会议", { exact: true }).fill(m.title);
    await a.getByRole("button", { name: "搜索历史", exact: true }).click();
    await a.getByRole("link", { name: m.title, exact: true }).click();
    await expect(
      a.getByText("第六阶段真实聊天记录", { exact: true }),
    ).toBeVisible();
    await expect(a.getByText("记录 B", { exact: true })).toBeVisible();
    await expect(a.getByLabel("我的笔记", { exact: true })).toHaveValue(
      "会议中保存，不断开媒体",
    );
    const other = await ca.newPage();
    await other.goto(`/history/${m.id}`);
    await expect(other.getByLabel("我的笔记", { exact: true })).toHaveValue(
      "会议中保存，不断开媒体",
    );
    await a.getByLabel("我的笔记", { exact: true }).fill("服务器新版本");
    await a.getByRole("button", { name: "保存笔记", exact: true }).click();
    await expect(
      a.getByRole("status").filter({ hasText: "已保存" }),
    ).toBeVisible();
    await other
      .getByLabel("我的笔记", { exact: true })
      .fill("另一个标签页保留的编辑");
    await other.getByRole("button", { name: "保存笔记", exact: true }).click();
    await expect(other.getByLabel("笔记版本冲突")).toBeVisible();
    await expect(other.getByLabel("我的笔记", { exact: true })).toHaveValue(
      "另一个标签页保留的编辑",
    );
    await expect(other.getByLabel("笔记版本冲突")).toContainText(
      "服务器新版本",
    );
    await other
      .getByRole("button", { name: "保留本地内容，以最新版本重试保存" })
      .click();
    await expect(
      other.getByRole("status").filter({ hasText: "已保存" }),
    ).toBeVisible();
    await other.reload();
    await expect(other.getByLabel("我的笔记", { exact: true })).toHaveValue(
      "另一个标签页保留的编辑",
    );
    await b.goto(`/history/${m.id}`);
    await expect(
      b.getByText("另一个标签页保留的编辑", { exact: true }),
    ).toBeVisible();
    await expect(b.getByLabel("我的笔记", { exact: true })).toHaveValue("");
    const c = await cc.newPage();
    await c.goto(`/history/${m.id}`);
    await expect(c.getByRole("alert")).toContainText(
      "只有主持人或实际参会成员",
    );
    expect(
      (
        await request.get(`/api/v1/meetings/${m.id}/notes`, {
          headers: auth(2),
        })
      ).status(),
    ).toBe(403);
    for (const size of [
      { width: 1440, height: 900 },
      { width: 390, height: 844 },
    ]) {
      await b.setViewportSize(size);
      expect(
        await b.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    }
  } finally {
    await Promise.all([ca.close(), cb.close(), cc.close()]);
  }
});
