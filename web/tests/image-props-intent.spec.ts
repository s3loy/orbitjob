import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page) {
  const demo = demoClient();
  const [job, fn] = await Promise.all([demo.job('17'), demo.functionDefinition('27')]);
  demo.dispose();
  let releaseImage!: () => void, releaseSave!: () => void;
  const imageHeld = new Promise<void>((resolve) => {
    releaseImage = resolve;
  });
  const saveHeld = new Promise<void>((resolve) => {
    releaseSave = resolve;
  });
  const state = { fn };
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
    if (path === '/api/v1/functions/27') {
      if (request.method() === 'GET') return route.fulfill({ json: state.fn });
      await saveHeld;
      return route.fulfill({
        status: 409,
        json: { error: { code: 'CONFLICT', message: 'Fixture version conflict' } },
      });
    }
    if (path === '/api/v1/jobs/17') {
      await imageHeld;
      return route.fulfill({ json: job });
    }
    return route.fulfill({ json: { items: path === '/api/v1/jobs' ? [job] : [] } });
  });
  await page.goto('/console/functions/27/edit');
  await signIn(page, 'image_props_fixture_not_a_real_key');
  await expect(page.getByLabel('Container image', { exact: true })).toHaveValue(fn.image);
  await page.getByRole('button', { name: 'Browse images', exact: true }).click();
  const requested = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/v1/jobs/17',
  );
  await page.getByRole('button', { name: `Reuse image from ${job.name}`, exact: true }).click();
  await requested;
  return { state, releaseImage, releaseSave };
}

test('reloading the parent definition wins over an older image-reuse response', async ({
  page,
}) => {
  const fixtureState = await fixture(page);
  try {
    fixtureState.state.fn = { ...fixtureState.state.fn, image: 'registry.example/reloaded:v3' };
    await page
      .getByRole('button', { name: 'Discard edits and reload current version', exact: true })
      .click();
    await expect(page.getByLabel('Container image', { exact: true })).toHaveValue(
      'registry.example/reloaded:v3',
    );
    const response = page.waitForResponse(
      (result) => new URL(result.url()).pathname === '/api/v1/jobs/17',
    );
    fixtureState.releaseImage();
    await response;
    await expect(page.getByLabel('Container image', { exact: true })).toHaveValue(
      'registry.example/reloaded:v3',
    );
    await expect(page.getByText('Image copied from', { exact: false })).toHaveCount(0);
  } finally {
    fixtureState.releaseImage();
    fixtureState.releaseSave();
  }
});

test('a pending save locks out an earlier image selection', async ({ page }) => {
  const fixtureState = await fixture(page);
  try {
    const image = fixtureState.state.fn.image;
    await page.getByRole('button', { name: 'Review function', exact: true }).click();
    const write = page.waitForRequest(
      (request) =>
        new URL(request.url()).pathname === '/api/v1/functions/27' && request.method() !== 'GET',
    );
    await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
    expect((await write).postDataJSON()).toMatchObject({ image });
    const review = page.getByRole('region', { name: 'Review Function save' });
    await expect(review.getByRole('button', { name: 'Keep editing', exact: true })).toBeDisabled();
    const response = page.waitForResponse(
      (result) => new URL(result.url()).pathname === '/api/v1/jobs/17',
    );
    fixtureState.releaseImage();
    await response;
    await expect(review.getByText(image, { exact: true })).toBeVisible();
    await expect(page.getByText('Image copied from', { exact: false })).toHaveCount(0);
    fixtureState.releaseSave();
    await expect(page.getByRole('alert').first()).toContainText('Fixture version conflict');
    await review.getByRole('button', { name: 'Keep editing', exact: true }).click();
    await expect(page.getByLabel('Container image', { exact: true })).toHaveValue(image);
  } finally {
    fixtureState.releaseImage();
    fixtureState.releaseSave();
  }
});
