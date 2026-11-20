import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: '.', testMatch: '*.spec.ts', workers: 1, timeout: 45000,
  outputDir: '../../test-results/portal-assets', reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:4239', headless: true, locale: 'zh-CN',
    viewport: { width: 1360, height: 900 } },
  webServer: { command: 'npm run dev -- --host 127.0.0.1 --port 4239 --strictPort',
    url: 'http://127.0.0.1:4239/tests/portal-assets/index.html', reuseExistingServer: false, timeout: 90000 },
})
