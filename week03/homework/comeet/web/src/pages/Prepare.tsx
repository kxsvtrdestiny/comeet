import { Link } from "react-router-dom";
import { ArrowLeft, VideoOff } from "lucide-react";
import { Disabled } from "../components/Common";
export default function Prepare() {
  return (
    <>
      <Link to="/" className="text-button">
        <ArrowLeft size={16} />
        返回工作台
      </Link>
      <h1>入会前，准备一下。</h1>
      <p className="subtitle">准备页骨架 · 当前未查询或加入任何真实会议。</p>
      <div className="prepare-grid">
        <div className="preview">
          <VideoOff size={40} />
          <h2>尚未启用设备预览</h2>
          <p>第 4 阶段接入摄像头和麦克风，不会在此阶段申请权限。</p>
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
          <Disabled stage={3}>进入会议</Disabled>
          <p className="muted">正式入会与文字交流将在第 3 阶段接通。</p>
        </section>
      </div>
    </>
  );
}
