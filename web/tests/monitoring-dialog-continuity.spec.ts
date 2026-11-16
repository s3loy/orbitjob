import { signIn } from './support';
import { expect, test } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

test('a successful pause retains its reviewed action and version throughout dialog exit', async ({
  page,
}) => {
  const demo = demoClient();
  let check = await demo.check('1');
  demo.dispose();
  const writes: { action: string; version: number }[] = [];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/checks/1/pause') {
      writes.push({ action: 'pause', version: route.request().postDataJSON().version });
      check = { ...check, status: 'paused', version: check.version + 1 };
      return route.fulfill({
        json: { id: check.id, version: check.version, status: check.status },
      });
    }
    if (path === '/api/v1/checks/1') return route.fulfill({ json: check });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/checks/1');
  await signIn(page, 'monitoring_dialog_fixture_key');
  await expect(page.getByRole('heading', { name: check.name, exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Review version 1');
  await expect(dialog.getByText('Active', { exact: true })).toBeVisible();
  await dialog.evaluate((element) => {
    type Snapshot = { state: string | null; title: string; text: string; buttons: string[] };
    const captured: Snapshot[] = [];
    Object.assign(window, { monitoringDialogSnapshots: captured });
    const capture = () =>
      captured.push({
        state: element.getAttribute('data-state'),
        title: element.querySelector('h1, h2, h3, [role="heading"]')?.textContent || '',
        text: element.textContent || '',
        buttons: Array.from(element.querySelectorAll('button')).map(
          (button) => button.textContent || '',
        ),
      });
    new MutationObserver(capture).observe(element, {
      attributes: true,
      childList: true,
      characterData: true,
      subtree: true,
    });
    capture();
  });
  await dialog.getByRole('button', { name: 'Confirm pause', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
  await expect(dialog).toHaveCount(0);
  const closed = await page.evaluate(() => {
    const records = (
      window as unknown as {
        monitoringDialogSnapshots: {
          state: string | null;
          title: string;
          text: string;
          buttons: string[];
        }[];
      }
    ).monitoringDialogSnapshots;
    return records.filter((snapshot) => snapshot.state === 'closed');
  });
  expect(closed.length).toBeGreaterThan(0);
  for (const frame of closed) {
    expect(frame.title).toBe(`Pause ${check.name}?`);
    expect(frame.text).toMatch(/Review version 1\s*Active/);
    expect(
      frame.buttons.some((label) => label === 'Confirm null' || label === 'Confirm resume'),
    ).toBe(false);
  }
  expect(writes).toEqual([{ action: 'pause', version: 1 }]);
  await page.getByRole('button', { name: 'Resume', exact: true }).click();
  await expect(dialog.getByRole('heading')).toHaveText(`Resume ${check.name}?`);
  await expect(dialog).toContainText('Review version 2');
  await expect(dialog.getByText('Paused', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  expect(writes).toEqual([{ action: 'pause', version: 1 }]);
});
