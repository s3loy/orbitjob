import { expect, test } from 'vitest';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { BudgetView } from './monitoring-ui';
test('Fractional allowances remain nonzero in the budget presentation', () => {
  const html = renderToStaticMarkup(
    createElement(BudgetView, {
      budget: {
        budget_total: 0.0001,
        budget_consumed: 1,
        budget_remaining: -0.9999,
        burn_rate: 10000,
        status: 'exhausted',
        window_start: '2026-10-19T00:00:00Z',
        window_end: '2026-10-19T01:00:00Z',
      },
    }),
  );
  expect(html).toContain('0.0001');
  expect(html).toContain('-0.9999');
  expect(html).toContain('0.9999 error events beyond');
});
