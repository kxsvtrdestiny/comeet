import { Link, useParams } from "react-router-dom";
import { ArrowLeft, VideoOff } from "lucide-react";
import { Disabled } from "../components/Common";
import { useRemote } from "../services/useRemote";
import {
  staticPreview,
  formatMeetingNo,
  type Preparation,
} from "../services/types";
export default function Prepare() {
  const { id } = useParams();
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
      {(staticPreview || data) && (
        <div className="prepare-grid">
          <div className="preview">
            <VideoOff size={40} />
            <h2>尚未启用设备预览</h2>
            <p>第 4 阶段接入设备；当前不会申请摄像头或麦克风权限。</p>
          </div>
          <section className="card">
            <h2>设备准备</h2>
            <label>
              麦克风
              <select disabled>
                <option>第 4 阶段开放设备选择</option>
              </select>
            </label>
            <label>
              摄像头
              <select disabled>
                <option>第 4 阶段开放设备选择</option>
              </select>
            </label>
            <Disabled stage={4}>检测设备</Disabled>
            <Link className="button" to={`/meetings/${id}/room`} aria-disabled={staticPreview} onClick={e=>{if(staticPreview)e.preventDefault()}}>进入会议</Link>
            <p className="muted">
              点击进入后才预留席位并正式入会。仅浏览准备页不会产生参会记录。
            </p>
          </section>
        </div>
      )}
    </>
  );
}
