import { Plus, LogIn } from "lucide-react";
import HealthPanel from "../components/HealthPanel";
import { Disabled, Empty } from "../components/Common";
export default function Dashboard() {
  return (
    <>
      <h1>工作台</h1>
      <div className="actions">
        <section className="card action-card">
          <div className="action-icon">
            <Plus />
          </div>
          <h2>发起即时会议</h2>
          <p>邀请伙伴，一起讨论下一步。</p>
          <Disabled>创建会议</Disabled>
        </section>
        <section className="card action-card">
          <div className="action-icon secondary">
            <LogIn />
          </div>
          <h2>通过会议号加入</h2>
          <p>与团队快速会合。</p>
          <label className="sr-only" htmlFor="meeting-no">
            会议号
          </label>
          <input
            id="meeting-no"
            placeholder="会议号将在第 2 阶段启用"
            disabled
          />
          <Disabled>加入会议</Disabled>
        </section>
      </div>
      <section className="card">
        <div className="section-title">
          <h2>我的会议</h2>
          <span className="tag">第 2 阶段接入</span>
        </div>
        <Empty title="尚未接入会议数据">
          这里将展示你进行中的会议和近期记录。当前没有加载或创建任何会议。
        </Empty>
      </section>
      <HealthPanel />
    </>
  );
}
