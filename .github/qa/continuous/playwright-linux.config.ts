import { defineConfig } from '@playwright/test'
if (!process.env.CONTINUOUS_E2E_DIR || !process.env.CONTINUOUS_JUNIT) throw new Error('Isolated evidence directory required')
export default defineConfig({
  testDir: '.',
  testMatch: ['save-business.spec.ts', 'close-cycle.spec.ts', 'navigation.spec.ts', 'editor.spec.ts', 'component.spec.ts', 'knowledge-gate.spec.ts', 'mcp-scope.spec.ts'],
  workers: 1, fullyParallel: false, timeout: 120000,
  outputDir: process.env.CONTINUOUS_E2E_DIR,
  reporter: [['junit', { outputFile: process.env.CONTINUOUS_JUNIT }]],
  use: {
    baseURL: 'http://127.0.0.1:4187', viewport: { width: 1440, height: 1000 },
    reducedMotion: 'no-preference', headless: true,
    screenshot: 'off', trace: 'off', actionTimeout: 60000,
  },
})
