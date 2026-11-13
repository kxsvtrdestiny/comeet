import { test, expect } from "../../web/node_modules/@playwright/test";
test.use({
  launchOptions: {
    executablePath: process.env.CHROMIUM_PATH,
    args: [
      "--no-sandbox",
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
    ],
  },
  permissions: ["camera", "microphone"],
});
let session: any;
test.beforeAll(async ({ request }) => {
  const username =
    "devices" + crypto.randomUUID().replaceAll("-", "").slice(0, 10);
  await request.post("/api/v1/auth/register", {
    data: {
      username,
      displayName: "设备测试",
      password: "Stage4_test_password",
    },
  });
  const r = await request.post("/api/v1/auth/login", {
    data: { username, password: "Stage4_test_password" },
  });
  session = (await r.json()).data;
});
async function prepare(page: any, request: any) {
  // Deliberate media-configuration fault injection; actual Go/SQLite chat remains real.
  await page.route("**/api/v1/meetings/*/media-token", (route: any) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({
        code: "MEDIA_NOT_CONFIGURED",
        message: "test-only media fault",
        data: null,
        requestId: "test",
      }),
    }),
  );
  await page.addInitScript((s: any) => {
    sessionStorage.setItem(
      "comeet.auth.v1",
      JSON.stringify({ state: { session: s }, version: 0 }),
    );
    const get = navigator.mediaDevices.getUserMedia.bind(
      navigator.mediaDevices,
    );
    (window as any).captured = [];
    (window as any).audioContexts = [];
    const Context = window.AudioContext;
    window.AudioContext = class extends Context {
      constructor(options?: AudioContextOptions) {
        super(options);
        (window as any).audioContexts.push(this);
      }
    };
    navigator.mediaDevices.getUserMedia = async (c) => {
      const stream = await get(c);
      (window as any).captured.push(...stream.getTracks());
      return stream;
    };
  }, session);
  const r = await request.post("/api/v1/meetings", {
    headers: { Authorization: "Bearer " + session.token },
    data: { title: "设备准备测试", clientRequestId: crypto.randomUUID() },
  });
  const m = (await r.json()).data;
  await page.goto("/meetings/" + m.id + "/prepare");
  await expect(
    page.getByRole("heading", { name: "设备准备", exact: true }),
  ).toBeVisible();
  return m;
}
test("independent preview, microphone meter, off/on and leave release every track", async ({
  page,
  request,
}) => {
  await prepare(page, request);
  expect(await page.evaluate(() => (window as any).captured.length)).toBe(0);
  await page.getByRole("button", { name: "开启摄像头", exact: true }).click();
  await expect(page.getByLabel("摄像头预览")).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        (window as any).captured.filter(
          (t: any) => t.readyState === "live" && t.kind === "audio",
        ).length,
    ),
  ).toBe(0);
  await page.getByRole("button", { name: "开启麦克风", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "关闭麦克风", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("meter", { name: "麦克风电平" })).toBeVisible();
  await expect
    .poll(
      () =>
        page
          .getByRole("meter", { name: "麦克风电平" })
          .evaluate((el: any) => el.value),
      { timeout: 5000 },
    )
    .toBeGreaterThan(0);
  await page.getByRole("button", { name: "关闭摄像头", exact: true }).click();
  await expect(page.getByLabel("摄像头预览")).toHaveCount(0);
  expect(
    await page.evaluate(
      () =>
        (window as any).captured.filter(
          (t: any) => t.readyState === "live" && t.kind === "audio",
        ).length,
    ),
  ).toBe(1);
  await page.getByRole("button", { name: "开启摄像头", exact: true }).click();
  await page.getByRole("button", { name: "扬声器试听", exact: true }).click();
  await expect(page.getByRole("button", { name: "停止试听" })).toBeVisible();
  await page.getByRole("button", { name: "停止试听" }).click();
  await page.screenshot({
    path: "../docs/screenshots/stage4-prepare.png",
    fullPage: true,
  });
  await page.getByRole("link", { name: "返回工作台" }).click();
  await expect
    .poll(() =>
      page.evaluate(() =>
        (window as any).captured.every((t: any) => t.readyState === "ended"),
      ),
    )
    .toBe(true);
  await expect
    .poll(() =>
      page.evaluate(() =>
        (window as any).audioContexts.every(
          (c: AudioContext) => c.state === "closed",
        ),
      ),
    )
    .toBe(true);
});
for (const [name, message] of [
  ["NotAllowedError", "设备权限被拒绝"],
  ["NotFoundError", "未找到设备"],
  ["NotReadableError", "可能被其他程序占用"],
  ["OverconstrainedError", "所选设备或分辨率不可用"],
]) {
  test(
    "camera " + name + " preserves working microphone and chat-only entry",
    async ({ page, request }) => {
      await prepare(page, request);
      await page.evaluate((name) => {
        const get = navigator.mediaDevices.getUserMedia.bind(
          navigator.mediaDevices,
        );
        navigator.mediaDevices.getUserMedia = (c) =>
          c.video
            ? Promise.reject(new DOMException("test only", name))
            : get(c);
      }, name);
      await page
        .getByRole("button", { name: "开启麦克风", exact: true })
        .click();
      await expect(page.getByRole("button", { name: "关闭麦克风", exact: true })).toBeVisible();
      await page
        .getByRole("button", { name: "开启摄像头", exact: true })
        .click();
      await expect(
        page.getByRole("alert").filter({ hasText: message }),
      ).toBeVisible();
      expect(
        await page.evaluate(
          () =>
            (window as any).captured.filter(
              (t: any) => t.readyState === "live" && t.kind === "audio",
            ).length,
        ),
      ).toBe(1);
      await page
        .getByRole("button", { name: "仅聊天参会", exact: true })
        .click();
      await page.getByRole("button", { name: "连接详情", exact: true }).click();
      await expect(page.locator(".connection-status")).toContainText("业务已连接");
      await page.getByRole("button", { name: "关闭对话框" }).click();
      await page.locator(".toolbar").getByRole("button", { name: /^聊天/ }).click();
      await expect(page.locator(".room-notice")).toContainText("音视频尚未配置");
      expect(
        await page.evaluate(() =>
          (window as any).captured.every((t: any) => t.readyState === "ended"),
        ),
      ).toBe(true);
      await page.locator("textarea").fill("设备不可用仍可聊天");
      await page.getByRole("button", { name: "发送", exact: true }).click();
      await expect(
        page.getByText("设备不可用仍可聊天", { exact: true }),
      ).toBeVisible();
    },
  );
}
test("pending capture completing after navigation stops the late track", async ({
  page,
  request,
}) => {
  await prepare(page, request);
  await page.evaluate(() => {
    const get = navigator.mediaDevices.getUserMedia.bind(
      navigator.mediaDevices,
    );
    navigator.mediaDevices.getUserMedia = async (c) => {
      const s = await get(c);
      await new Promise((r) => setTimeout(r, 500));
      return s;
    };
  });
  await page.getByRole("button", { name: "开启摄像头", exact: true }).click();
  await page.getByRole("button", { name: "仅聊天参会", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => (window as any).captured.length))
    .toBe(1);
  await expect
    .poll(() =>
      page.evaluate(() =>
        (window as any).captured.every((t: any) => t.readyState === "ended"),
      ),
    )
    .toBe(true);
});

