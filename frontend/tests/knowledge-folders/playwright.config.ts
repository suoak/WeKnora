import { defineConfig } from '@playwright/test'
export default defineConfig({
 testDir:'.', testMatch:'*.spec.ts',workers:1,timeout:180000,
 outputDir:'../../test-results/knowledge-folders',reporter:'list',
 use:{baseURL:'http://127.0.0.1:4238',viewport:{width:1280,height:900},headless:true,trace:'retain-on-failure',
 launchOptions:process.platform==='win32'?{executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'}:{}},
 webServer:{command:'npm run dev -- --host 127.0.0.1 --port 4238 --strictPort',url:'http://127.0.0.1:4238/tests/knowledge-folders/index.html',reuseExistingServer:false,timeout:120000},
})
