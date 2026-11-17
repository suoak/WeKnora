import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: '.', testMatch: '*.spec.ts', workers: 1, timeout: 90000,
  outputDir: '../../test-results/datasource-picker', reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:4227', viewport: { width: 1280, height: 900 }, locale: 'zh-CN',
    headless: true, screenshot: 'only-on-failure', trace: 'retain-on-failure',
    launchOptions: process.platform === 'win32'
      ? { executablePath: 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' } : {},
  },
  webServer: {
    timeout: 120000,
    command: 'npm run dev -- --host 127.0.0.1 --port 4227 --strictPort',
    url: 'http://127.0.0.1:4227/tests/datasource-picker/index.html', reuseExistingServer: false,
  },
})