test('media ownership: real SDK local tracks, single capture and late capture after disposal is stopped',async({page,request})=>{
 await prepare(page,request);
 const result=await page.evaluate(async()=>{
  // Only publication/transport is a test double; gUM and LocalTrack objects are real.
  const {MediaClient}=await import('/src/services/media.ts');
  const publications=new Map<string,any>();
  const local={trackPublications:publications,getTrackPublication:(source:string)=>publications.get(source),get isMicrophoneEnabled(){return publications.has('microphone');},get isCameraEnabled(){return publications.has('camera');},async publishTrack(track:any,options:any){publications.set(options.source,{source:options.source,track});},async unpublishTrack(track:any){publications.delete(track.source);track.stop();}};
  const c=new MediaClient('test-only-meeting','test-only-session',()=>{});
  c.room={state:'connected',localParticipant:local,remoteParticipants:new Map(),removeAllListeners(){},async disconnect(){publications.clear();}} as any;
  await c.toggle('microphone',true);await c.toggle('microphone',true);
  await c.toggle('camera',true);
  const provided=[...publications.values()].every(p=>p.track.isUserProvided);
  const active=(window as any).captured.filter((t:MediaStreamTrack)=>t.readyState==='live');
  await c.toggle('camera',false);
  const cameraReleased=(window as any).captured.filter((t:MediaStreamTrack)=>t.kind==='video').every((t:MediaStreamTrack)=>t.readyState==='ended');
  const get=navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices);
  navigator.mediaDevices.getUserMedia=async constraints=>{const stream=await get(constraints);await new Promise(r=>setTimeout(r,200));return stream;};
  const pending=c.toggle('camera',true);await new Promise(r=>setTimeout(r,30));c.stop();c.stop();await pending;
  return {provided,audio:active.filter((t:MediaStreamTrack)=>t.kind==='audio').length,video:active.filter((t:MediaStreamTrack)=>t.kind==='video').length,cameraReleased,allStopped:(window as any).captured.every((t:MediaStreamTrack)=>t.readyState==='ended')};
 });
 expect(result).toEqual({provided:true,audio:1,video:1,cameraReleased:true,allStopped:true});
});
