import { defineConfig } from '@playwright/test'
if(!process.env.RC24_EVIDENCE_DIR)throw new Error('Set RC24_EVIDENCE_DIR')
export default defineConfig({testDir:'.',testMatch:['save-business.spec.ts','close-cycle.spec.ts','navigation.spec.ts','editor.spec.ts','component.spec.ts','knowledge-gate.spec.ts'],
  workers:1,fullyParallel:false,timeout:120000,
  outputDir:process.env.RC24_EVIDENCE_DIR,
  reporter:[['list'],['json',{outputFile:process.env.RC24_EVIDENCE_DIR+'/results.json'}]],
  use:{baseURL:'http://127.0.0.1:4187',viewport:{width:1440,height:1000},reducedMotion:'no-preference',headless:false,
    launchOptions:{executablePath:'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'},
    screenshot:'only-on-failure',trace:'retain-on-failure',actionTimeout:60000},
  // Explicitly no Vite dev server or watcher. Static server started outside the test runner.
})
