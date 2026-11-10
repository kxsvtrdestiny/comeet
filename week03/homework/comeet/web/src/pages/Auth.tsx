import { useState, type FormEvent } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { Eye, EyeOff } from "lucide-react";
import { post } from "../services/api";
import { staticPreview, type Session, type User } from "../services/types";
import { useAuth } from "../stores/auth";
export default function Auth({ register = false }: { register?: boolean }) {
  const [show, setShow] = useState(false),
    [username, setUsername] = useState(""),
    [name, setName] = useState(""),
    [password, setPassword] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const session = useAuth((s) => s.session),
    notice = useAuth((s) => s.notice);
  const nav = useNavigate(),
    location = useLocation();
  const value = location.state?.from;
  const from =
    typeof value === "string" &&
    value.startsWith("/") &&
    !value.startsWith("//") &&
    !value.startsWith("/login") &&
    !value.startsWith("/register")
      ? value
      : "/";
  if (!staticPreview && session) return <Navigate to={from} replace />;
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (staticPreview || busy) return;
    setError("");
    if (register && new TextEncoder().encode(password).length > 72) {
      setError("密码的 UTF-8 长度不能超过 72 字节");
      return;
    }
    setBusy(true);
    try {
      if (register) {
        await post<User>("/auth/register", {
          username,
          displayName: name,
          password,
        });
        nav("/login", { replace: true, state: { from, registered: true } });
      } else {
        const s = await post<Session>("/auth/login", { username, password });
        useAuth.getState().setSession(s);
        nav(from, { replace: true });
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="auth-layout">
      <Link to="/">返回工作台</Link>
      <section className="card auth-card">
        <h1>{register ? "注册" : "登录"}</h1>
        {staticPreview && (
          <p className="muted">
            静态预览未连接后端，请在 Windows 本机运行后注册和登录。
          </p>
        )}
        {!register && (notice || location.state?.registered) && (
          <p role="status">{notice || "注册成功，请使用新账号登录"}</p>
        )}
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <form onSubmit={submit}>
          <label>
            用户名
            <input
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              minLength={register ? 3 : undefined}
              maxLength={32}
              pattern={register ? "[A-Za-z0-9_]{3,32}" : undefined}
              placeholder="3–32 位字母、数字或下划线"
            />
          </label>
          {register && (
            <label>
              显示名称
              <input
                autoComplete="nickname"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                maxLength={32}
                placeholder="你的会议显示名称"
              />
            </label>
          )}
          <label>
            密码
            <div className="password">
              <input
                type={show ? "text" : "password"}
                autoComplete={register ? "new-password" : "current-password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={register ? 8 : undefined}
                placeholder="至少 8 个字符"
              />
              <button
                type="button"
                aria-label={show ? "隐藏密码" : "显示密码"}
                title={show ? "隐藏密码" : "显示密码"}
                onClick={() => setShow(!show)}
              >
                {show ? <EyeOff size={18} /> : <Eye size={18} />}
              </button>
            </div>
          </label>
          <button className="button" disabled={busy || staticPreview}>
            {busy ? "提交中…" : register ? "注册" : "登录"}
          </button>
        </form>
        <p>
          {register ? "已有账号？" : "还没有账号？"}{" "}
          <Link to={register ? "/login" : "/register"} state={{ from }}>
            {register ? "查看登录页" : "查看注册页"}
          </Link>
        </p>
      </section>
    </div>
  );
}
