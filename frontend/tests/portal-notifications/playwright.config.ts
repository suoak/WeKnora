import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: '.',
  testMatch: 'inbox.spec.ts',
  workers: 1,
  timeout: 120000,
  expect: { timeout: 30000 },
  outputDir: '../../test-results/portal-notifications',
  use: { baseURL: 'http://127.0.0.1:5198', headless: true, actionTimeout: 30000, trace: 'retain-on-failure' },
  webServer: { command: 'npm run preview -- --host 127.0.0.1 --port 5198', url: 'http://127.0.0.1:5198', reuseExistingServer: false },
})
