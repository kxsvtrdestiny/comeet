import { useEffect, useState } from "react";
import { request } from "./api";
export function useRemote<T>(path: string | null) {
  const [data, setData] = useState<T>(),
    [loading, setLoading] = useState(!!path),
    [error, setError] = useState(""),
    [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!path) {
      setLoading(false);
      return;
    }
    const c = new AbortController();
    setLoading(true);
    setData(undefined);
    setError("");
    request<T>(path, { signal: c.signal })
      .then(setData)
      .catch((e) => {
        if (!c.signal.aborted) setError(e.message);
      })
      .finally(() => {
        if (!c.signal.aborted) setLoading(false);
      });
    return () => c.abort();
  }, [path, attempt]);
  return { data, loading, error, retry: () => setAttempt((n) => n + 1) };
}
