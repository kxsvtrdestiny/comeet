import { useState } from "react";
import { Link } from "react-router-dom";
import { Monitor, Users, MessageSquare, MicOff, VideoOff } from "lucide-react";
import { Disabled, Empty } from "../components/Common";
export default function Room() {
  const [panel, setPanel] = useState<"members" | "chat">("members");
  return (
    <div className="room">
      <header className="room-header">
        <div>
          <span className="tag">会议室骨架</span>
          <h1>等待连接会议</h1>
          <p>业务未连接 · 媒体未连接 · 当前没有真实会议</p>
        </div>
        <Link to="/" className="room-back">
          返回工作台
        </Link>
      </header>
      <div className="room-body">
        <div className="room-stage">
          <Monitor size={48} />
          <h2>尚未连接会议</h2>
          <p>第 3 阶段接通成员与聊天，第 4 阶段接入真实音视频。</p>
        </div>
        <aside className="room-panel">
          <div className="tabs">
            <button
              onClick={() => setPanel("members")}
              aria-pressed={panel === "members"}
            >
              <Users size={17} />
              成员
            </button>
            <button
              onClick={() => setPanel("chat")}
              aria-pressed={panel === "chat"}
            >
              <MessageSquare size={17} />
              聊天
            </button>
          </div>
          <Empty
            title={panel === "members" ? "尚未连接成员列表" : "尚未连接聊天"}
          >
            业务连接将在第 3 阶段开放。
          </Empty>
        </aside>
      </div>
      <footer className="toolbar">
        <Disabled stage={4}>
          <MicOff size={18} />
          麦克风
        </Disabled>
        <Disabled stage={4}>
          <VideoOff size={18} />
          摄像头
        </Disabled>
        <Disabled stage={5}>
          <Monitor size={18} />
          共享屏幕
        </Disabled>
        <Disabled stage={3}>离开会议</Disabled>
      </footer>
    </div>
  );
}
