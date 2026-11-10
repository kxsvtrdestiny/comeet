import { Link, useParams } from "react-router-dom";
import { useRemote } from "../services/useRemote";
import {
  staticPreview,
  formatMeetingNo,
  type Meeting,
} from "../services/types";
export default function MeetingDetail() {
  const { id } = useParams();
  const { data, loading, error, retry } = useRemote<Meeting>(
    staticPreview ? null : `/meetings/${encodeURIComponent(id || "")}`,
  );
  return (
    <>
      <Link to="/">返回工作台</Link>
      <h1>会议详情</h1>
      {staticPreview ? (
        <p>静态预览不提供真实会议详情。</p>
      ) : loading ? (
        <p role="status">正在加载会议…</p>
      ) : error ? (
        <section className="card" role="alert">
          <p className="error">{error}</p>
          <button className="text-button" onClick={retry}>
            重试
          </button>
        </section>
      ) : (
        data && (
          <section className="card">
            <h2>{data.title}</h2>
            <p>会议号：{formatMeetingNo(data.meetingNo)}</p>
            <p>主持人：{data.hostName}</p>
            <p>创建时间：{new Date(data.startedAt).toLocaleString("zh-CN")}</p>
            <p>
              状态：
              {data.status === "active"
                ? "进行中"
                : data.status === "ending"
                  ? "结束中"
                  : "已结束"}
            </p>
            {data.status === "active" && (
              <Link to={`/meetings/${data.id}/prepare`}>前往入会准备</Link>
            )}
          </section>
        )
      )}
    </>
  );
}
