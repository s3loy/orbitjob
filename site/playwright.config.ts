import { defineConfig, devices } from '@playwright/test';

const origin = 'http://127.0.0.1:18433';
const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  workers: 2,
  timeout: 40_000,
  expect: { timeout: 10_000 },
  outputDir: './test-results',
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: `${origin}${base}/`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run preview -- --port 18433 --ignore-lock',
    url: `${origin}${base}/`,
    reuseExistingServer: !process.env.CI,
  },
});
