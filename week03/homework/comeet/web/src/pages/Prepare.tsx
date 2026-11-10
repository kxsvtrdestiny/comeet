import { Link, useParams, useNavigate } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import DevicePrepare from "../components/DevicePrepare";
import { useRemote } from "../services/useRemote";
import {
  staticPreview,
  formatMeetingNo,
  type Preparation,
} from "../services/types";
export default function Prepare() {
  const { id } = useParams();
  const nav = useNavigate();
  const media = useRemote<{ configured: boolean }>(
    staticPreview ? null : "/media/config",
  );
  const { data, loading, error, retry } = useRemote<Preparation>(
    staticPreview ? null : `/meetings/${encodeURIComponent(id || "")}/prepare`,
  );
  return (
    <>
      <Link to="/" className="text-button">
        <ArrowLeft size={16} />
        返回工作台
      </Link>
      <h1>入会准备</h1>
      {staticPreview ? (
        <p className="subtitle">静态预览未连接后端，没有真实会议信息。</p>
      ) : loading ? (
        <p role="status">正在查询会议信息…</p>
      ) : error ? (
        <section className="card" role="alert">
          <p className="error">{error}</p>
          <button className="text-button" onClick={retry}>
            重试
          </button>
        </section>
      ) : (
        data && (
          <section className="card meeting-info">
            <h2>{data.title}</h2>
            <p>
              会议号：<strong>{formatMeetingNo(data.meetingNo)}</strong>
            </p>
            <p>主持人：{data.hostName}</p>
            <p>状态：进行中</p>
          </section>
        )
      )}
      {staticPreview ? (
        <p className="muted">设备预览和音视频请在本机运行后测试。</p>
      ) : (
        data && (
          <>
            {media.data && !media.data.configured && (
              <p className="subtitle">
                媒体未配置，可预览设备或仅聊天参会；配置方法见 README。
              </p>
            )}
            <DevicePrepare onEnter={() => nav(`/meetings/${id}/room`)} />
          </>
        )
      )}
    </>
  );
}
