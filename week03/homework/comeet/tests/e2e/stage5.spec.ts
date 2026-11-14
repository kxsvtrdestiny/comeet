import { test, expect } from "../../web/node_modules/@playwright/test";
import {
  nextSelection,
  sourceKey,
  type ViewSource,
} from "../../web/src/services/roomView";

test("multiple screen sources: stable auto, manual priority, separate identities, stopped-share fallback", () => {
  const make = (
    id: string,
    source: ViewSource["source"],
    local = false,
  ): ViewSource => ({
    key: sourceKey(id, source),
    participantIdentity: id,
    source,
    local,
    available: true,
  });
  const a = make("a", "camera", true),
    b = make("b", "camera"),
    sa = make("a", "screenShare", true),
    sb = make("b", "screenShare");
  let view = nextSelection({ mode: "auto", key: null }, [a, b]);
  expect(view.key).toBe(b.key);
  view = nextSelection(view, [a, b, sa]);
  expect(view.key).toBe(sa.key);
  expect(nextSelection(view, [a, b, sb, sa]).key).toBe(sa.key); // second share never steals a valid auto share
  expect(
    nextSelection({ mode: "manual", key: a.key }, [a, b, sa, sb]).key,
  ).toBe(a.key);
  expect(
    nextSelection({ mode: "manual", key: sb.key }, [a, b, sa, sb]).key,
  ).toBe(sb.key);
  expect(nextSelection({ mode: "manual", key: sb.key }, [a, b, sa])).toEqual({
    mode: "auto",
    key: sa.key,
  });
  expect(nextSelection(view, [a, b]).key).toBe(b.key);
  const pinned = { mode: "manual" as const, key: sb.key };
  expect(
    nextSelection(pinned, [
      a,
      b,
      { ...sb, available: false, recovering: true },
    ]),
  ).toEqual(pinned);
  expect(
    nextSelection(pinned, [
      a,
      b,
      { ...sb, available: false, recovering: false },
    ]),
  ).toEqual({ mode: "auto", key: b.key });
});

test("real Go moderation UI: denied guest controls, mute-without-mic, remove retry/ban and host end after personal leave", async ({
  browser,
  request,
}) => {
  test.setTimeout(60000);
  const sessions = [];
  for (const displayName of ["第五阶段 A", "第五阶段 B"]) {
    const username =
      "s5_" + crypto.randomUUID().replaceAll("-", "").slice(0, 10);
    const data = { username, displayName, password: "Stage5_test_password" };
    expect(
      (await request.post("/api/v1/auth/register", { data })).status(),
    ).toBe(201);
    const r = await request.post("/api/v1/auth/login", {
      data: { username, password: data.password },
    });
    expect(r.status()).toBe(200);
    sessions.push((await r.json()).data);
  }
  const headers = (i: number) => ({
    Authorization: "Bearer " + sessions[i].token,
  });
  const create = async () => {
    const r = await request.post("/api/v1/meetings", {
      headers: headers(0),
      data: {
        title: "第五阶段真实会控检查",
        clientRequestId: crypto.randomUUID(),
      },
    });
    expect(r.status()).toBe(201);
    return (await r.json()).data;
  };
  const contexts = await Promise.all(sessions.map(() => browser.newContext()));
  try {
    const pages = [];
    for (let i = 0; i < 2; i++) {
      await contexts[i].addInitScript(
        (s) =>
          sessionStorage.setItem(
            "comeet.auth.v1",
            JSON.stringify({ state: { session: s }, version: 0 }),
          ),
        sessions[i],
      );
      pages.push(await contexts[i].newPage());
    }
    const m = await create();
    const [a, b] = pages;
    for (const p of pages) {
      await p.goto(`/meetings/${m.id}/room`);
      await expect(p.locator(".member-tile").first()).toBeVisible();
    }
    await expect(a.locator(".member-tile")).toHaveCount(2);
    await expect(
      b.getByRole("button", { name: "结束会议", exact: true }),
    ).toHaveCount(0);
    for (const action of ["mute", "remove", "end"]) {
      const url =
        action === "end"
          ? `/api/v1/meetings/${m.id}/end`
          : `/api/v1/meetings/${m.id}/members/${sessions[0].user.id}/${action}`;
      const denied = await request.post(url, {
        headers: headers(1),
        data: { requestId: crypto.randomUUID() },
      });
      expect(denied.status()).toBe(403);
    }
    await a.locator(".toolbar").getByRole("button", { name: /成员/ }).click();
    await a
      .getByRole("button", { name: "静音：第五阶段 B", exact: true })
      .click();
    await expect(
      a.getByRole("alert").filter({ hasText: "该成员当前未开麦" }),
    ).toBeVisible();
    let failed = true;
    const ids: string[] = [];
    await a.route("**/api/v1/meetings/*/members/*/remove", async (route) => {
      ids.push(route.request().postDataJSON().requestId);
      if (failed) {
        failed = false;
        await route.fulfill({
          status: 502,
          contentType: "application/json",
          body: JSON.stringify({
            code: "MEDIA_CONTROL_FAILED",
            message: "测试注入：媒体管理未完全完成，可重试",
            data: null,
            requestId: "test",
          }),
        });
      } else await route.continue();
    });
    a.once("dialog", (d) => d.accept());
    await a
      .getByRole("button", { name: "移出：第五阶段 B", exact: true })
      .click();
    await expect(
      a.getByRole("alert").filter({ hasText: "媒体管理未完全完成" }),
    ).toBeVisible();
    await a.getByRole("button", { name: "重试移出成员", exact: true }).click();
    await expect(b).toHaveURL("/");
    const success = a.locator(".room-toast.success");
    await expect(success).toContainText("成员已移出，媒体清理完成");
    await expect(success).toHaveCount(0, { timeout: 8000 });
    expect(ids).toHaveLength(2);
    expect(ids[0]).toBe(ids[1]);
    expect(
      (
        await request.post(`/api/v1/meetings/${m.id}/join`, {
          headers: headers(1),
          data: { clientRequestId: crypto.randomUUID() },
        })
      ).status(),
    ).toBe(403);
    const next = await create();
    for (const p of pages) {
      await p.goto(`/meetings/${next.id}/room`);
      await expect(p.locator(".member-tile").first()).toBeVisible();
    }
    await expect(a.locator(".member-tile")).toHaveCount(2);
    await a.getByRole("button", { name: "离开会议", exact: true }).click();
    await expect(a).toHaveURL("/");
    await expect(b.locator(".member-tile")).toHaveCount(1);
    await a.goto(`/meetings/${next.id}`);
    a.once("dialog", (d) => d.accept());
    await a
      .getByRole("button", { name: "结束所有人的会议", exact: true })
      .click();
    await expect(a.getByText("状态：已结束")).toBeVisible();
    await expect(b).toHaveURL("/");
    for (let i = 0; i < 2; i++)
      expect(
        (
          await request.post(`/api/v1/meetings/${next.id}/join`, {
            headers: headers(i),
            data: { clientRequestId: crypto.randomUUID() },
          })
        ).status(),
      ).toBe(409);
  } finally {
    await Promise.all(contexts.map((c) => c.close()));
  }
});
