import { useState } from "react";
import { Link } from "react-router-dom";
import { Eye, EyeOff } from "lucide-react";
import { Disabled } from "../components/Common";
export default function Auth({ register = false }: { register?: boolean }) {
  const [show, setShow] = useState(false);
  return (
    <div className="auth-layout">
      <section className="auth-intro">
        <div className="eyebrow">MEET WITH PURPOSE</div>
        <h1>
          相聚于此，
          <br />
          让想法发生。
        </h1>
        <p>一个简单、专注的团队会议空间。</p>
        <hr />
        <p className="muted">工程骨架 · 界面方向预览</p>
        <Link to="/">返回工作台</Link>
      </section>
      <section className="card auth-card">
        <span className="tag">第 1 阶段 · 界面预览</span>
        <h2>{register ? "创建 Comeet 账号" : "欢迎回来"}</h2>
        <p className="muted">账号功能将在第 2 阶段开放，现在无需登录。</p>
        <form onSubmit={(e) => e.preventDefault()}>
          <label>
            用户名
            <input
              autoComplete="username"
              placeholder="3–32 位字母、数字或下划线"
            />
          </label>
          {register && (
            <label>
              显示名称
              <input autoComplete="nickname" placeholder="你的会议显示名称" />
            </label>
          )}
          <label>
            密码
            <div className="password">
              <input
                type={show ? "text" : "password"}
                autoComplete={register ? "new-password" : "current-password"}
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
          <Disabled>{register ? "注册" : "登录"}</Disabled>
        </form>
        <p>
          {register ? "已有账号？" : "还没有账号？"}{" "}
          <Link to={register ? "/login" : "/register"}>
            {register ? "查看登录页" : "查看注册页"}
          </Link>
        </p>
      </section>
    </div>
  );
}
