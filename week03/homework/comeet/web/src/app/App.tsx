import { useEffect } from "react";
import { Link, NavLink, Route, Routes, useLocation } from "react-router-dom";
import { Video, LayoutDashboard, History } from "lucide-react";
import Dashboard from "../pages/Dashboard";
import Auth from "../pages/Auth";
import Prepare from "../pages/Prepare";
import Room from "../pages/Room";
import HistoryPage from "../pages/HistoryPage";
export default function App() {
  const { pathname } = useLocation();
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [pathname]);
  if (/^\/meetings\/[^/]+\/room\/?$/.test(pathname)) return <Room />;
  return (
    <div className="shell">
      <aside className="sidebar">
        <Link to="/" className="brand">
          <span>
            <Video size={22} />
          </span>
          comeet.
        </Link>
        <div className="sidebar-label">我的空间</div>
        <nav>
          <NavLink end to="/">
            <LayoutDashboard size={19} />
            工作台
          </NavLink>
          <NavLink to="/history">
            <History size={19} />
            会议记录
          </NavLink>
        </nav>
        <div className="sidebar-bottom">
          <span className="tag">阶段 01 / 08</span>
          <p>先把基础，做得扎实。</p>
          <Link to="/login">登录界面 ↗</Link>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <span>团队会议空间</span>
          <div>
            <span className="stage-label">{import.meta.env.MODE === "pages" ? "静态预览 · 未连接后端" : "界面预览 · 尚未登录"}</span>
            <Link to="/login">登录 / 注册</Link>
          </div>
        </header>
        <main>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/login" element={<Auth />} />
            <Route path="/register" element={<Auth register />} />
            <Route path="/meetings/:id/prepare" element={<Prepare />} />
            <Route path="/history" element={<HistoryPage />} />
            <Route path="/history/:id" element={<HistoryPage detail />} />
            <Route
              path="*"
              element={
                <>
                  <h1>页面不存在</h1>
                  <Link to="/">返回工作台</Link>
                </>
              }
            />
          </Routes>
          <footer className="page-footer">
            COMEET · 少一点打扰，多一点共识。
            <div className="preview-links">
              <Link to="/meetings/preview/prepare">准备页骨架</Link>
              <Link to="/meetings/preview/room">会议室骨架</Link>
            </div>
          </footer>
        </main>
      </div>
    </div>
  );
}
