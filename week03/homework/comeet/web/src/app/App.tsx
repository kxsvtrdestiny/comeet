import { useEffect, useState } from "react";
import {
  Link,
  NavLink,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import {
  Video,
  LayoutDashboard,
  History,
  Settings as SettingsIcon,
} from "lucide-react";
import Dashboard from "../pages/Dashboard";
import Auth from "../pages/Auth";
import Prepare from "../pages/Prepare";
import Room from "../pages/Room";
import HistoryPage from "../pages/HistoryPage";
import MeetingDetail from "../pages/MeetingDetail";
import Settings from "../pages/Settings";
import Protected, { SessionCheck } from "../components/Protected";
import { useAuth } from "../stores/auth";
import { post } from "../services/api";
import { staticPreview } from "../services/types";
export default function App() {
  const { pathname } = useLocation();
  const nav = useNavigate();
  const session = useAuth((s) => s.session);
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [pathname]);
  async function logout() {
    setBusy(true);
    setError("");
    try {
      await post("/auth/logout");
      useAuth.getState().clear("已退出登录");
      nav("/login", { replace: true });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  const protectedPage = (page: React.ReactNode) => (
    <Protected>{page}</Protected>
  );
  return (
    <>
      <SessionCheck />
      {/^\/meetings\/[^/]+\/room\/?$/.test(pathname) ? (
        <Routes>
          <Route path="/meetings/:id/room" element={protectedPage(<Room />)} />
        </Routes>
      ) : (
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
              <NavLink to="/settings">
                <SettingsIcon size={19} />
                设置
              </NavLink>
            </nav>
            <div className="sidebar-bottom">
              <span className="tag">阶段 06 / 08</span>
            </div>
          </aside>
          <div className="workspace">
            <header className="topbar">
              <span>团队会议空间</span>
              <div>
                {staticPreview ? (
                  <>
                    <span>静态预览 · 未连接后端</span>
                    <Link to="/login">登录 / 注册</Link>
                  </>
                ) : session ? (
                  <>
                    <span>{session.user.displayName}</span>
                    <button
                      className="text-button"
                      onClick={logout}
                      disabled={busy}
                    >
                      {busy ? "退出中…" : "退出登录"}
                    </button>
                  </>
                ) : (
                  <Link to="/login">登录 / 注册</Link>
                )}
              </div>
            </header>
            <main>
              {error && (
                <p role="alert" className="error">
                  {error}
                </p>
              )}
              <Routes>
                <Route path="/" element={protectedPage(<Dashboard />)} />
                <Route path="/settings" element={protectedPage(<Settings />)} />
                <Route path="/login" element={<Auth />} />
                <Route path="/register" element={<Auth register />} />
                <Route
                  path="/meetings/:id/prepare"
                  element={protectedPage(<Prepare />)}
                />
                <Route
                  path="/meetings/:id"
                  element={protectedPage(<MeetingDetail />)}
                />
                <Route
                  path="/history"
                  element={protectedPage(<HistoryPage />)}
                />
                <Route
                  path="/history/:id"
                  element={protectedPage(<HistoryPage detail />)}
                />
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
            </main>
          </div>
        </div>
      )}
    </>
  );
}
