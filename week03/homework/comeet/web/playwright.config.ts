import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "../tests/e2e",
  workers: 2,
  testIgnore: ["**/stage4-media.spec.ts"],
  reporter: "list",
  use: {
    baseURL: "http://localhost:5173",
    headless: true,
    launchOptions: process.env.CHROMIUM_PATH
      ? { executablePath: process.env.CHROMIUM_PATH, args: ["--no-sandbox"] }
      : {},
  },
  outputDir: "test-results",
});
