import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: '.', testMatch: ['knowledge-experience.spec.ts'], workers: 1, timeout: 90000,
  outputDir: process.env.KNOWLEDGE_UI_EVIDENCE_DIR || '../../test-results/knowledge-experience',
  use: { baseURL: process.env.KNOWLEDGE_UI_BASE_URL || 'http://127.0.0.1:4196', viewport: { width: 1600, height: 1000 }, headless: true,
    launchOptions: { executablePath: 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' }, screenshot: 'only-on-failure', trace: 'retain-on-failure' },
})
