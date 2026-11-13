import { useEffect, useRef, useState } from "react";
import { Track, type Participant } from "livekit-client";
import { MediaClient, initialMedia, type MediaState } from "../services/media";
import { preferences } from "../services/devices";
import { MicrophoneLevel, outputSupported } from "./DeviceTools";
export function useMedia(
  meetingId: string,
  sessionId: string | undefined,
  terminal: boolean,
) {
  const [state, setState] = useState<MediaState>(initialMedia),
    [attempt, setAttempt] = useState(0);
  const client = useRef<MediaClient | null>(null);
  useEffect(() => {
    if (!sessionId || terminal) {
      setState(initialMedia);
      return;
    }
    const c = new MediaClient(meetingId, sessionId, setState);
    client.current = c;
    queueMicrotask(() => void c.start());
    const leave = () => c.stop();
    const restore = (event: PageTransitionEvent) => {
      if (event.persisted) window.location.reload();
    };
    window.addEventListener("pagehide", leave);
    window.addEventListener("pageshow", restore);
    return () => {
      window.removeEventListener("pagehide", leave);
      window.removeEventListener("pageshow", restore);
      c.stop();
      if (client.current === c) client.current = null;
    };
  }, [meetingId, sessionId, terminal, attempt]);
  return {
    state,
    client: client.current,
    retry: () => setAttempt((n) => n + 1),
    stop: () => client.current?.stop(),
  };
}
function VideoTrack({
  track,
  local,
  screen,
}: {
  track: Track;
  local: boolean;
  screen: boolean;
}) {
  const element = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    const el = element.current!;
    el.muted = true;
    track.attach(el);
    void el.play().catch(() => {});
    return () => {
      track.detach(el);
      el.pause();
      el.srcObject = null;
    };
  }, [track]);
  return (
    <video
      ref={element}
      className={screen ? "screen-video" : local ? "local-video" : ""}
      autoPlay
      playsInline
      muted
      aria-label={screen ? "共享画面" : local ? "本人摄像头" : "成员摄像头"}
    />
  );
}
export function MemberMedia({
  participant,
  local,
  screen = false,
}: {
  participant?: Participant;
  local: boolean;
  screen?: boolean;
}) {
  const publication = participant?.getTrackPublication(
      screen ? Track.Source.ScreenShare : Track.Source.Camera,
    ),
    track = publication?.track;
  return track && !publication?.isMuted ? (
    <VideoTrack track={track} local={local} screen={screen} />
  ) : null;
}
function RemoteAudio({ track }: { track: Track }) {
  const element = useRef<HTMLAudioElement>(null);
  useEffect(() => {
    const el = element.current!;
    track.attach(el);
    void el.play().catch(() => {});
    return () => {
      track.detach(el);
      el.pause();
      el.srcObject = null;
    };
  }, [track]);
  return <audio ref={element} autoPlay data-testid="remote-audio" />;
}
export function AudioSubscriptions({ state }: { state: MediaState }) {
  const tracks: Track[] = [];
  state.room?.remoteParticipants.forEach((p) =>
    p.audioTrackPublications.forEach((pub) => {
      if (pub.track && pub.source === Track.Source.Microphone)
        tracks.push(pub.track);
    }),
  );
  return (
    <div className="audio-subscriptions" aria-hidden="true">
      {tracks.map((t) => (
        <RemoteAudio key={t.sid} track={t} />
      ))}
    </div>
  );
}
export function MediaControls({
  state,
  client,
  retry,
}: {
  state: MediaState;
  client: MediaClient | null;
  retry: () => void;
}) {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([]),
    [prefs, setPrefs] = useState(preferences),
    [listError, setListError] = useState("");
  useEffect(() => {
    let alive = true;
    const refresh = () => {
      void navigator.mediaDevices
        ?.enumerateDevices()
        .then((d) => {
          if (alive) {
            setDevices(d);
            setListError("");
          }
        })
        .catch(() => {
          if (alive) setListError("无法读取设备列表，请检查浏览器权限。");
        });
    };
    refresh();
    navigator.mediaDevices?.addEventListener("devicechange", refresh);
    return () => {
      alive = false;
      navigator.mediaDevices?.removeEventListener("devicechange", refresh);
    };
  }, [state.microphone, state.camera]);
  const connected = state.status === "connected";
  const mic = state.room?.localParticipant.getTrackPublication(
    Track.Source.Microphone,
  )?.track;
  return (
    <section className="media-controls" aria-label="音视频设置">
      <p role="status">{state.notice}</p>
      {(state.status === "error" || state.status === "unconfigured") && (
        <button className="text-button" onClick={retry}>
          重试媒体连接
        </button>
      )}
      <MicrophoneLevel
        track={state.microphone ? mic?.mediaStreamTrack : undefined}
      />
      <details>
        <summary>设备选择</summary>
        {(
          [
            "audioinput",
            "videoinput",
            ...(outputSupported() ? ["audiooutput"] : []),
          ] as MediaDeviceKind[]
        ).map((kind) => {
          const key =
            kind === "audioinput"
              ? "audioInput"
              : kind === "videoinput"
                ? "videoInput"
                : "audioOutput";
          return (
            <label key={kind}>
              {kind === "audioinput"
                ? "麦克风"
                : kind === "videoinput"
                  ? "摄像头"
                  : "扬声器"}
              <select
                aria-label={"会议" + kind}
                disabled={!connected || state.busy}
                value={prefs[key]}
                onChange={async (e) => {
                  await client?.switchDevice(kind, e.target.value);
                  setPrefs(preferences());
                }}
              >
                <option value="">系统默认</option>
                {devices
                  .filter((d) => d.kind === kind)
                  .map((d, i) => (
                    <option key={d.deviceId || i} value={d.deviceId}>
                      {d.label || `设备 ${i + 1}`}
                    </option>
                  ))}
              </select>
            </label>
          );
        })}
        {!outputSupported() && <p>扬声器使用系统默认输出。</p>}
      </details>
      {(state.error || listError) && (
        <p className="error" role="alert">
          {state.error || listError}
        </p>
      )}
    </section>
  );
}
