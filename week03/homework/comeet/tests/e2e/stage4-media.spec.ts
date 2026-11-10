import {
  test,
  expect,
  type Page,
  type BrowserContext,
} from "../../web/node_modules/@playwright/test";
let a: any, b: any;
test.beforeAll(async ({ request }) => {
  for (const name of ["a", "b"]) {
    const username =
      "media" + name + crypto.randomUUID().replaceAll("-", "").slice(0, 10);
    const r = await request.post("/api/v1/auth/register", {
      data: {
        username,
        displayName: "音视频 " + name.toUpperCase(),
        password: "Stage4_test_password",
      },
    });
    expect(r.status()).toBe(201);
    const login = await request.post("/api/v1/auth/login", {
      data: { username, password: "Stage4_test_password" },
    });
    if (name === "a") a = (await login.json()).data;
    else b = (await login.json()).data;
  }
});
async function setup(c: BrowserContext, s: any) {
  await c.addInitScript((s) => {
    sessionStorage.setItem(
      "comeet.auth.v1",
      JSON.stringify({ state: { session: s }, version: 0 }),
    );
    const get = navigator.mediaDevices.getUserMedia.bind(
      navigator.mediaDevices,
    );
    (window as any).captured = [];
    navigator.mediaDevices.getUserMedia = async (c) => {
      const stream = await get(c);
      (window as any).captured.push(...stream.getTracks());
      return stream;
    };
    const Peer = window.RTCPeerConnection;
    (window as any).peers = [];
    window.RTCPeerConnection = class extends Peer {
      constructor(config?: RTCConfiguration) {
        super(config);
        (window as any).peers.push(this);
      }
    };
  }, s);
  return c.newPage();
}
async function connected(p: Page) {
  await expect(p.locator(".connection-status")).toHaveText("业务已连接", {
    timeout: 15000,
  });
  await expect(p.locator(".media-status")).toHaveText("媒体已连接", {
    timeout: 20000,
  });
}
async function inbound(p: Page) {
  return p.evaluate(async () => {
    let video = 0,
      audio = 0;
    for (const pc of (window as any).peers as RTCPeerConnection[]) {
      if (pc.connectionState === "closed") continue;
      for (const s of (await pc.getStats()).values()) {
        if (s.type === "inbound-rtp" && s.kind === "video")
          video += s.framesDecoded || 0;
        if (s.type === "inbound-rtp" && s.kind === "audio")
          audio += s.bytesReceived || 0;
      }
    }
    return { video, audio };
  });
}
test("real LiveKit SFU: bidirectional audio/video, VP8 simulcast, toggles, no duplicate capture/playback and release", async ({
  browser,
  request,
}) => {
  test.setTimeout(90000);
  const r = await request.post("/api/v1/meetings", {
    headers: { Authorization: "Bearer " + a.token },
    data: { title: "第四阶段音视频联调", clientRequestId: crypto.randomUUID() },
  });
  const m = (await r.json()).data;
  const ca = await browser.newContext({
      permissions: ["camera", "microphone"],
    }),
    cb = await browser.newContext({ permissions: ["camera", "microphone"] });
  const pa = await setup(ca, a),
    pb = await setup(cb, b);
  try {
    for (const p of [pa, pb]) {
      await p.goto("/meetings/" + m.id + "/prepare");
      await p.getByRole("button", { name: "开启麦克风", exact: true }).click();
      await p.getByRole("button", { name: "开启摄像头", exact: true }).click();
      await expect(p.getByLabel("摄像头预览")).toBeVisible();
      await p.getByRole("button", { name: "进入会议", exact: true }).click();
      await connected(p);
    }
    for (const p of [pa, pb]) {
      await expect(p.locator(".member-tile video")).toHaveCount(2, {
        timeout: 15000,
      });
      await expect(p.getByTestId("remote-audio")).toHaveCount(1);
      await expect
        .poll(
          async () => {
            const s = await inbound(p);
            return s.video > 0 && s.audio > 0;
          },
          { timeout: 20000 },
        )
        .toBe(true);
      const live = await p.evaluate(() =>
        (window as any).captured
          .filter((t: MediaStreamTrack) => t.readyState === "live")
          .map((t: MediaStreamTrack) => t.kind),
      );
      expect(live.filter((k: string) => k === "audio")).toHaveLength(1);
      expect(live.filter((k: string) => k === "video")).toHaveLength(1);
      expect(
        await p.evaluate(() =>
          [...document.querySelectorAll("video")].every((v) => v.muted),
        ),
      ).toBe(true);
    }
    const encoding = await pa.evaluate(async () => {
      let layers = 0,
        vp8 = false;
      for (const pc of (window as any).peers as RTCPeerConnection[]) {
        if (pc.connectionState === "closed") continue;
        for (const sender of pc.getSenders()) {
          if (sender.track?.kind === "video")
            layers = Math.max(layers, sender.getParameters().encodings.length);
        }
        for (const s of (await pc.getStats()).values()) {
          if (s.type === "codec" && s.mimeType?.toLowerCase() === "video/vp8")
            vp8 = true;
        }
      }
      return { layers, vp8 };
    });
    expect(encoding.layers).toBe(3);
    expect(encoding.vp8).toBe(true);
    await pa.getByRole("button", { name: "关闭麦克风", exact: true }).click();
    await expect(
      pb.locator(".member-tile").filter({ hasText: "音视频 A" }),
    ).toContainText("麦克风关闭");
    await pa.getByRole("button", { name: "关闭摄像头", exact: true }).click();
    await expect(pb.locator(".member-tile video")).toHaveCount(1);
    await pa.getByRole("button", { name: "开启麦克风", exact: true }).click();
    await expect(pb.getByTestId("remote-audio")).toHaveCount(1);
    await pa.locator("textarea").fill("音视频同时聊天");
    await pa.getByRole("button", { name: "发送", exact: true }).click();
    await expect(pb.getByText("音视频同时聊天", { exact: true })).toBeVisible();
    await pa.screenshot({
      path: "../docs/screenshots/stage4-room.png",
      fullPage: true,
    });
    await pa.getByRole("button", { name: "离开会议", exact: true }).click();
    await expect(pa).toHaveURL("/");
    await expect
      .poll(() =>
        pa.evaluate(() =>
          (window as any).captured.every(
            (t: MediaStreamTrack) => t.readyState === "ended",
          ),
        ),
      )
      .toBe(true);
    await expect(pb.getByTestId("remote-audio")).toHaveCount(0);
    await expect(pb.locator(".member-tile")).toHaveCount(1);
    // SPA navigation away also uses the same release path.
    await pb.evaluate(() => {
      history.pushState({}, "", "/settings");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await expect(
      pb.getByRole("heading", { name: "设置", exact: true }),
    ).toBeVisible();
    await expect
      .poll(() =>
        pb.evaluate(() =>
          (window as any).captured.every(
            (t: MediaStreamTrack) => t.readyState === "ended",
          ),
        ),
      )
      .toBe(true);
  } finally {
    await ca.close();
    await cb.close();
  }
});
test("media unavailable keeps actual business chat usable", async ({
  browser,
  request,
}) => {
  const r = await request.post("/api/v1/meetings", {
    headers: { Authorization: "Bearer " + a.token },
    data: { title: "媒体故障与文字隔离", clientRequestId: crypto.randomUUID() },
  });
  const m = (await r.json()).data;
  const c = await browser.newContext();
  const p = await setup(c, a);
  try {
    await p.route("**/api/v1/meetings/*/media-token", (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({
          code: "MEDIA_NOT_CONFIGURED",
          message: "测试故障注入",
          data: null,
          requestId: "test",
        }),
      }),
    );
    await p.goto("/meetings/" + m.id + "/room");
    await expect(p.locator(".connection-status")).toHaveText("业务已连接");
    await expect(p.locator(".media-status")).toContainText("媒体未配置");
    await p.locator("textarea").fill("媒体失败不影响文字");
    await p.getByRole("button", { name: "发送", exact: true }).click();
    await expect(
      p.getByText("媒体失败不影响文字", { exact: true }),
    ).toBeVisible();
  } finally {
    await c.close();
  }
});
