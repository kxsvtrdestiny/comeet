import { Empty } from "../components/Common";
export default function HistoryPage({ detail = false }: { detail?: boolean }) {
  return (
    <>
      <div className="eyebrow">MEETING ARCHIVE</div>
      <h1>{detail ? "会议记录详情" : "历史会议"}</h1>
      <p className="subtitle">让每一次讨论，都有迹可循。</p>
      <section className="card">
        <Empty title="历史记录将在第 6 阶段开放">
          当前未查询历史数据，也没有演示记录。
        </Empty>
      </section>
    </>
  );
}
