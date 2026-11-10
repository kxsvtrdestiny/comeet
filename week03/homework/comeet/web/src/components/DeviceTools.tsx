import { useEffect, useRef, useState } from "react";
import { deviceError } from "../services/devices";
export function MicrophoneLevel({ track }: { track?: MediaStreamTrack }) {
  const [level, setLevel] = useState(0);
  useEffect(() => {
    if (!track) {
      setLevel(0);
      return;
    }
    let frame = 0,
      closed = false;
    const ctx = new AudioContext();
    const source = ctx.createMediaStreamSource(new MediaStream([track]));
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 256;
    source.connect(analyser);
    const samples = new Uint8Array(analyser.fftSize);
    const tick = () => {
      if (closed) return;
      analyser.getByteTimeDomainData(samples);
      let sum = 0;
      for (const v of samples) sum += ((v - 128) / 128) ** 2;
      setLevel(
        Math.min(100, Math.round(Math.sqrt(sum / samples.length) * 300)),
      );
      frame = requestAnimationFrame(tick);
    };
    void ctx
      .resume()
      .then(() => {
        if (!closed) tick();
      })
      .catch(() => {});
    const dispose = () => {
      if (closed) return;
      closed = true;
      cancelAnimationFrame(frame);
      source.disconnect();
      analyser.disconnect();
      void ctx.close();
    };
    window.addEventListener("pagehide", dispose);
    return () => {
      window.removeEventListener("pagehide", dispose);
      dispose();
    };
  }, [track]);
  return (
    <label className="mic-level">
      麦克风电平{" "}
      <meter aria-label="麦克风电平" min={0} max={100} value={level} />
    </label>
  );
}
interface SinkAudio extends HTMLAudioElement {
  setSinkId(id: string): Promise<void>;
}
export function outputSupported() {
  return (
    typeof HTMLMediaElement !== "undefined" &&
    "setSinkId" in HTMLMediaElement.prototype
  );
}
export function SpeakerTest({ deviceId }: { deviceId: string }) {
  const [playing, setPlaying] = useState(false),
    [error, setError] = useState("");
  const cleanup = useRef<(() => void) | null>(null),
    epoch = useRef(0);
  function stop() {
    epoch.current++;
    cleanup.current?.();
    cleanup.current = null;
    setPlaying(false);
  }
  useEffect(() => {
    const dispose = () => {
      epoch.current++;
      cleanup.current?.();
      cleanup.current = null;
    };
    window.addEventListener("pagehide", dispose);
    return () => {
      window.removeEventListener("pagehide", dispose);
      dispose();
    };
  }, []);
  useEffect(() => {
    stop();
  }, [deviceId]);
  async function play() {
    stop();
    setError("");
    const attempt = epoch.current;
    const ctx = new AudioContext(),
      osc = ctx.createOscillator(),
      gain = ctx.createGain(),
      dest = ctx.createMediaStreamDestination();
    const audio = new Audio() as SinkAudio;
    audio.srcObject = dest.stream;
    osc.frequency.value = 440;
    gain.gain.value = 0.08;
    osc.connect(gain);
    gain.connect(dest);
    osc.start();
    let timer: ReturnType<typeof setTimeout> | undefined;
    cleanup.current = () => {
      if (timer) clearTimeout(timer);
      audio.pause();
      audio.srcObject = null;
      try {
        osc.stop();
      } catch {}
      osc.disconnect();
      gain.disconnect();
      dest.stream.getTracks().forEach((t) => t.stop());
      void ctx.close();
    };
    try {
      await ctx.resume();
      if (deviceId && outputSupported()) await audio.setSinkId(deviceId);
      if (epoch.current !== attempt) return;
      await audio.play();
      if (epoch.current !== attempt) return;
      setPlaying(true);
      timer = setTimeout(stop, 1500);
    } catch (e) {
      if (epoch.current === attempt) {
        setError(deviceError(e));
        stop();
      }
    }
  }
  return (
    <div className="speaker-test">
      <button
        type="button"
        className="text-button"
        onClick={playing ? stop : () => void play()}
      >
        {playing ? "停止试听" : "扬声器试听"}
      </button>
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
