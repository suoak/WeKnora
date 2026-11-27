import { defineConfig } from '@playwright/test'
import { fileURLToPath } from 'node:url'
export default defineConfig({
  testDir: '.', testMatch: '*.spec.ts', workers: 1, timeout: 90000,
  outputDir: '../../test-results/portal-refresh', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:4231', viewport: { width: 1440, height: 1000 }, locale: 'zh-CN',
    headless: true, trace: 'retain-on-failure', screenshot: 'only-on-failure',
  },
  webServer: { cwd: fileURLToPath(new URL('../..', import.meta.url)), command: 'npx vite --host 127.0.0.1 --port 4231 --strictPort',
    url: 'http://127.0.0.1:4231', reuseExistingServer: false, timeout: 120000 },
})
