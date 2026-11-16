import { signIn } from './support';
import { expect, test, fixtureIdentity } from './support';
import { demoClient } from '../src/api/demo';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function openJobForm(page: import('@playwright/test').Page) {
  await page.goto('/console/manifests/job');
  await signIn(page, 'otj_image_catalog_fixture');
  await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
}

test('image browser pages authorized workload names and reads only a selected Job image', async ({
  page,
}) => {
  const fixture = demoClient();
  const sample = (await fixture.jobs())[0];
  const detail = await fixture.job(sample.id);
  fixture.dispose();
  const detailReads: string[] = [];
  const external: string[] = [];
  page.on('request', (request) => {
    if (!new URL(request.url()).hostname.match(/^(127\.0\.0\.1|localhost)$/))
      external.push(request.url());
  });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity')
      return route.fulfill({ json: { ...fixtureIdentity, workload_namespaces: ['team-ops'] } });
    if (url.pathname === '/api/v1/jobs') {
      const second = url.searchParams.get('offset') === '20';
      return route.fulfill({
        json: {
          items: Array.from({ length: second ? 1 : 20 }, (_, index) => ({
            ...sample,
            id: index + (second ? 21 : 1),
            source_kind: 'ScheduledJob',
            namespace: 'team-ops',
            name: `catalog-job-${index + (second ? 21 : 1)}`,
          })),
        },
      });
    }
    if (url.pathname === '/api/v1/jobs/21') {
      detailReads.push(url.pathname);
      return route.fulfill({
        json: {
          ...detail,
          id: 21,
          name: 'catalog-job-21',
          job_template: {
            ...detail.job_template,
            image: 'private.example:5000/team/export@sha256:abcdef012345',
          },
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await openJobForm(page);
  expect(detailReads).toEqual([]);
  await page.getByRole('button', { name: 'Browse images' }).click();
  await expect(
    page.getByRole('button', { name: 'Reuse image from catalog-job-1', exact: true }),
  ).toBeVisible();
  expect(detailReads).toEqual([]);
  await page.getByRole('button', { name: 'Next sources' }).click();
  await page.getByRole('button', { name: 'Reuse image from catalog-job-21', exact: true }).click();
  await expect(page.getByLabel('Container image', { exact: true })).toHaveValue(
    'private.example:5000/team/export@sha256:abcdef012345',
  );
  expect(detailReads).toEqual(['/api/v1/jobs/21']);
  await expect(page.getByLabel('Public image search')).toHaveValue('');
  await expect(page.getByRole('link', { name: 'Search Docker Hub' })).toHaveCount(0);
  await page.getByLabel('Public image search').fill('python & alpine');
  await expect(page.getByRole('link', { name: 'Search Docker Hub' })).toHaveAttribute(
    'href',
    'https://hub.docker.com/search?q=python%20%26%20alpine',
  );
  expect(external).toEqual([]);
});

test('denied Job sources keep manual entry and independent Function reuse available', async ({
  page,
}) => {
  const fixture = demoClient();
  const fn = (await fixture.functions())[0];
  fixture.dispose();
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity')
      return route.fulfill({ json: { ...fixtureIdentity, workload_namespaces: ['team-ops'] } });
    if (url.pathname === '/api/v1/jobs')
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Denied' } },
      });
    if (url.pathname === '/api/v1/functions')
      return route.fulfill({
        json: {
          items: [{ ...fn, name: 'existing-export', image: 'registry.internal/export:release-42' }],
        },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await openJobForm(page);
  await page.getByRole('button', { name: 'Browse images' }).click();
  await expect(page.getByText('Your key cannot read this source.', { exact: false })).toBeVisible();
  await page.getByLabel('Container image', { exact: true }).fill('private.example/custom:v3');
  await page.getByRole('button', { name: 'Functions' }).click();
  await page.getByRole('button', { name: 'Reuse image from existing-export' }).click();
  await expect(page.getByLabel('Container image', { exact: true })).toHaveValue(
    'registry.internal/export:release-42',
  );
});

test('AI assistant supplies a working bundled download and manual copy recovery', async ({
  page,
}) => {
  await page.addInitScript(() => {
    document.execCommand = () => false;
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: () => Promise.reject(new Error('Clipboard blocked')) },
      configurable: true,
    });
  });
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity')
      return route.fulfill({ json: { ...fixtureIdentity, workload_namespaces: ['team-ops'] } });
    return route.fulfill({ json: { items: [] } });
  });
  await openJobForm(page);
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByText('YAML resources', { exact: true }).click();
  await page.getByLabel('AI workload request').fill('Run our existing export image on demand.');
  await page.getByRole('button', { name: 'Copy AI brief' }).click();
  const assistant = page.getByRole('region', { name: 'AI YAML assistant' });
  await expect(
    assistant.getByText('Copy is unavailable in this browser.', { exact: false }),
  ).toBeVisible();
  await expect(assistant.getByLabel('AI brief', { exact: true })).toBeVisible();
  await expect(assistant.getByLabel('AI brief', { exact: true })).toHaveValue(
    /Run our existing export image on demand/,
  );
  const download = page.waitForEvent('download');
  await assistant.getByRole('link', { name: 'Download skill and references' }).click();
  expect((await download).suggestedFilename()).toBe('orbitjob-workloads.zip');
  const response = await page.request.get(
    `${process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174'}/console/ai/orbitjob-workloads/orbitjob-workloads.zip`,
  );
  expect(response.status()).toBe(200);
  const bytes = await response.body();
  expect(bytes.subarray(0, 4).toString('hex')).toBe('504b0304');
  expect(bytes.includes(Buffer.from('orbitjob-workloads/references/scheduledjob-crd.yaml'))).toBe(
    true,
  );
  expect(bytes.includes(Buffer.from('orbitjob-workloads/references/openapi.yaml'))).toBe(true);
});
