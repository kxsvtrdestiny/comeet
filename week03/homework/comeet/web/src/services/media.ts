import {
  Room,
  RoomEvent,
  TrackEvent,
  LocalAudioTrack,
  LocalVideoTrack,
  type LocalTrack,
  Track,
  VideoPresets,
  ConnectionState,
  type Participant,
} from "livekit-client";
import { request } from "./api";
import {
  preferences,
  savePreferences,
  deviceError,
  type DevicePreferences,
} from "./devices";
export interface MediaState {
  status:
    | "idle"
    | "connecting"
    | "connected"
    | "reconnecting"
    | "error"
    | "unconfigured";
  notice: string;
  room: Room | null;
  revision: number;
  playbackBlocked: boolean;
  busy: boolean;
  error: string;
  microphone: boolean;
  camera: boolean;
  screenSharing: boolean;
  sharingBusy: boolean;
  sharingCleanupPending: boolean;
}
export const initialMedia: MediaState = {
  status: "idle",
  notice: "媒体未连接",
  room: null,
  revision: 0,
  playbackBlocked: false,
  busy: false,
  error: "",
  microphone: false,
  camera: false,
  screenSharing: false,
  sharingBusy: false,
  sharingCleanupPending: false,
};
// One owner per mounted room. Deferred start and synchronous invalidation make StrictMode safe.
export class MediaClient {
  private static closing: Promise<unknown> = Promise.resolve();
  private starting = false;
  private owned = new Map<LocalTrack, () => void>();
  room: Room | null = null;
  private disposed = false;
  private abort = new AbortController();
  private state = { ...initialMedia };
  private busy = false;
  private shareEpoch = 0;
  private screenTrack: LocalVideoTrack | null = null;
  constructor(
    private meetingId: string,
    private sessionId: string,
    private emit: (s: MediaState) => void,
  ) {}
  private update(p: Partial<MediaState> = {}) {
    if (this.disposed) return;
    this.state = { ...this.state, ...p, revision: this.state.revision + 1 };
    this.emit(this.state);
  }
  private stopOwned(track: LocalTrack) {
    this.owned.get(track)?.();
    this.owned.delete(track);
    track.detach().forEach((el) => {
      el.pause();
      el.srcObject = null;
    });
    track.stop();
  }
  private releaseTracks() {
    this.shareEpoch++;
    this.screenTrack = null;
    for (const track of this.owned.keys()) this.stopOwned(track);
    this.room?.remoteParticipants.forEach((participant) =>
      participant.trackPublications.forEach((p) =>
        p.track?.detach().forEach((el) => {
          el.pause();
          el.srcObject = null;
        }),
      ),
    );

    this.room?.localParticipant.trackPublications.forEach((p) => {
      p.track?.detach().forEach((e) => {
        e.pause();
        e.srcObject = null;
      });
      p.track?.stop();
    });
  }
  async start() {
    if (this.disposed || this.room || this.starting) return;
    this.starting = true;
    await MediaClient.closing;
    if (this.disposed) return;
    this.update({ status: "connecting", notice: "媒体正在连接…" });
    try {
      const grant = await request<{
        url: string;
        token: string;
        identity: string;
        room: string;
      }>("/meetings/" + encodeURIComponent(this.meetingId) + "/media-token", {
        method: "POST",
        body: JSON.stringify({ sessionId: this.sessionId }),
        signal: this.abort.signal,
      });
      if (this.disposed) return;
      const prefs = preferences();
      const room = new Room({
        adaptiveStream: true,
        dynacast: true,
        stopLocalTrackOnUnpublish: true,
        disconnectOnPageLeave: true,
        videoCaptureDefaults: {
          resolution: { width: 1280, height: 720, frameRate: 30 },
          deviceId: prefs.videoInput || undefined,
        },
        audioCaptureDefaults: {
          deviceId: prefs.audioInput || undefined,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
        publishDefaults: {
          videoCodec: "vp8",
          simulcast: true,
          videoSimulcastLayers: [VideoPresets.h180, VideoPresets.h360],
          videoEncoding: { maxBitrate: 1700000, maxFramerate: 30 },
          stopMicTrackOnMute: true,
        },
      });
      this.room = room;
      const refresh = () =>
        this.update({
          room,
          microphone: room.localParticipant.isMicrophoneEnabled,
          camera: room.localParticipant.isCameraEnabled,
          screenSharing: room.localParticipant.isScreenShareEnabled,
          playbackBlocked: !room.canPlaybackAudio,
        });
      for (const event of [
        RoomEvent.TrackSubscribed,
        RoomEvent.TrackUnsubscribed,
        RoomEvent.TrackMuted,
        RoomEvent.TrackUnmuted,
        RoomEvent.LocalTrackPublished,
        RoomEvent.LocalTrackUnpublished,
        RoomEvent.ParticipantConnected,
        RoomEvent.ParticipantDisconnected,
        RoomEvent.AudioPlaybackStatusChanged,
      ])
        room.on(event, refresh);
      room.on(RoomEvent.TrackMuted, (publication, participant) => {
        if (
          participant === room.localParticipant &&
          publication.source === Track.Source.Microphone
        ) {
          savePreferences({ ...preferences(), microphone: false });
          this.update({
            microphone: false,
            notice: "麦克风已静音，可自行重新开启",
          });
        }
      });
      room.on(RoomEvent.ConnectionStateChanged, (value: ConnectionState) => {
        if (value === ConnectionState.Connected)
          this.update({ status: "connected", notice: "媒体已连接" });
        else if (
          value === ConnectionState.Reconnecting ||
          value === ConnectionState.SignalReconnecting
        )
          this.update({
            status: "reconnecting",
            notice: "媒体重连中，文字会议可继续",
          });
      });
      room.on(RoomEvent.Disconnected, () => {
        this.releaseTracks();
        this.update({
          status: "error",
          notice: "媒体已断开，可重试连接或继续文字会议",
          microphone: false,
          camera: false,
          screenSharing: false,
          sharingBusy: false,
        });
      });
      room.on(RoomEvent.MediaDevicesError, () =>
        this.update({
          error: "设备不可用，请检查权限、设备连接和其他程序占用。",
        }),
      );
      await room.connect(grant.url, grant.token, { autoSubscribe: true });
      if (this.disposed) {
        await room.disconnect(true);
        this.releaseTracks();
        return;
      }
      if (prefs.audioOutput)
        try {
          await room.switchActiveDevice("audiooutput", prefs.audioOutput);
        } catch {
          this.update({ error: "扬声器切换失败，正在使用系统默认输出。" });
        }
      this.update({ room, status: "connected", notice: "媒体已连接" });
      // Each capture is independent: failure of one never discards the other.
      if (prefs.microphone) await this.toggle("microphone", true);
      if (!this.disposed && prefs.camera) await this.toggle("camera", true);
      refresh();
    } catch (e) {
      if (this.disposed) return;
      this.releaseTracks();
      if (this.room) void this.room.disconnect(true);
      const missing = (e as { code?: string }).code === "MEDIA_NOT_CONFIGURED";
      this.update({
        status: missing ? "unconfigured" : "error",
        notice: missing
          ? "媒体未配置，可继续文字会议"
          : "媒体连接失败，请检查 LiveKit 配置与网络，文字会议可继续",
        error: "",
      });
    }
  }
  async toggle(kind: "microphone" | "camera", enabled: boolean) {
    const room = this.room;
    if (
      this.disposed ||
      !room ||
      room.state !== ConnectionState.Connected ||
      this.busy
    )
      return;
    this.busy = true;
    this.update({ busy: true, error: "" });
    try {
      const source =
        kind === "microphone" ? Track.Source.Microphone : Track.Source.Camera;
      const existing = room.localParticipant.getTrackPublication(source)?.track;
      if (!enabled) {
        if (existing) {
          this.stopOwned(existing);
          await room.localParticipant.unpublishTrack(existing, true);
        }
      } else if (existing) {
        // A host mute is a one-shot mute, never a publish-permission lock.
        await existing.unmute();
      } else {
        const prefs = preferences();
        const deviceId =
          kind === "microphone" ? prefs.audioInput : prefs.videoInput;
        const constraints: MediaTrackConstraints =
          kind === "microphone"
            ? {
                echoCancellation: true,
                noiseSuppression: true,
                autoGainControl: true,
              }
            : {
                width: { ideal: 1280 },
                height: { ideal: 720 },
                frameRate: { ideal: 30 },
              };
        if (deviceId) constraints.deviceId = { exact: deviceId };
        const stream = await navigator.mediaDevices.getUserMedia(
          kind === "microphone"
            ? { audio: constraints, video: false }
            : { audio: false, video: constraints },
        );
        const raw = stream.getTracks()[0];
        if (this.disposed) {
          stream.getTracks().forEach((t) => t.stop());
          return;
        }
        // User-provided tracks prevent SDK hot-unplug fallback to another device.
        const track =
          kind === "microphone"
            ? new LocalAudioTrack(raw, constraints, true)
            : new LocalVideoTrack(raw, constraints, true);
        track.source = source;
        const ended = () => {
          if (this.disposed) return;
          savePreferences({ ...preferences(), [kind]: false });
          this.stopOwned(track);
          void room.localParticipant
            .unpublishTrack(track, true)
            .catch(() => {})
            .finally(() =>
              this.update({
                [kind]: false,
                error: "设备已断开，请选择其他设备。",
              }),
            );
        };
        track.on(TrackEvent.Ended, ended);
        this.owned.set(track, () => track.off(TrackEvent.Ended, ended));
        try {
          await room.localParticipant.publishTrack(track, { source });
        } catch (e) {
          this.stopOwned(track);
          throw e;
        }
      }
      if (this.disposed) {
        this.releaseTracks();
        return;
      }
      const on =
        kind === "microphone"
          ? room.localParticipant.isMicrophoneEnabled
          : room.localParticipant.isCameraEnabled;
      savePreferences({ ...preferences(), [kind]: on });
      this.update({ [kind]: on });
    } catch (e) {
      if (!this.disposed) {
        savePreferences({ ...preferences(), [kind]: false });
        this.update({ error: deviceError(e), [kind]: false });
      }
    } finally {
      this.busy = false;
      if (this.disposed) this.releaseTracks();
      else this.update({ busy: false });
    }
  }
  // Called directly from a button handler: getDisplayMedia starts before any await.
  async startScreenShare() {
    const room = this.room;
    if (
      this.disposed ||
      !room ||
      room.state !== ConnectionState.Connected ||
      this.state.sharingBusy ||
      this.screenTrack
    )
      return;
    const epoch = ++this.shareEpoch;
    this.update({ sharingBusy: true, error: "" });
    let stream: MediaStream | undefined;
    let track: LocalVideoTrack | undefined;
    try {
      stream = await navigator.mediaDevices.getDisplayMedia({
        video: { frameRate: 15 },
        audio: false,
      });
      if (
        this.disposed ||
        epoch !== this.shareEpoch ||
        room.state !== ConnectionState.Connected
      ) {
        stream.getTracks().forEach((t) => t.stop());
        return;
      }
      const raw = stream.getVideoTracks()[0];
      if (!raw || raw.readyState === "ended") {
        stream.getTracks().forEach((t) => t.stop());
        return;
      }
      // This stage shares video only; an unexpected audio track is never published.
      stream.getAudioTracks().forEach((t) => t.stop());
      track = new LocalVideoTrack(raw, undefined, true);
      track.source = Track.Source.ScreenShare;
      const ended = () => {
        void this.stopScreenShare();
      };
      raw.addEventListener("ended", ended);
      this.owned.set(track, () => raw.removeEventListener("ended", ended));
      this.screenTrack = track;
      await room.localParticipant.publishTrack(track, {
        source: Track.Source.ScreenShare,
        simulcast: false,
        videoEncoding: { maxBitrate: 3000000, maxFramerate: 15 },
      });
      if (
        this.disposed ||
        epoch !== this.shareEpoch ||
        (raw.readyState as string) === "ended"
      ) {
        this.stopOwned(track);
        await room.localParticipant.unpublishTrack(track, true);
        return;
      }
      this.update({ screenSharing: true });
    } catch (e) {
      if (track) {
        this.stopOwned(track);
        if (this.screenTrack === track) this.screenTrack = null;
        await room.localParticipant.unpublishTrack(track, true).catch(() => {});
      }
      stream?.getTracks().forEach((t) => t.stop());
      if (
        !this.disposed &&
        epoch === this.shareEpoch &&
        (e as Error).name !== "NotAllowedError" &&
        (e as Error).name !== "AbortError"
      )
        this.update({ error: "共享未能启动，请检查屏幕录制权限或重试。" });
    } finally {
      if (epoch === this.shareEpoch)
        this.update({ sharingBusy: false, screenSharing: !!this.screenTrack });
      if (this.disposed) this.releaseTracks();
    }
  }
  async stopScreenShare() {
    ++this.shareEpoch;
    const track = this.screenTrack;
    this.screenTrack = null;
    if (track) this.stopOwned(track);
    this.update({
      screenSharing: false,
      sharingBusy: false,
      sharingCleanupPending: false,
    });
    if (track && this.room) {
      try {
        await this.room.localParticipant.unpublishTrack(track, true);
      } catch {
        if (!this.disposed) this.screenTrack = track;
        this.update({
          sharingCleanupPending: true,
          error:
            "本地共享已停止，但发布清理失败，请重试停止共享或重新连接音视频。",
        });
      }
    }
  }
  async switchDevice(kind: MediaDeviceKind, id: string) {
    const room = this.room;
    if (this.disposed || !room || this.busy) return;
    this.busy = true;
    this.update({ busy: true, error: "" });
    try {
      const ok = await room.switchActiveDevice(kind, id || "default");
      if (!ok) throw Error("switch");
      if (this.disposed) {
        this.releaseTracks();
        return;
      }
      const key: keyof DevicePreferences =
        kind === "audioinput"
          ? "audioInput"
          : kind === "videoinput"
            ? "videoInput"
            : "audioOutput";
      savePreferences({ ...preferences(), [key]: id });
      this.update();
    } catch (e) {
      this.update({ error: deviceError(e) });
    } finally {
      this.busy = false;
      if (this.disposed) this.releaseTracks();
      else this.update({ busy: false });
    }
  }
  async startAudio() {
    try {
      await this.room?.startAudio();
      this.update({ playbackBlocked: !this.room?.canPlaybackAudio });
    } catch {
      this.update({
        error: "播放被浏览器阻止，请点击启用声音并检查浏览器站点声音权限。",
      });
    }
  }
  stop() {
    if (this.disposed) return;
    this.disposed = true;
    this.abort.abort();

    this.releaseTracks();
    const room = this.room;
    if (room) {
      room.removeAllListeners();
      MediaClient.closing = room
        .disconnect(true)
        .finally(() => this.releaseTracks())
        .catch(() => {});
    }
  }
}
export function cameraTrack(participant?: Participant) {
  return participant?.getTrackPublication(Track.Source.Camera)?.track;
}
