import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: '.', testMatch: '*.spec.ts', workers: 1, timeout: 180000,
  outputDir: '../../test-results/mcp-access', reporter: 'list',
  use: {
    baseURL: process.env.MCP_BASE_URL || 'http://127.0.0.1:4219', viewport: { width: 1440, height: 1000 },
    launchOptions: { executablePath: process.env.MCP_BROWSER_PATH || (process.platform === 'win32' ? 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' : undefined) },
    screenshot: 'only-on-failure', trace: 'retain-on-failure',
  },
  webServer: process.env.MCP_BASE_URL ? undefined : { command: 'npm run dev -- --host 127.0.0.1 --port 4219 --strictPort', url: 'http://127.0.0.1:4219', reuseExistingServer: false },
})
