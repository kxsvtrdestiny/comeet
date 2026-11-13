import { test, expect } from "../../web/node_modules/@playwright/test";
import {
  nextSelection,
  sourceKey,
  type ViewSource,
} from "../../web/src/services/roomView";
const source = (
  id: string,
  local = false,
  available = true,
  kind: ViewSource["source"] = "camera",
): ViewSource => ({
  key: sourceKey(id, kind),
  participantIdentity: id,
  source: kind,
  local,
  available,
});
test("view selection: stable auto, local pin, camera mute/replacement, leave and future source fallback", () => {
  const self = source("self", true),
    other = source("other");
  const auto = nextSelection({ mode: "auto", key: null }, [self, other]);
  expect(auto.key).toBe(other.key);
  expect(nextSelection(auto, [source("new"), self, other])).toEqual(auto);
  const pin = { mode: "manual" as const, key: self.key };
  expect(nextSelection(pin, [{ ...self, available: false }, other])).toEqual(
    pin,
  );
  expect(nextSelection(pin, [self, other])).toEqual(pin);
  const share = source("other", false, true, "screenShare");
  expect(nextSelection(pin, [self, other, share])).toEqual(pin);
  expect(nextSelection(auto, [self, other, share]).key).toBe(share.key);
  expect(
    nextSelection({ mode: "manual", key: share.key }, [
      self,
      other,
      { ...share, available: false },
    ]),
  ).toEqual(auto);
  expect(nextSelection(pin, [other])).toEqual(auto);
  expect(nextSelection(pin, [])).toEqual({ mode: "auto", key: null });
});
test("light room: local focus, panel/unread, responsive drawer and real chat survive view changes", async ({
  browser,
  request,
}) => {
  const accounts = [];
  for (const name of ["界面 A", "界面 B"]) {
    const username =
      "ui_" + crypto.randomUUID().replaceAll("-", "").slice(0, 10);
    await request.post("/api/v1/auth/register", {
      data: { username, displayName: name, password: "Ui_test_password_42" },
    });
    const r = await request.post("/api/v1/auth/login", {
      data: { username, password: "Ui_test_password_42" },
    });
    expect(r.status()).toBe(200);
    accounts.push((await r.json()).data);
  }
  const r = await request.post("/api/v1/meetings", {
    headers: { Authorization: "Bearer " + accounts[0].token },
    data: { title: "浅色会议室界面检查", clientRequestId: crypto.randomUUID() },
  });
  expect(r.status()).toBe(201);
  const meeting = (await r.json()).data;
  const contexts = await Promise.all(accounts.map(() => browser.newContext()));
  try {
    const pages = [];
    for (let i = 0; i < 2; i++) {
      await contexts[i].addInitScript(
        (s) =>
          sessionStorage.setItem(
            "comeet.auth.v1",
            JSON.stringify({ state: { session: s }, version: 0 }),
          ),
        accounts[i],
      );
      const p = await contexts[i].newPage();
      await p.route("**/api/v1/meetings/*/media-token", (route) =>
        route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({
            code: "MEDIA_NOT_CONFIGURED",
            message: "UI test media unavailable",
            data: null,
            requestId: "test",
          }),
        }),
      );
      await p.goto(`/meetings/${meeting.id}/room`);
      await expect(p.locator(".member-tile")).toHaveCount(i + 1);
      pages.push(p);
    }
    const [a, b] = pages;
    await expect(a.locator(".member-tile")).toHaveCount(2);
    await expect(a.locator(".room-panel")).toHaveCount(0);
    await expect(a.locator(".connection-status")).toHaveCount(0);
    await a.getByRole("button", { name: "连接详情", exact: true }).click();
    await expect(a.getByRole("dialog")).toContainText("聊天与成员连接：正常");
    await a.keyboard.press("Escape");
    const roomSession = await a.evaluate(
      () =>
        JSON.parse(
          Object.entries(sessionStorage).find(([k]) =>
            k.startsWith("comeet.room."),
          )![1],
        ).sessionId,
    );
    for (const size of [
      { width: 1440, height: 900 },
      { width: 1366, height: 768 },
      { width: 390, height: 844 },
    ]) {
      await a.setViewportSize(size);
      expect(
        await a.evaluate(
          () =>
            document.documentElement.scrollWidth <= innerWidth &&
            document.documentElement.scrollHeight <= innerHeight,
        ),
      ).toBe(true);
      await expect(a.locator(".toolbar")).toBeInViewport();
      expect(
        await a
          .locator(".live-room")
          .evaluate((el) => getComputedStyle(el).backgroundColor),
      ).toBe("rgb(245, 247, 251)");
    }
    await a.setViewportSize({ width: 1440, height: 900 });
    await a.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    await a
      .getByRole("button", { name: "设为主画面：界面 A", exact: true })
      .click();
    await expect(a.locator(".main-view")).toContainText("界面 A");
    await a.getByRole("button", { name: "关闭面板" }).click();
    const expanded = await a
      .locator(".room-stage")
      .evaluate((el) => el.clientWidth);
    await a.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    expect(
      await a.locator(".room-stage").evaluate((el) => el.clientWidth),
    ).toBeLessThan(expanded);
    await a.getByRole("button", { name: "关闭面板" }).click();
    await a.getByTitle("宫格布局").click();
    await a.getByTitle("聚焦布局").click();
    await expect(a.locator(".main-view")).toContainText("界面 A");
    await a.getByTitle("专注观看").click();
    await expect(a.locator(".thumbnail-strip")).toHaveCount(0);
    await a.getByTitle("退出专注").click();
    await b.locator(".toolbar").getByRole("button", { name: /^聊天/ }).click();
    await b.getByLabel("消息", { exact: true }).fill("关闭面板仍收到消息");
    await b.getByRole("button", { name: "发送", exact: true }).click();
    await expect(
      a.locator(".toolbar").getByRole("button", { name: /聊天.*1 未读/ }),
    ).toBeVisible();
    await a.locator(".toolbar").getByRole("button", { name: /^聊天/ }).click();
    await expect(a.locator(".chat-message")).toContainText(
      "关闭面板仍收到消息",
    );
    await a.getByRole("button", { name: "关闭面板" }).click();
    await a.setViewportSize({ width: 390, height: 844 });
    await a.locator(".toolbar").getByRole("button", { name: /^聊天/ }).click();
    await expect(a.getByRole("button", { name: "关闭面板" })).toBeInViewport();
    await a.keyboard.press("Escape");
    await expect(a.locator(".room-panel")).toHaveCount(0);
    await a.setViewportSize({ width: 1440, height: 900 });
    await a.screenshot({ path: "test-results/room-light-desktop.png" });
    expect(
      await a.evaluate(
        () =>
          JSON.parse(
            Object.entries(sessionStorage).find(([k]) =>
              k.startsWith("comeet.room."),
            )![1],
          ).sessionId,
      ),
    ).toBe(roomSession);
    await a
      .getByRole("button", { name: "设为主画面：界面 B", exact: true })
      .click();
    await b.getByRole("button", { name: "离开会议", exact: true }).click();
    await expect(a.locator(".main-view")).toContainText("界面 A");
    await expect(a.getByText("所选成员已离会，已恢复自动选择")).toBeVisible();
  } finally {
    await Promise.all(contexts.map((c) => c.close()));
  }
});
