import { defineConfig, devices } from '@playwright/test';
import { readFileSync, lstatSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';

if (!process.env.ORBITJOB_CONSOLE_CREDENTIALS)
  throw new Error(
    'Run python3 scripts/console-installed.py prepare, then use its private credentials.json.',
  );
const credentialsPath = resolve(process.env.ORBITJOB_CONSOLE_CREDENTIALS);
const credentialFile = lstatSync(credentialsPath);
if (!credentialFile.isFile() || (credentialFile.mode & 0o077) !== 0)
  throw new Error('Installed credentials must be a private regular file (0600).');
const credentials = JSON.parse(readFileSync(credentialsPath, 'utf8'));
if (
  credentials.context !== 'kind-orbitjob-console-e2e' ||
  credentials.namespace !== 'orbitjob' ||
  credentials.prepared !== true ||
  credentials.retired === true
)
  throw new Error(
    'Installed fixtures are incomplete or retired; prepare a fresh dedicated installation.',
  );
if (
  !credentials.username ||
  !credentials.password ||
  !credentials.userId ||
  !credentials.accountsByKey
)
  throw new Error(
    'Installed account profiles are missing; prepare fresh fixtures with the current installer.',
  );
if (!process.env.KUBECONFIG || process.env.KUBECONFIG.includes(':'))
  throw new Error('KUBECONFIG must select one explicit disposable kind kubeconfig.');
const baseURL = process.env.ORBITJOB_CONSOLE_URL ?? 'http://127.0.0.1:18081';
const origin = new URL(baseURL);
if (origin.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(origin.hostname))
  throw new Error('Installed Console verification requires a loopback HTTP origin.');
const outputDir =
  process.env.ORBITJOB_CONSOLE_REPORT_DIR ?? join(dirname(credentialsPath), 'playwright-results');
export default defineConfig({
  testDir: './installed-tests',
  outputDir,
  workers: 1,
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  reporter: [['list'], ['json', { outputFile: join(outputDir, 'report.json') }]],
  timeout: 120_000,
  expect: { timeout: 90_000 },
  use: {
    baseURL,
    ...devices['Desktop Chrome'],
    viewport: { width: 1180, height: 920 },
    timezoneId: 'Asia/Shanghai',
    trace: 'off',
    video: 'off',
    screenshot: 'off',
  },
});
