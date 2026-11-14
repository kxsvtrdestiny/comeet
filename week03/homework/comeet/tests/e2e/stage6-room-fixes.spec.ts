import { test, expect } from "../../web/node_modules/@playwright/test";
function overlap(a: any, b: any) {
  return (
    !!a &&
    !!b &&
    a.x < b.x + b.width &&
    a.x + a.width > b.x &&
    a.y < b.y + b.height &&
    a.y + a.height > b.y
  );
}
test("notes sidebar preserves drafts/conflicts; no overlay or selection strip; corner notifications avoid editors", async ({
  browser,
  request,
}) => {
  const username = "fix_" + crypto.randomUUID().replaceAll("-", "").slice(0, 9),
    password = "Room_fix_test_password";
  expect(
    (
      await request.post("/api/v1/auth/register", {
        data: { username, displayName: "侧栏检查", password },
      })
    ).status(),
  ).toBe(201);
  const login = await request.post("/api/v1/auth/login", {
    data: { username, password },
  });
  const session = (await login.json()).data;
  const c = await browser.newContext();
  await c.addInitScript(
    (s) =>
      sessionStorage.setItem(
        "comeet.auth.v1",
        JSON.stringify({ state: { session: s }, version: 0 }),
      ),
    session,
  );
  try {
    const r = await request.post("/api/v1/meetings", {
      headers: { Authorization: "Bearer " + session.token },
      data: { title: "会议室修复检查", clientRequestId: crypto.randomUUID() },
    });
    const meeting = (await r.json()).data;
    const p = await c.newPage();
    let grants = 0;
    p.on("request", (r) => {
      if (r.url().endsWith("/media-token")) grants++;
    });
    await p.goto(`/meetings/${meeting.id}/room`);
    await expect(p.locator(".member-tile")).toHaveCount(1);
    await expect.poll(() => grants).toBeGreaterThan(0);
    const before = grants;
    await expect(p.getByText("自动选择", { exact: true })).toHaveCount(0);
    await expect(p.locator(".view-mode")).toHaveCount(0);
    await p.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    await p
      .getByRole("button", { name: "设为主画面：侧栏检查", exact: true })
      .click();
    await expect(p.getByTitle("恢复自动选择")).toBeVisible();
    await expect(p.getByText("已固定主画面", { exact: true })).toHaveCount(0);
    await p.getByTitle("恢复自动选择").click();
    await p.getByRole("button", { name: "会议笔记", exact: true }).click();
    await p
      .getByLabel("我的笔记", { exact: true })
      .fill("关闭和切换仍保留的草稿");
    await expect(p.getByRole("dialog")).toHaveCount(0);
    await expect(p.locator(".drawer-backdrop")).toHaveCount(0);
    await p.getByRole("button", { name: "关闭面板" }).click();
    await p.locator(".toolbar").getByRole("button", { name: /^聊天/ }).click();
    await p.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    await p.getByRole("button", { name: "会议笔记", exact: true }).click();
    await expect(p.getByLabel("我的笔记", { exact: true })).toHaveValue(
      "关闭和切换仍保留的草稿",
    );
    await request.put(`/api/v1/meetings/${meeting.id}/notes/me`, {
      headers: { Authorization: "Bearer " + session.token },
      data: { content: "另一个标签页的新版本", version: 0 },
    });
    await p.getByRole("button", { name: "保存笔记", exact: true }).click();
    await expect(p.getByLabel("笔记版本冲突")).toBeVisible();
    await p.getByRole("button", { name: "关闭面板" }).click();
    await p.getByRole("button", { name: "会议笔记", exact: true }).click();
    await expect(p.getByLabel("笔记版本冲突")).toContainText(
      "另一个标签页的新版本",
    );
    await p
      .getByRole("button", { name: "保留本地内容，以最新版本重试保存" })
      .click();
    await expect(
      p.getByRole("status").filter({ hasText: "已保存" }),
    ).toBeVisible();
    expect(grants).toBe(before);
    // Deliberate management failure; the actual Go permission/media tests cover
    // persistent partial cleanup. Here only the UI response is injected.
    const fail = true;
    await p.route("**/api/v1/meetings/*/end", (route) =>
      route.fulfill({
        status: fail ? 502 : 200,
        contentType: "application/json",
        body: JSON.stringify(
          fail
            ? {
                code: "MEDIA_CONTROL_FAILED",
                message: "测试：删除房间鉴权失败；诊断编号：ui-safe-diagnostic",
                data: null,
                requestId: "ui-request",
              }
            : {
                code: "OK",
                message: "success",
                data: { operationId: "test", state: "completed" },
                requestId: "ui-request",
              },
        ),
      }),
    );
    for (const size of [
      { width: 1440, height: 900 },
      { width: 1366, height: 768 },
      { width: 390, height: 844 },
    ]) {
      await p.setViewportSize(size);
      p.once("dialog", (d) => d.accept());
      await p.getByRole("button", { name: "结束会议", exact: true }).click();
      const toast = p.locator(".room-toast.error").last();
      await expect(toast).toContainText("ui-safe-diagnostic");
      expect(
        overlap(
          await p.locator(".main-view").boundingBox(),
          await p.locator(".notes-panel").boundingBox(),
        ),
      ).toBe(false);
      expect(
        overlap(
          await toast.boundingBox(),
          await p.getByLabel("我的笔记", { exact: true }).boundingBox(),
        ),
      ).toBe(false);
      expect(
        overlap(
          await toast.boundingBox(),
          await p
            .getByRole("button", { name: "保存笔记", exact: true })
            .boundingBox(),
        ),
      ).toBe(false);
      expect(
        overlap(
          await toast.boundingBox(),
          await p.locator(".toolbar").boundingBox(),
        ),
      ).toBe(false);
      expect(
        await p.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await expect(p.getByRole("button", { name: "保存笔记", exact: true })).toBeInViewport({ ratio: 1 });
      await p.screenshot({ path: `/tmp/comeet-room-notes-${size.width}.png` });
      await p.locator(".toolbar").getByRole("button", { name: /^聊天/ }).click();
      expect(overlap(await toast.boundingBox(), await p.locator(".chat-composer").boundingBox())).toBe(false);
      await p.locator(".tabs").getByRole("button", { name: "笔记", exact: true }).click();
      await p
        .locator(".room-toasts")
        .getByRole("button", { name: "关闭通知" })
        .last()
        .click();
    }
    await p.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    await expect(
      p
        .locator(".member-panel-content")
        .getByRole("button", { name: "重试结束会议", exact: true })
        .first(),
    ).toBeVisible();
    expect(grants).toBe(before);
  } finally {
    await c.close();
  }
});
