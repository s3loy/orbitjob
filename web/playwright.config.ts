import { defineConfig, devices } from '@playwright/test';

const fixturePort = Number(process.env.ORBITJOB_BROWSER_FIXTURE_PORT || 4173);
const installedPort = Number(
  new URL(process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174').port,
);

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  timeout: 35_000,
  use: { baseURL: `http://127.0.0.1:${fixturePort}`, trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: `npm run dev -- --mode fixtures --port ${fixturePort} --strictPort`,
      url: `http://127.0.0.1:${fixturePort}/console/`,
      reuseExistingServer: !process.env.CI,
    },
    {
      command: `npm run dev -- --port ${installedPort} --strictPort`,
      url: `http://127.0.0.1:${installedPort}/console/`,
      reuseExistingServer: !process.env.CI,
    },
  ],
});
