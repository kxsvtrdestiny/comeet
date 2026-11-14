"""Linux process regression; local HTTP fixture, NOT a real DeepSeek test.
Usage: python3 tests/tools/summary_process_probe.py /absolute/path/to/server-binary
Uses temporary database and random loopback ports; never reads server/.env.
"""
import json
import os
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import urllib.request
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


mode = {"slow": True}
release = threading.Event()


class Provider(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", "0")))
        if mode["slow"]:
            release.wait(20)
        result = {"summary": "真实测试笔记概述", "topics": [], "decisions": [],
                  "actionItems": [], "openQuestions": [], "limitations": ["不含语音转写"]}
        body = json.dumps({"choices": [{"finish_reason": "stop", "message": {
            "content": json.dumps(result, ensure_ascii=False)}}]}).encode()
        try:
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass


provider = ThreadingHTTPServer(("127.0.0.1", 0), Provider)
threading.Thread(target=provider.serve_forever, daemon=True).start()
port = free_port()
base = f"http://127.0.0.1:{port}/api/v1"
server_process = None
with tempfile.TemporaryDirectory(prefix="comeet-summary-probe-") as tmp:
    settings = dict(os.environ, APP_ENV="development", HTTP_ADDR=f"127.0.0.1:{port}",
                    PUBLIC_ORIGIN="http://localhost:5173", ALLOWED_ORIGINS="http://localhost:5173",
                    DATABASE_PATH=str(Path(tmp) / "probe.db"),
                    JWT_SECRET="process-probe-only-secret-00000000000000",
                    DEEPSEEK_ENABLED="true", DEEPSEEK_API_KEY="local-fixture-key",
                    DEEPSEEK_BASE_URL=f"http://127.0.0.1:{provider.server_port}",
                    DEEPSEEK_MODEL="fixture-model", DEEPSEEK_TIMEOUT_SECONDS="10",
                    LIVEKIT_API_KEY="", LIVEKIT_API_SECRET="")
    log = open(Path(tmp) / "server.log", "wb")
    token = ""

    def call(method, path, data=None, authorized=True):
        headers = {"Content-Type": "application/json"}
        if token and authorized:
            headers["Authorization"] = "Bearer " + token
        req = urllib.request.Request(base + path, method=method, headers=headers,
                                     data=json.dumps(data).encode() if data is not None else None)
        with urllib.request.urlopen(req, timeout=4) as res:
            reply = json.load(res)
            assert reply["code"] == "OK", reply["code"]
            return reply["data"]

    def start():
        global server_process
        server_process = subprocess.Popen([sys.argv[1]], env=settings, stdout=log, stderr=log)
        for _ in range(100):
            try:
                health = call("GET", "/health/ready", authorized=False)
                assert health["stage"] == 7
                return
            except Exception:
                if server_process.poll() is not None:
                    raise RuntimeError("server exited; inspect local probe log")
                time.sleep(.05)
        raise RuntimeError("server did not start")

    def stop(force=False):
        started = time.monotonic()
        server_process.send_signal(signal.SIGKILL if force else signal.SIGTERM)
        server_process.wait(timeout=8)
        return round(time.monotonic() - started, 3)

    def meeting():
        mid = call("POST", "/meetings", {"title": "进程纪要检查", "clientRequestId": str(uuid.uuid4())})["id"]
        call("PUT", f"/meetings/{mid}/notes/me", {"content": "本场真实手动笔记", "version": 0})
        call("POST", f"/meetings/{mid}/end", {"requestId": str(uuid.uuid4())})
        return mid

    def generate(mid):
        return call("POST", f"/meetings/{mid}/summaries", {"requestId": str(uuid.uuid4()), "consent": True})

    def terminal(mid):
        for _ in range(80):
            job = call("GET", f"/meetings/{mid}/summaries")["jobs"][0]
            if job["status"] != "running":
                return job
            time.sleep(.05)
        raise RuntimeError("task stayed running")

    try:
        start()
        username = "probe_" + uuid.uuid4().hex[:12]
        password = "process-test-password"
        call("POST", "/auth/register", {"username": username, "password": password, "displayName": "进程测试"}, False)
        token = call("POST", "/auth/login", {"username": username, "password": password}, False)["token"]
        mid = meeting()
        assert generate(mid)["status"] == "running"
        elapsed = stop()
        start()
        assert terminal(mid)["errorCode"] == "SERVER_RESTART"
        mode["slow"] = False
        release.set()
        generate(mid)
        success = terminal(mid)
        assert success["status"] == "succeeded"
        stop()
        start()
        assert terminal(mid)["id"] == success["id"]
        exported = call("GET", f"/meetings/{mid}/summaries/export?jobId={success['id']}")
        assert "不含语音转写" in exported["markdown"] and "本场真实手动笔记" in exported["markdown"]
        second = meeting()
        mode["slow"] = True
        release.clear()
        generate(second)
        stop(True)
        start()
        assert terminal(second)["errorCode"] == "SERVER_RESTART"
        mode["slow"] = False
        release.set()
        generate(second)
        assert terminal(second)["status"] == "succeeded"
        stop()
        print(json.dumps({"provider": "local fixture, not DeepSeek", "sigterm_exit_seconds": elapsed,
                          "sigterm_interruption_retry": "passed", "sigkill_interruption_retry": "passed",
                          "saved_result_original_login_export_after_restart": "passed"}))
    finally:
        release.set()
        if server_process and server_process.poll() is None:
            server_process.kill()
            server_process.wait(timeout=8)
        log.close()
        provider.shutdown()
