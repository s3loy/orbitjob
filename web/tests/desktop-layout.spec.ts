import { expect, test, type Page } from './support';

async function openDemo(page: Page, path = '/jobs') {
  await page.goto(`/console${path}`);
  await page.getByRole('button', { name: 'Explore sample data', exact: true }).click();
  await expect(page.locator('.topbar, .breadcrumbs')).toHaveCount(0);
}

async function fitsViewport(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
}

for (const viewport of [
  { width: 1710, height: 1000 },
  { width: 1440, height: 900 },
]) {
  test.describe(`Desktop frame at ${viewport.width}px`, () => {
    test.use({ viewport, deviceScaleFactor: 2 });

    test('aligns content and keeps access navigation available on long pages', async ({ page }) => {
      await openDemo(page);
      await expect(page.locator('.jobs-table')).toBeVisible();
      const content = await page.locator('.jobs-table').boundingBox();
      const title = await page.getByRole('heading', { name: 'Jobs', exact: true }).boundingBox();
      expect(content).not.toBeNull();
      expect(title).not.toBeNull();
      expect(Math.abs(content!.x - title!.x)).toBeLessThanOrEqual(1);
      await expect(page.getByLabel('Current workspace')).toHaveText('Commerce Operations');
      await expect(page.locator('.connection-state')).toHaveText('Sample data');
      await expect(page.locator('.sidebar')).not.toContainText('127.0.0.1');
      await fitsViewport(page);

      await page
        .getByRole('navigation', { name: 'Main navigation' })
        .getByRole('link', { name: 'Runs', exact: true })
        .click();
      await expect(page.locator('.runs-table tbody tr').first()).toBeVisible();
      const administration = page.locator('.sidebar-bottom').getByRole('link', {
        name: 'Administration',
        exact: true,
      });
      await expect(administration).toBeInViewport();
      await page.mouse.wheel(0, 650);
      await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(300);
      await expect(administration).toBeInViewport();
      await expect(
        page
          .getByRole('navigation', { name: 'Main navigation' })
          .getByRole('link', { name: 'Jobs', exact: true }),
      ).toBeInViewport();
      await administration.click();
      await expect(
        page.getByRole('heading', { name: 'Administration', exact: true }),
      ).toBeVisible();
      await fitsViewport(page);
    });
  });
}

test.describe('Compact desktop panel', () => {
  test.use({ viewport: { width: 626, height: 881 }, deviceScaleFactor: 2 });

  test('keeps table actions readable and access management reachable', async ({ page }) => {
    await openDemo(page);
    const firstRow = page.locator('.jobs-table tbody tr').first();
    const trigger = firstRow.getByRole('button', { name: 'Trigger billing-reconciliation' });
    await expect(trigger).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Schedule', exact: true })).toBeHidden();
    expect(
      await trigger.evaluate((button) => {
        const control = button.getBoundingClientRect();
        const cell = button.closest('td')!.getBoundingClientRect();
        return (
          button.scrollWidth <= button.clientWidth &&
          control.left >= cell.left - 1 &&
          control.right <= cell.right + 1
        );
      }),
    ).toBe(true);
    await fitsViewport(page);
    const administration = page.locator('.sidebar-bottom').getByRole('link', {
      name: 'Administration',
      exact: true,
    });
    await expect(administration).toBeVisible();
    await expect(page.getByRole('link', { name: 'Administration', exact: true })).toHaveCount(1);
    await expect(page.getByRole('link', { name: 'Workspace', exact: true })).toHaveCount(1);
    await administration.click();
    await expect(page.getByRole('heading', { name: 'Administration', exact: true })).toBeVisible();
    await fitsViewport(page);
  });
});

test.describe('Direct authoring entry', () => {
  test.use({ viewport: { width: 1710, height: 1000 }, deviceScaleFactor: 2 });

  test('uses the same bounded desktop form before visiting any collection', async ({ page }) => {
    await openDemo(page, '/functions/new');
    const name = page.getByRole('textbox', { name: 'Function name', exact: true });
    const image = page.getByRole('textbox', { name: 'Container image', exact: true });
    await expect(name).toBeVisible();
    await expect(image).toBeVisible();
    const nameBox = await name.boundingBox();
    const imageBox = await image.boundingBox();
    expect(nameBox).not.toBeNull();
    expect(imageBox).not.toBeNull();
    expect(nameBox!.y + nameBox!.height).toBeLessThan(imageBox!.y);
    expect(Math.abs(nameBox!.x - imageBox!.x)).toBeLessThanOrEqual(1);
    await expect(image).toBeInViewport();
    const formWidth = await page
      .locator('main > form')
      .evaluate((form) => form.getBoundingClientRect().width);
    expect(formWidth).toBeLessThanOrEqual(900);
    expect(formWidth).toBeGreaterThan(800);
    await fitsViewport(page);
  });
});

test.describe('Touch and dark layout', () => {
  test.use({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    colorScheme: 'dark',
    deviceScaleFactor: 2,
  });

  test('keeps a usable Trigger target and its confirmation in view', async ({ page }) => {
    await openDemo(page);
    const connection = page.locator('.sidebar-bottom').getByRole('link', {
      name: 'Workspace',
      exact: true,
    });
    await expect(connection).toBeVisible();
    await expect(page.getByRole('link', { name: 'Administration', exact: true })).toHaveCount(1);
    await expect(page.getByRole('link', { name: 'Workspace', exact: true })).toHaveCount(1);
    const trigger = page.getByRole('button', { name: 'Trigger billing-reconciliation' });
    await expect(trigger).toBeVisible();
    expect((await trigger.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    await fitsViewport(page);
    await trigger.click();
    const confirm = page.getByRole('dialog').getByRole('button', { name: 'Confirm trigger' });
    await expect(confirm).toBeInViewport();
    await expect(page.getByRole('dialog')).toContainText('billing-reconciliation');
    await page.getByRole('button', { name: 'Keep reviewing', exact: true }).click();
    await expect(trigger).toBeFocused();
  });
});
