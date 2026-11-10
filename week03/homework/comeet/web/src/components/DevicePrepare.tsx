import { useEffect, useRef, useState } from "react";
import {
  deviceError,
  preferences,
  savePreferences,
  stopStream,
  type DevicePreferences,
} from "../services/devices";
import { MicrophoneLevel, SpeakerTest, outputSupported } from "./DeviceTools";
export default function DevicePrepare({
  onEnter,
}: {
  onEnter: (chatOnly: boolean) => void;
}) {
  const [prefs, setPrefs] = useState(() => ({
      ...preferences(),
      microphone: false,
      camera: false,
    })),
    [devices, setDevices] = useState<MediaDeviceInfo[]>([]),
    [errors, setErrors] = useState({ microphone: "", camera: "", list: "" }),
    [busy, setBusy] = useState({ microphone: false, camera: false });
  const [mic, setMic] = useState<MediaStreamTrack>(),
    [camera, setCamera] = useState<MediaStream>();
  const state = useRef(prefs),
    streams = useRef<{
      microphone: MediaStream | null;
      camera: MediaStream | null;
    }>({ microphone: null, camera: null }),
    generation = useRef({ microphone: 0, camera: 0 }),
    alive = useRef(true),
    video = useRef<HTMLVideoElement>(null);
  function update(patch: Partial<DevicePreferences>) {
    state.current = { ...state.current, ...patch };
    savePreferences(state.current);
    setPrefs(state.current);
  }
  async function refresh() {
    try {
      if (!navigator.mediaDevices)
        throw new DOMException("secure context", "SecurityError");
      const all = await navigator.mediaDevices.enumerateDevices();
      if (alive.current) {
        setDevices(all);
        setErrors((e) => ({ ...e, list: "" }));
      }
    } catch (e) {
      if (alive.current) setErrors((v) => ({ ...v, list: deviceError(e) }));
    }
  }
  function halt(kind: "microphone" | "camera") {
    generation.current[kind]++;
    stopStream(streams.current[kind]);
    streams.current[kind] = null;
    if (kind === "camera") setCamera(undefined);
    else setMic(undefined);
  }
  async function capture(
    kind: "microphone" | "camera",
    enabled: boolean,
    id?: string,
  ) {
    halt(kind);
    update({
      [kind]: enabled,
      ...(id !== undefined
        ? { [kind === "microphone" ? "audioInput" : "videoInput"]: id }
        : {}),
    });
    const gen = generation.current[kind];
    setBusy((v) => ({ ...v, [kind]: enabled }));
    setErrors((v) => ({ ...v, [kind]: "" }));
    if (!enabled) return;
    try {
      if (!navigator.mediaDevices)
        throw new DOMException("insecure", "SecurityError");
      const deviceId =
        kind === "microphone"
          ? state.current.audioInput
          : state.current.videoInput;
      const stream = await navigator.mediaDevices.getUserMedia(
        kind === "microphone"
          ? {
              audio: deviceId ? { deviceId: { exact: deviceId } } : true,
              video: false,
            }
          : {
              audio: false,
              video: {
                width: { ideal: 1280 },
                height: { ideal: 720 },
                frameRate: { ideal: 30 },
                ...(deviceId ? { deviceId: { exact: deviceId } } : {}),
              },
            },
      );
      if (!alive.current || gen !== generation.current[kind]) {
        stopStream(stream);
        return;
      }
      streams.current[kind] = stream;
      const track = stream.getTracks()[0];
      track.onended = () => {
        if (alive.current && generation.current[kind] === gen) {
          halt(kind);
          update({ [kind]: false });
          setErrors((v) => ({ ...v, [kind]: "设备已断开，请选择其他设备。" }));
        }
      };
      if (kind === "microphone") setMic(track);
      else setCamera(stream);
      await refresh();
    } catch (e) {
      if (alive.current && gen === generation.current[kind]) {
        update({ [kind]: false });
        setErrors((v) => ({ ...v, [kind]: deviceError(e) }));
      }
    } finally {
      if (alive.current && gen === generation.current[kind])
        setBusy((v) => ({ ...v, [kind]: false }));
    }
  }
  useEffect(() => {
    alive.current = true;
    void refresh();
    const change = () => void refresh();
    const leave = () => {
      halt("microphone");
      halt("camera");
      update({ microphone: false, camera: false });
    };
    window.addEventListener("pagehide", leave);
    navigator.mediaDevices?.addEventListener("devicechange", change);
    return () => {
      window.removeEventListener("pagehide", leave);
      alive.current = false;
      generation.current.microphone++;
      generation.current.camera++;
      stopStream(streams.current.microphone);
      stopStream(streams.current.camera);
      streams.current = { microphone: null, camera: null };
      navigator.mediaDevices?.removeEventListener("devicechange", change);
    };
  }, []);
  useEffect(() => {
    if (video.current) video.current.srcObject = camera || null;
    return () => {
      if (video.current) video.current.srcObject = null;
    };
  }, [camera]);
  // Preparation always starts without capture. Stored choices are only used on explicit entry.
  function enter(chatOnly: boolean) {
    halt("microphone");
    halt("camera");
    if (chatOnly) update({ microphone: false, camera: false });
    else savePreferences(state.current);
    onEnter(chatOnly);
  }
  return (
    <div className="prepare-grid">
      <div className="preview">
        {camera ? (
          <video
            ref={video}
            autoPlay
            playsInline
            muted
            aria-label="摄像头预览"
          />
        ) : (
          <>
            <h2>摄像头未开启</h2>
            <p>可分别开启设备预览，也可仅聊天参会。</p>
          </>
        )}
      </div>
      <section className="card device-controls">
        <h2>设备准备</h2>
        {(["microphone", "camera"] as const).map((kind) => (
          <div key={kind}>
            <label>
              {kind === "microphone" ? "麦克风" : "摄像头"}
              <select
                aria-label={kind === "microphone" ? "麦克风设备" : "摄像头设备"}
                value={
                  kind === "microphone" ? prefs.audioInput : prefs.videoInput
                }
                onChange={(e) =>
                  void capture(kind, !!streams.current[kind], e.target.value)
                }
              >
                <option value="">系统默认</option>
                {devices
                  .filter(
                    (d) =>
                      d.kind ===
                      (kind === "microphone" ? "audioinput" : "videoinput"),
                  )
                  .map((d, i) => (
                    <option key={d.deviceId || i} value={d.deviceId}>
                      {d.label || `设备 ${i + 1}`}
                    </option>
                  ))}
              </select>
            </label>
            <button
              type="button"
              className="text-button"
              disabled={busy[kind]}
              onClick={() => void capture(kind, !streams.current[kind])}
            >
              {streams.current[kind] ? "关闭" : "开启"}
              {kind === "microphone" ? "麦克风" : "摄像头"}
            </button>
            {errors[kind] && (
              <p className="error" role="alert">
                {errors[kind]}
              </p>
            )}
          </div>
        ))}
        <MicrophoneLevel track={mic} />
        {outputSupported() ? (
          <label>
            扬声器
            <select
              aria-label="扬声器设备"
              value={prefs.audioOutput}
              onChange={(e) => update({ audioOutput: e.target.value })}
            >
              <option value="">系统默认</option>
              {devices
                .filter((d) => d.kind === "audiooutput")
                .map((d, i) => (
                  <option key={d.deviceId || i} value={d.deviceId}>
                    {d.label || `扬声器 ${i + 1}`}
                  </option>
                ))}
            </select>
          </label>
        ) : (
          <p className="muted">此浏览器使用系统默认扬声器。</p>
        )}
        <SpeakerTest deviceId={prefs.audioOutput} />
        <button className="text-button" onClick={() => void refresh()}>
          刷新设备列表
        </button>
        {errors.list && (
          <p role="alert" className="error">
            {errors.list}
          </p>
        )}
        <button
          className="button"
          disabled={busy.microphone || busy.camera}
          onClick={() => enter(false)}
        >
          进入会议
        </button>
        <button className="text-button" onClick={() => enter(true)}>
          仅聊天参会
        </button>
        <p className="muted">点击进入后才正式入会，准备页不发布音视频。</p>
      </section>
    </div>
  );
}
