// Requires a real configured LiveKit service. Browser capture uses Chromium's
// automated desktop picker; this does not replace physical Windows acceptance.
import { test, expect } from "../../web/node_modules/@playwright/test";
test("LiveKit sharing/mute/remove/end and Cloud old-token revocation", async ({
  browser,
  request,
}) => {
  test.setTimeout(150000);
  const sessions = [];
  for (const displayName of ["共享 A", "共享 B"]) {
    const username =
        "s5media_" + crypto.randomUUID().replaceAll("-", "").slice(0, 8),
      password = "Stage5_media_test_password";
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
  const r = await request.post("/api/v1/meetings", {
    headers: { Authorization: "Bearer " + sessions[0].token },
    data: {
      title: "第五阶段真实共享会控",
      clientRequestId: crypto.randomUUID(),
    },
  });
  expect(r.status()).toBe(201);
  const meeting = (await r.json()).data;
  const contexts = await Promise.all(
    sessions.map(() =>
      browser.newContext({ permissions: ["camera", "microphone"] }),
    ),
  );
  let oldGrant: any;
  try {
    const pages = [];
    for (let i = 0; i < 2; i++) {
      const c = contexts[i];
      await c.addInitScript(
        (s) =>
          sessionStorage.setItem(
            "comeet.auth.v1",
            JSON.stringify({ state: { session: s }, version: 0 }),
          ),
        sessions[i],
      );
      const p = await c.newPage();
      if (i === 1)
        p.on("response", async (response) => {
          if (response.url().endsWith("/media-token") && response.ok())
            oldGrant = (await response.json()).data;
        });
      await p.goto(`/meetings/${meeting.id}/room`);
      await expect(
        p.getByRole("button", { name: "开启麦克风", exact: true }),
      ).toBeEnabled({ timeout: 30000 });
      await p.getByRole("button", { name: "开启麦克风", exact: true }).click();
      await expect(
        p.getByRole("button", { name: "关闭麦克风", exact: true }),
      ).toBeVisible();
      pages.push(p);
    }
    const [a, b] = pages;
    await a.getByRole("button", { name: "共享屏幕", exact: true }).click();
    await expect(
      a.getByRole("button", { name: "停止共享", exact: true }),
    ).toBeVisible({ timeout: 15000 });
    await expect(b.locator(".main-view .screen-video")).toBeVisible({
      timeout: 20000,
    });
    await expect
      .poll(() =>
        b
          .locator(".main-view video")
          .evaluate(
            (el: HTMLVideoElement) => el.videoWidth > 0 && el.readyState >= 2,
          ),
      )
      .toBe(true);
    await a.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    await a
      .getByRole("button", { name: "设为主画面：共享 A", exact: true })
      .click();
    await a.getByRole("button", { name: "关闭面板" }).click();
    await b.getByRole("button", { name: "共享屏幕", exact: true }).click();
    await expect(
      b.getByRole("button", { name: "停止共享", exact: true }),
    ).toBeVisible({ timeout: 15000 });
    await expect(a.locator(".main-view")).not.toContainText("的共享");
    await a
      .getByRole("button", { name: "设为主画面：共享 B的共享", exact: true })
      .click();
    await expect(a.locator(".main-view")).toContainText("共享 B的共享");
    const style = await a
      .locator(".main-view video")
      .evaluate((el) => ({
        fit: getComputedStyle(el).objectFit,
        transform: getComputedStyle(el).transform,
      }));
    expect(style).toEqual({ fit: "contain", transform: "none" });
    await b.getByRole("button", { name: "停止共享", exact: true }).click();
    await expect(a.locator(".main-view")).toContainText("共享 A的共享");
    await a.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    await expect(a.locator(".member-list li")).toHaveCount(2);
    await a.getByRole("button", { name: "静音：共享 B", exact: true }).click();
    await expect(
      b.getByRole("button", { name: "开启麦克风", exact: true }),
    ).toBeVisible();
    await b.getByRole("button", { name: "开启麦克风", exact: true }).click();
    await expect(
      b.getByRole("button", { name: "关闭麦克风", exact: true }),
    ).toBeVisible();
    a.once("dialog", (d) => d.accept());
    await a.getByRole("button", { name: "移出：共享 B", exact: true }).click();
    await expect(b).toHaveURL("/");
    expect(!!oldGrant).toBe(true);
    // The old raw media token bypasses Go on purpose: this checks the Cloud's
    // actual revocation, not just our REST ban. Never log or persist the token.
    const reentered = await b.evaluate(async (grant) => {
      const { Room } = await import(
        "/node_modules/.vite/deps/livekit-client.js"
      );
      const room = new Room();
      try {
        await room.connect(grant.url, grant.token, {
          peerConnectionTimeout: 10000,
          websocketTimeout: 10000,
        });
        return room.state === "connected";
      } catch {
        return false;
      } finally {
        await room.disconnect(true);
      }
    }, oldGrant);
    expect(reentered).toBe(false);
    await a.getByRole("button", { name: "关闭面板" }).click();
    a.once("dialog", (d) => d.accept());
    await a.getByRole("button", { name: "结束会议", exact: true }).click();
    await expect(a).toHaveURL("/");
  } finally {
    await Promise.all(contexts.map((c) => c.close()));
  }
});
