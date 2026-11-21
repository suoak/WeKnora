import { defineConfig } from '@playwright/test'

if (!process.env.PORTAL_REAL_RUNTIME || !process.env.PORTAL_REAL_EVIDENCE) throw new Error('Set private runtime and evidence paths')

export default defineConfig({
  testDir: '.', testMatch: 'real.spec.ts', workers: 1,
  timeout: 240000, expect: { timeout: 30000 },
  outputDir: process.env.PORTAL_REAL_EVIDENCE,
  use: { headless: true, trace: 'off', video: 'off', screenshot: 'off', actionTimeout: 30000 },
})
