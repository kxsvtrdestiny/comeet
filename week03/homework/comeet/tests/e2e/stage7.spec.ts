import { test, expect } from "../../web/node_modules/@playwright/test";
import { readFile } from "node:fs/promises";
test("real Go: missing DeepSeek config does not fabricate summaries; original records export and private access", async ({
  browser,
  request,
}) => {
  const sessions: any[] = [];
  for (let i = 0; i < 2; i++) {
    const username =
        "s7_" + crypto.randomUUID().replaceAll("-", "").slice(0, 10),
      password = "Stage7_test_password";
    expect(
      (
        await request.post("/api/v1/auth/register", {
          data: { username, password, displayName: "纪要检查" + i },
        })
      ).status(),
    ).toBe(201);
    const login = await request.post("/api/v1/auth/login", {
      data: { username, password },
    });
    sessions.push((await login.json()).data);
  }
  const headers = { Authorization: "Bearer " + sessions[0].token };
  const created = await request.post("/api/v1/meetings", {
    headers,
    data: { title: "第七阶段真实记录", clientRequestId: crypto.randomUUID() },
  });
  expect(created.status()).toBe(201);
  const m = (await created.json()).data;
  expect(
    (
      await request.put(`/api/v1/meetings/${m.id}/notes/me`, {
        headers,
        data: {
          content: "会议确定下周继续讨论；未指定负责人或日期。",
          version: 0,
        },
      })
    ).status(),
  ).toBe(200);
  expect(
    (
      await request.post(`/api/v1/meetings/${m.id}/end`, {
        headers,
        data: { requestId: crypto.randomUUID() },
      })
    ).status(),
  ).toBe(200);
  const context = await browser.newContext();
  await context.addInitScript(
    (s) =>
      sessionStorage.setItem(
        "comeet.auth.v1",
        JSON.stringify({ state: { session: s }, version: 0 }),
      ),
    sessions[0],
  );
  try {
    const page = await context.newPage();
    await page.goto(`/history/${m.id}`);
    const section = page.getByRole("region", { name: "DeepSeek会议纪要" });
    await expect(section).toContainText("不含语音转写");
    await expect(section).toContainText("DeepSeek未配置");
    await expect(
      section.getByRole("button", { name: "生成 AI 纪要", exact: true }),
    ).toBeDisabled();
    await expect(section.locator(".summary-result")).toHaveCount(0);
    const dl = page.waitForEvent("download");
    await section
      .getByRole("button", { name: "导出原始会议记录 Markdown" })
      .click();
    const download = await dl;
    const path = await download.path();
    const text = await readFile(path!, "utf8");
    expect(text).toContain("# 会议记录");
    expect(text).toContain("会议确定下周继续讨论");
    expect(text).toContain("不含语音转写");
    expect(text).not.toContain("# AI会议纪要");
    for (const path of ["summaries", "summaries/export"])
      expect(
        (
          await request.get(`/api/v1/meetings/${m.id}/${path}`, {
            headers: { Authorization: "Bearer " + sessions[1].token },
          })
        ).status(),
      ).toBe(403);
    expect(
      (
        await request.post(`/api/v1/meetings/${m.id}/summaries`, {
          headers,
          data: { requestId: crypto.randomUUID(), consent: true },
        })
      ).status(),
    ).toBe(503);
    for (const width of [1440, 1366, 390]) {
      await page.setViewportSize({ width, height: 900 });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
    }
  } finally {
    await context.close();
  }
});

test("UI fixture only: running, failure/manual retry, saved result, source update and escaped rendering", async ({
  page,
}) => {
  let attempts = 0,
    ticks = 0,
    currentHash = "frozen-hash",
    jobs: any[] = [];
  const result = {
    summary: "真实内容 <script>window.aiInjected=true</script>",
    topics: ["议题"],
    decisions: [{ text: "方案A", sourceIds: ["message:1"] }],
    actionItems: [
      {
        task: "继续讨论",
        owner: null,
        dueDate: null,
        sourceIds: ["note:fixture"],
      },
    ],
    openQuestions: [],
    limitations: ["不含语音转写"],
  };
  await page.route("**/api/v1/meetings/ui-fixture/summaries", async (route) => {
    if (route.request().method() === "POST") {
      attempts++;
      ticks = 0;
      const body = route.request().postDataJSON();
      expect(body.consent).toBe(true);
      jobs.unshift({
        id: body.requestId,
        status: "running",
        sourceHash: currentHash,
        model: "fixture-model",
        createdAt: Date.now(),
        finishedAt: null,
        messageCount: 1,
        noteCount: 1,
      });
      await route.fulfill({
        status: 202,
        json: {
          code: "OK",
          data: jobs[0],
          message: "success",
          requestId: "safe-fixture",
        },
      });
      return;
    }
    if (jobs[0]?.status === "running" && ++ticks >= 2) {
      jobs[0] = {
        ...jobs[0],
        status: attempts === 1 ? "failed" : "succeeded",
        errorCode: attempts === 1 ? "AI_TIMEOUT" : "",
        errorMessage: attempts === 1 ? "生成超时，请手动重试" : "",
        diagnosticId: "safe-fixture",
        finishedAt: Date.now(),
        result: attempts > 1 ? result : undefined,
      };
    }
    await route.fulfill({
      json: {
        code: "OK",
        message: "success",
        requestId: "safe-fixture",
        data: {
          configured: true,
          canGenerate: true,
          reason: "",
          model: "fixture-model",
          sourceHash: currentHash,
          maxChars: 20000,
          maxBytes: 80000,
          disclaimer: "不含语音转写",
          jobs,
        },
      },
    });
  });
  await page.goto("/login");
  await page.evaluate(async () => {
    const React = (await import("/node_modules/.vite/deps/react.js")).default;
    const { createRoot } = (
      await import("/node_modules/.vite/deps/react-dom_client.js")
    ).default;
    const { AISummaries } = await import("/src/components/AISummaries.tsx");
    const el = document.createElement("div");
    document.body.appendChild(el);
    (window as any).summaryRoot = createRoot(el);
    (window as any).summaryRoot.render(
      React.createElement(AISummaries, {
        meetingId: "ui-fixture",
        revision: 0,
      }),
    );
  });
  const section = page.getByRole("region", { name: "DeepSeek会议纪要" });
  page.once("dialog", (d) => d.accept());
  await section
    .getByRole("button", { name: "生成 AI 纪要", exact: true })
    .click();
  await expect(section).toContainText("任务状态：生成中");
  await expect(
    section.getByRole("button", { name: "正在生成纪要…" }),
  ).toBeDisabled();
  await expect(section.getByRole("alert")).toContainText("AI_TIMEOUT");
  expect(attempts).toBe(1);
  page.once("dialog", (d) => d.accept());
  await section.getByRole("button", { name: "重试生成纪要" }).click();
  await expect(section).toContainText("任务状态：已成功保存");
  await expect(section).toContainText("负责人：未明确");
  await expect(section).toContainText("截止：未明确");
  await expect(section.locator("script")).toHaveCount(0);
  expect(await page.evaluate(() => (window as any).aiInjected)).toBeUndefined();
  currentHash = "updated-hash";
  await section.getByRole("button", { name: "刷新纪要状态" }).click();
  await expect(section).toContainText("记录已更新，可重新生成");
  for (const width of [1440, 1366, 390]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await section.getByLabel("纪要任务").selectOption(jobs[1].id);
  await expect(section).toContainText("AI_TIMEOUT");
  expect(attempts).toBe(2);
  await page.evaluate(() => (window as any).summaryRoot.unmount());
});
