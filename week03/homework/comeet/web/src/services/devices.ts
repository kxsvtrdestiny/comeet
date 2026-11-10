export interface DevicePreferences {
  microphone: boolean;
  camera: boolean;
  audioInput: string;
  videoInput: string;
  audioOutput: string;
}
const key = "comeet.devices.v1";
export function preferences(): DevicePreferences {
  try {
    return {
      microphone: false,
      camera: false,
      audioInput: "",
      videoInput: "",
      audioOutput: "",
      ...JSON.parse(sessionStorage.getItem(key) || "{}"),
    };
  } catch {
    return {
      microphone: false,
      camera: false,
      audioInput: "",
      videoInput: "",
      audioOutput: "",
    };
  }
}
export function savePreferences(p: DevicePreferences) {
  sessionStorage.setItem(key, JSON.stringify(p));
}
export function deviceError(error: unknown): string {
  const name = (error as Error)?.name;
  if (name === "NotAllowedError" || name === "PermissionDeniedError")
    return "设备权限被拒绝，请在浏览器地址栏的权限设置中允许，或仅聊天参会。";
  if (name === "NotFoundError" || name === "DevicesNotFoundError")
    return "未找到设备，请连接设备后刷新列表，或仅聊天参会。";
  if (name === "NotReadableError" || name === "TrackStartError")
    return "设备无法读取，可能被其他程序占用，请关闭占用程序后重试。";
  if (name === "OverconstrainedError")
    return "所选设备或分辨率不可用，请更换设备后重试。";
  if (name === "SecurityError" || !window.isSecureContext)
    return "设备采集需要 HTTPS 或 localhost 安全环境。";
  return "设备操作失败，请检查设备、权限或更换设备后重试。";
}
export function stopStream(stream: MediaStream | null) {
  stream?.getTracks().forEach((t) => t.stop());
}
