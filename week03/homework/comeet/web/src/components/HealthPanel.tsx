import { useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { getHealth, type Health } from "../services/api";
export default function HealthPanel() {
  if (import.meta.env.MODE === "pages") return (
    <section className="card health" aria-label="静态预览说明">
      <h2>第一阶段 · 静态界面预览</h2>
      <p>此测试站仅展示界面，不运行 Go 和 SQLite，尚未连接业务服务。</p>
      <p className="muted">数据库健康检查需在 Windows 按 README 启动后端后验收；此处不展示模拟成功状态。</p>
      <a className="text-button" href={`${import.meta.env.BASE_URL}comeet-stage1.zip`} download>下载第一阶段源码 ZIP</a>
    </section>
  );
  return <LiveHealthPanel />;
}
function LiveHealthPanel() {
  const [health, setHealth] = useState<Health>();
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setLoading(true);
    setError("");
    const timer = setTimeout(() => controller.abort(), 8000);
    getHealth(controller.signal)
      .then((h) => {
        if (active) setHealth(h);
      })
      .catch(() => {
        if (active)
          setError("无法连接业务服务或请求超时，请检查 Go 是否已启动后重试。");
      })
      .finally(() => {
        clearTimeout(timer);
        if (active) setLoading(false);
      });
    return () => {
      active = false;
      clearTimeout(timer);
      controller.abort();
    };
  }, [attempt]);
  return (
    <section className="card health" aria-label="服务健康检查">
      <div className="section-title">
        <h2>开发环境状态</h2>
        <button
          className="text-button"
          onClick={() => setAttempt((n) => n + 1)}
          disabled={loading}
        >
          <RefreshCw size={15} />
          重新检查
        </button>
      </div>
      <div aria-live="polite">
        {loading ? (
          <p>正在请求 Go 健康检查…</p>
        ) : error ? (
          <p role="alert" className="error">
            {error}
          </p>
        ) : (
          health && (
            <>
              <p className="success">
                ● 数据库初始化正常{" "}
                <span>SQLite · 迁移 {health.schemaVersion}</span>
              </p>
              <div className="health-grid">
                <p>
                  业务服务<strong>已连接</strong>
                </p>
                <p>
                  媒体配置
                  <strong>
                    {health.mediaConfigured
                      ? "已填写 · 待第 4 阶段联调"
                      : "未配置 · 第 4 阶段接入"}
                  </strong>
                </p>
                <p>
                  AI 配置
                  <strong>
                    {health.aiConfigured
                      ? "已填写 · 待第 7 阶段联调"
                      : "未配置 · 第 7 阶段接入"}
                  </strong>
                </p>
              </div>
              <p className="muted">配置状态不代表外部服务联调通过。</p>
            </>
          )
        )}
      </div>
    </section>
  );
}
