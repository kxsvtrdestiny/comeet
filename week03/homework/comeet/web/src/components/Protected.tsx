import { useEffect, useState, type ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { request } from "../services/api";
import { staticPreview, type User } from "../services/types";
import { useAuth } from "../stores/auth";
export function SessionCheck() {
  const session = useAuth((s) => s.session);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (staticPreview || !session) return;
    if (
      !Number.isFinite(session.expiresAt) ||
      session.expiresAt <= Date.now()
    ) {
      useAuth.getState().clear("登录已过期，请重新登录");
      return;
    }
    const controller = new AbortController();
    const expire = setTimeout(
      () => useAuth.getState().clear("登录已过期，请重新登录"),
      Math.min(session.expiresAt - Date.now(), 2147483647),
    );
    request<User>("/me", { signal: controller.signal })
      .then(() => {
        if (useAuth.getState().session?.token === session.token)
          useAuth.getState().verify();
      })
      .catch((e) => {
        if (
          e.name !== "AbortError" &&
          useAuth.getState().session?.token === session.token
        )
          useAuth.getState().verify(e.message);
      });
    return () => {
      controller.abort();
      clearTimeout(expire);
    };
  }, [session, retry]);
  const error = useAuth((s) => s.verificationError);
  return error ? (
    <div className="session-error" role="alert">
      {error} <button onClick={() => setRetry((n) => n + 1)}>重试连接</button>
    </div>
  ) : null;
}
export default function Protected({ children }: { children: ReactNode }) {
  const session = useAuth((s) => s.session);
  const verified = useAuth((s) => s.verified);
  const error = useAuth((s) => s.verificationError);
  const location = useLocation();
  if (staticPreview) return <>{children}</>;
  if (!session)
    return (
      <Navigate
        to="/login"
        replace
        state={{ from: location.pathname + location.search }}
      />
    );
  if (!verified)
    return (
      <section className="card">
        <p>
          {error ? "暂时无法验证登录状态，请重试连接。" : "正在验证登录状态…"}
        </p>
      </section>
    );
  return <>{children}</>;
}
