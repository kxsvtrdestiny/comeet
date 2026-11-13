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
test("sharing lifecycle: cancel, system stop, publish failure, pending picker/publish disposal and one-shot muted mic reopen", async ({
  page,
}) => {
  await page.goto("/login");
  const result = await page.evaluate(async () => {
    // Explicit test doubles: display capture comes from a fake camera, publication
    // is an in-memory transport. These assertions are not SFU/screen-content proof.
    const { MediaClient } = await import("/src/services/media.ts");
    const { LocalAudioTrack } = await import(
      "/node_modules/.vite/deps/livekit-client.js"
    );
    const captured: MediaStreamTrack[] = [];
    let failUnpublish = false;
    let calls = 0,
      cancel = false,
      fail = false;
    let delayPicker: (() => void) | null = null,
      delayPublish: (() => void) | null = null;
    let waitPicker = false,
      waitPublish = false;
    navigator.mediaDevices.getDisplayMedia = async () => {
      calls++;
      if (cancel) throw new DOMException("cancelled", "NotAllowedError");
      const stream = await navigator.mediaDevices.getUserMedia({
        video: true,
        audio: false,
      });
      captured.push(...stream.getTracks());
      if (waitPicker)
        await new Promise<void>((r) => {
          delayPicker = r;
        });
      return stream;
    };
    const pubs = new Map<string, any>();
    const local = {
      trackPublications: pubs,
      getTrackPublication: (s: string) => pubs.get(s),
      get isScreenShareEnabled() {
        return pubs.has("screen_share");
      },
      get isMicrophoneEnabled() {
        return (
          !!pubs.get("microphone") && !pubs.get("microphone").track.isMuted
        );
      },
      get isCameraEnabled() {
        return false;
      },
      async publishTrack(track: any, o: any) {
        if (waitPublish)
          await new Promise<void>((r) => {
            delayPublish = r;
          });
        if (fail) throw Error("publish failed");
        pubs.set(o.source, { track, source: o.source });
      },
      async unpublishTrack(track: any) {
        if (failUnpublish) throw Error("cleanup failed");
        pubs.delete(track.source);
        track.stop();
      },
    };
    let state: any;
    const c = new MediaClient("test-room", "test-session", (s: any) => {
      state = s;
    });
    c.room = {
      state: "connected",
      localParticipant: local,
      remoteParticipants: new Map(),
      removeAllListeners() {},
      async disconnect() {
        pubs.clear();
      },
    } as any;
    cancel = true;
    await c.startScreenShare();
    const cancellation = pubs.size === 0 && !state.sharingBusy && !state.error;
    cancel = false;
    await c.startScreenShare();
    await c.startScreenShare();
    const single = pubs.size === 1 && calls === 2;
    const share = pubs.get("screen_share").track;
    share.mediaStreamTrack.dispatchEvent(new Event("ended"));
    await new Promise((r) => setTimeout(r, 30));
    const systemStop =
      pubs.size === 0 &&
      captured.every((t) => t.readyState === "ended") &&
      !state.screenSharing;
    fail = true;
    await c.startScreenShare();
    const publicationFailure =
      pubs.size === 0 &&
      captured.every((t) => t.readyState === "ended") &&
      !!state.error;
    fail = false;
    waitPublish = true;
    const publishing = c.startScreenShare();
    while (!delayPublish) await new Promise((r) => setTimeout(r, 10));
    await c.stopScreenShare();
    delayPublish!();
    await publishing;
    waitPublish = false;
    const stopDuringPublish =
      pubs.size === 0 && captured.every((t) => t.readyState === "ended");
    await c.startScreenShare();
    failUnpublish = true;
    await c.stopScreenShare();
    const cleanupFailure =
      state.sharingCleanupPending &&
      !state.screenSharing &&
      captured.every((t) => t.readyState === "ended");
    failUnpublish = false;
    await c.stopScreenShare();
    const cleanupRetried = !state.sharingCleanupPending && pubs.size === 0;
    const micStream = await navigator.mediaDevices.getUserMedia({
      audio: true,
      video: false,
    });
    captured.push(...micStream.getTracks());
    const mic = new LocalAudioTrack(
      micStream.getAudioTracks()[0],
      undefined,
      true,
    );
    mic.source = "microphone" as any;
    await mic.mute();
    pubs.set("microphone", { source: "microphone", track: mic });
    await c.toggle("microphone", true);
    const reopen = !mic.isMuted;
    mic.stop();
    pubs.delete("microphone");
    waitPicker = true;
    const picking = c.startScreenShare();
    while (!delayPicker) await new Promise((r) => setTimeout(r, 10));
    c.stop();
    c.stop();
    delayPicker!();
    await picking;
    return {
      cancellation,
      single,
      systemStop,
      publicationFailure,
      stopDuringPublish,
      reopen,
      cleanupFailure,
      cleanupRetried,
      allReleased: captured.every((t) => t.readyState === "ended"),
      empty: pubs.size === 0,
    };
  });
  expect(result).toEqual({
    cancellation: true,
    single: true,
    systemStop: true,
    publicationFailure: true,
    stopDuringPublish: true,
    reopen: true,
    cleanupFailure: true,
    cleanupRetried: true,
    allReleased: true,
    empty: true,
  });
});
