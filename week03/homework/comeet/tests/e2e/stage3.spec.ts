import {
  test,
  expect,
  type Browser,
  type BrowserContext,
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
  title: string;
  meetingNo: string;
}
let a: Session, b: Session;
async function account(
  request: APIRequestContext,
  prefix: string,
  name: string,
): Promise<Session> {
  const username =
    prefix + crypto.randomUUID().replaceAll("-", "").slice(0, 10);
  let r = await request.post("/api/v1/auth/register", {
    data: { username, displayName: name, password: "Stage3_test_password" },
  });
  expect(r.status()).toBe(201);
  r = await request.post("/api/v1/auth/login", {
    data: { username, password: "Stage3_test_password" },
  });
  expect(r.status()).toBe(200);
  return (await r.json()).data;
}
async function context(browser: Browser, s: Session) {
  const c = await browser.newContext();
  await c.addInitScript(
    (value) =>
      sessionStorage.setItem(
        "comeet.auth.v1",
        JSON.stringify({ state: { session: value }, version: 0 }),
      ),
    s,
  );
  return c;
}
async function meeting(request: APIRequestContext) {
  const r = await request.post("/api/v1/meetings", {
    headers: { Authorization: `Bearer ${a.token}` },
    data: { title: "第三阶段业务测试", clientRequestId: crypto.randomUUID() },
  });
  expect(r.status()).toBe(201);
  return (await r.json()).data as Meeting;
}
async function connected(page: Page) {
  await expect(page.getByRole("status")).toHaveText("业务已连接 · 仅文字会议", {
    timeout: 15000,
  });
}
async function enter(c: BrowserContext, m: Meeting) {
  const p = await c.newPage();
  await p.goto(`http://localhost:5173/meetings/${m.id}/room`);
  await connected(p);
  return p;
}
async function send(p: Page, text: string) {
  await p.getByLabel("消息", { exact: true }).fill(text);
  await p.getByRole("button", { name: "发送", exact: true }).click();
}
test.beforeAll(async ({ request }) => {
  a = await account(request, "s3a_", "成员 A");
  b = await account(request, "s3b_", "成员 B");
});
test("two real browsers: members, identity markers, bidirectional chat, XSS and leave", async ({
  browser,
  request,
}) => {
  const m = await meeting(request),
    ca = await context(browser, a),
    cb = await context(browser, b);
  try {
    const pa = await enter(ca, m),
      pb = await enter(cb, m);
    await expect(pa.locator(".member-tile")).toHaveCount(2);
    await expect(pb.locator(".member-tile")).toHaveCount(2);
    await expect(
      pa
        .locator(".member-tile")
        .filter({ hasText: "成员 A" })
        .getByText("主持人", { exact: true }),
    ).toBeVisible();
    await expect(
      pb
        .locator(".member-tile")
        .filter({ hasText: "成员 B" })
        .getByText("本人", { exact: true }),
    ).toBeVisible();
    const text = '<img src=x onerror="window.chatInjected=1">';
    await send(pa, text);
    await expect(
      pb.locator(".chat-message p").filter({ hasText: text }),
    ).toBeVisible();
    expect(
      await pb.evaluate(
        () => (window as unknown as { chatInjected?: number }).chatInjected,
      ),
    ).toBeUndefined();
    await send(pb, "B → A 实时消息");
    await expect(
      pa.locator(".chat-message p").filter({ hasText: "B → A 实时消息" }),
    ).toBeVisible();
    await pb.getByRole("button", { name: "离开会议", exact: true }).click();
    await expect(pb).toHaveURL(/\/$/);
    await expect(pa.locator(".member-tile")).toHaveCount(1);
    await expect(pa.getByRole("status")).toHaveText("业务已连接 · 仅文字会议");
  } finally {
    await ca.close();
    await cb.close();
  }
});
test("duplicated tab never takes over healthy original connection", async ({
  browser,
  request,
}) => {
  const m = await meeting(request),
    ca = await context(browser, a),
    cb = await context(browser, b),
    duplicate = await browser.newContext();
  try {
    const pa = await enter(ca, m),
      pb = await enter(cb, m);
    const saved = await pa.evaluate(() =>
      Object.fromEntries(
        Object.keys(sessionStorage).map((key) => [
          key,
          sessionStorage.getItem(key),
        ]),
      ),
    );
    await duplicate.addInitScript((values) => {
      for (const [key, value] of Object.entries(values))
        if (value !== null) sessionStorage.setItem(key, value);
    }, saved);
    const pc = await duplicate.newPage();
    await pc.goto(`http://localhost:5173/meetings/${m.id}/room`);
    await expect(pc.getByRole("status")).toContainText("原连接仍在线", {
      timeout: 8000,
    });
    await connected(pa);
    await send(pb, "原连接仍可接收");
    await expect(
      pa.locator(".chat-message p").filter({ hasText: "原连接仍可接收" }),
    ).toBeVisible();
    await duplicate.close();
    await expect(pa.locator(".member-tile")).toHaveCount(2);
    const r = await request.get(`/api/v1/meetings/${m.id}/members`, {
      headers: { Authorization: `Bearer ${a.token}` },
    });
    expect((await r.json()).data).toHaveLength(2);
  } finally {
    await ca.close();
    await cb.close();
    await duplicate.close();
  }
});
test("refresh resumes same participant without duplicated history", async ({
  browser,
  request,
}) => {
  const m = await meeting(request),
    ca = await context(browser, a),
    cb = await context(browser, b);
  try {
    const pa = await enter(ca, m),
      pb = await enter(cb, m);
    await send(pa, "刷新前消息");
    await expect(pb.locator(".chat-message")).toHaveCount(1);
    const key = `comeet.room.${b.user.id}.${m.id}`;
    const before = await pb.evaluate(
      (k) => JSON.parse(sessionStorage.getItem(k)!).sessionId,
      key,
    );
    await pb.reload();
    await connected(pb);
    const after = await pb.evaluate(
      (k) => JSON.parse(sessionStorage.getItem(k)!).sessionId,
      key,
    );
    expect(after).toBe(before);
    await expect(pb.locator(".chat-message")).toHaveCount(1);
    await expect(pa.locator(".member-tile")).toHaveCount(2);
    await send(pb, "刷新后消息");
    await expect(pa.locator(".chat-message")).toHaveCount(2);
  } finally {
    await ca.close();
    await cb.close();
  }
});
test("offline recovery catches more than 100 missed messages and history pages", async ({
  browser,
  request,
}) => {
  test.setTimeout(120000);
  const m = await meeting(request),
    ca = await context(browser, a),
    cb = await context(browser, b);
  try {
    const pa = await enter(ca, m),
      pb = await enter(cb, m);
    const catchup: string[] = [];
    pb.on("request", (r) => {
      if (r.url().includes("/messages?afterId=")) catchup.push(r.url());
    });
    await cb.setOffline(true);
    await expect(pb.getByRole("status")).toContainText("重连");
    await expect(
      pa.locator(".member-tile").filter({ hasText: "成员 B" }),
    ).toContainText("重连中");
    for (let i = 0; i < 105; i++) await send(pa, `缺失消息_${i}`);
    await expect(pa.locator(".chat-message")).toHaveCount(105);
    await cb.setOffline(false);
    await connected(pb);
    await expect(pb.locator(".chat-message")).toHaveCount(105);
    expect(catchup.length).toBeGreaterThanOrEqual(2);
    await expect(
      pb.locator(".chat-message p").filter({ hasText: "缺失消息_104" }),
    ).toBeVisible();
    await pb.reload();
    await connected(pb);
    await expect(pb.locator(".chat-message")).toHaveCount(50);
    await pb.getByRole("button", { name: "加载更早消息" }).click();
    await expect(pb.locator(".chat-message")).toHaveCount(100);
    await pb.getByRole("button", { name: "加载更早消息" }).click();
    await expect(pb.locator(".chat-message")).toHaveCount(105);
    const ids = await pb
      .locator(".chat-message")
      .evaluateAll((rows) =>
        rows.map((row) => row.getAttribute("data-message-id")),
      );
    expect(new Set(ids).size).toBe(105);
  } finally {
    await ca.close();
    await cb.close();
  }
});
test("lost ACK: retry original clientMessageId only persists once", async ({
  browser,
  request,
}) => {
  test.setTimeout(45000);
  const m = await meeting(request),
    ca = await context(browser, a),
    cb = await context(browser, b);
  let dropping = true;
  try {
    const pa = await ca.newPage();
    await pa.routeWebSocket("**/ws?*", (ws) => {
      const server = ws.connectToServer();
      server.onMessage((raw) => {
        const frame = JSON.parse(raw.toString());
        if (
          dropping &&
          (frame.type === "chat.ack" || frame.type === "chat.created")
        )
          return;
        ws.send(raw);
      });
    });
    await pa.goto(`http://localhost:5173/meetings/${m.id}/room`);
    await connected(pa);
    const pb = await enter(cb, m);
    await send(pa, "同一原消息重试");
    await expect(pb.locator(".chat-message")).toHaveCount(1);
    await expect(pa.getByRole("button", { name: "重试此消息" })).toBeVisible({
      timeout: 12000,
    });
    const id = await pa
      .locator(".chat-message")
      .getAttribute("data-client-message-id");
    dropping = false;
    await pa.getByRole("button", { name: "重试此消息" }).click();
    await expect(pa.locator(".chat-message small")).toHaveText("已发送");
    expect(
      await pa.locator(".chat-message").getAttribute("data-client-message-id"),
    ).toBe(id);
    await expect(pa.locator(".chat-message")).toHaveCount(1);
    await expect(pb.locator(".chat-message")).toHaveCount(1);
    const r = await request.get(`/api/v1/meetings/${m.id}/messages?limit=100`, {
      headers: { Authorization: `Bearer ${a.token}` },
    });
    expect((await r.json()).data.items).toHaveLength(1);
  } finally {
    await ca.close();
    await cb.close();
  }
});
test("host personal leave does not end meeting and can join again", async ({
  browser,
  request,
}) => {
  const m = await meeting(request),
    ca = await context(browser, a),
    cb = await context(browser, b);
  try {
    const pa = await enter(ca, m),
      pb = await enter(cb, m);
    await pa.getByRole("button", { name: "离开会议", exact: true }).click();
    await expect(pb.locator(".member-tile")).toHaveCount(1);
    await send(pb, "主持人离开后会议仍在");
    await expect(pb.locator(".chat-message small")).toHaveText("已发送");
    const r = await request.get(`/api/v1/meetings/${m.id}`, {
      headers: { Authorization: `Bearer ${a.token}` },
    });
    expect((await r.json()).data.status).toBe("active");
    await pa.goto(`http://localhost:5173/meetings/${m.id}/room`);
    await connected(pa);
    await expect(pb.locator(".member-tile")).toHaveCount(2);
    await expect(pa.locator(".chat-message p")).toHaveText(
      "主持人离开后会议仍在",
    );
  } finally {
    await ca.close();
    await cb.close();
  }
});
test("real 45-second reconnect grace expires, then explicit rejoin works", async ({
  browser,
  request,
}) => {
  test.setTimeout(100000);
  const m = await meeting(request),
    ca = await context(browser, a),
    cb = await context(browser, b);
  try {
    const pa = await enter(ca, m),
      pb = await enter(cb, m);
    const key = `comeet.room.${b.user.id}.${m.id}`;
    const old = await pb.evaluate(
      (k) => JSON.parse(sessionStorage.getItem(k)!).sessionId,
      key,
    );
    await cb.setOffline(true);
    await expect(
      pb.getByRole("button", { name: "重新加入", exact: true }),
    ).toBeVisible({ timeout: 60000 });
    await expect(pa.locator(".member-tile")).toHaveCount(1, { timeout: 5000 });
    await cb.setOffline(false);
    await pb.getByRole("button", { name: "重新加入", exact: true }).click();
    await connected(pb);
    const current = await pb.evaluate(
      (k) => JSON.parse(sessionStorage.getItem(k)!).sessionId,
      key,
    );
    expect(current).not.toBe(old);
    await expect(pa.locator(".member-tile")).toHaveCount(2);
  } finally {
    await ca.close();
    await cb.close();
  }
});
test("dashboard diagnostics moved into settings", async ({ browser }) => {
  const c = await context(browser, a);
  try {
    const p = await c.newPage();
    await p.goto("http://localhost:5173/");
    await expect(
      p.getByRole("heading", { name: "工作台", exact: true }),
    ).toBeVisible();
    await expect(
      p.getByRole("heading", { name: "服务状态", exact: true }),
    ).toHaveCount(0);
    await p.getByRole("link", { name: "设置", exact: true }).click();
    await expect(
      p.getByRole("heading", { name: "服务状态", exact: true }),
    ).toBeVisible();
    await expect(p.getByText("● 数据库初始化正常")).toBeVisible();
  } finally {
    await c.close();
  }
});
