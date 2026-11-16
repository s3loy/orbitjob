import { test, expect } from './support';

for (const width of [1440, 3200, 320, 390]) {
  for (const kind of ['login', 'setup', 'invite', 'reset'] as const) {
    test(`${kind} entry is centered in the ${width}px viewport`, async ({ page }) => {
      await page.setViewportSize({ width, height: 1000 });
      await page.route('**/api/v1/auth/session', (route) =>
        route.fulfill({
          json: {
            authenticated: false,
            initialized: kind !== 'setup',
            workspaces: [],
          },
        }),
      );
      await page.goto(
        `/console/connection${kind === 'login' ? '' : `#${kind}=mock-layout-capability`}`,
      );
      await expect(
        page.getByRole('button', {
          name:
            kind === 'login'
              ? 'Sign in'
              : kind === 'setup'
                ? 'Create administrator account'
                : kind === 'invite'
                  ? 'Accept invitation'
                  : 'Reset password',
          exact: true,
        }),
      ).toBeVisible();
      const layout = await page.evaluate(() => {
        const frame = document.querySelector('.connection-page')!.getBoundingClientRect();
        const form = document.querySelector('.connection-signin')!.getBoundingClientRect();
        return {
          frameCenter: frame.x + frame.width / 2,
          formCenter: form.x + form.width / 2,
          frameMiddle: frame.y + frame.height / 2,
          width: frame.width,
          overflow: document.documentElement.scrollWidth > innerWidth,
        };
      });
      expect(Math.abs(layout.frameCenter - width / 2)).toBeLessThanOrEqual(1);
      expect(Math.abs(layout.formCenter - width / 2)).toBeLessThanOrEqual(1);
      expect(layout.width).toBeLessThanOrEqual(520);
      expect(layout.overflow).toBe(false);
      if (width >= 1440) expect(Math.abs(layout.frameMiddle - 500)).toBeLessThanOrEqual(1);
      await expect(page.locator('.topbar, .sidebar')).toHaveCount(0);
    });
  }
}

test('an invitation pasted into the current account route is consumed without a reload', async ({
  page,
}) => {
  await page.goto('/console/connection');
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
  await page.evaluate(() => {
    location.hash = 'invite=mock-late-invitation';
  });
  await expect(
    page.getByRole('heading', { name: 'Accept workspace invitation', exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/connection$/);
  await expect(page.getByLabel('New password', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    'mock-late-invitation',
  );
});
