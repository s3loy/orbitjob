import { afterEach, expect, test, vi } from 'vitest';
import { liveClient } from './transport';
import { demoClient } from './demo';
import { checkSchema, checkDetailSchema, resultDetailSchema, sloDetailSchema } from './monitoring';
import { monitoringDemo } from './monitoring-demo';
afterEach(() => vi.unstubAllGlobals());

test('SLO writes retain exact int64 SLI ID and versioned DELETE JSON', async () => {
  const fetcher = vi
    .fn()
    .mockImplementation(async () => new Response('{"id":9007199254740993}', { status: 201 }));
  vi.stubGlobal('fetch', fetcher);
  const client = liveClient('private-memory-key');
  const receipt = await client.create('slos', {
    name: 'test',
    sli_id: '9007199254740993',
    target: 0.99,
    window_duration: '24h',
    window_type: 'rolling',
  });
  expect(receipt.id).toBe('9007199254740993');
  expect(fetcher.mock.calls[0][1].body).toContain('"sli_id":9007199254740993');
  await client.remove('slos', String(receipt.id), 7);
  expect(fetcher.mock.calls[1][0]).toBe('/api/v1/slos/9007199254740993');
  expect(fetcher.mock.calls[1][1]).toMatchObject({ method: 'DELETE', body: '{"version":7}' });
  client.dispose();
});

test('M3 demo detail contracts and absent budgets match wire schema', async () => {
  const client = demoClient();
  expect(resultDetailSchema.parse(await client.checkResult('700')).run_id).toContain('-');
  expect(sloDetailSchema.parse(await client.slo('21')).sli_id).toBe(11);
  expect(await client.budget('24')).toMatchObject({ status: 'no_data' });
  expect(await client.budget('23')).toMatchObject({ budget_total: 0, budget_remaining: -3 });
  await client.remove('slis', '11', 1);
  expect(() => client.sli('11')).toThrow('unavailable');
  expect((await client.slo('21')).sli_id).toBe(11);
  client.dispose();
});

test.each([0, 1])('check responses preserve execution policy version %i', async (version) => {
  const client = demoClient();
  const response = { ...(await client.check('1')), execution_policy_version: version };
  client.dispose();
  expect(checkSchema.parse(response).execution_policy_version).toBe(version);
  expect(checkDetailSchema.parse(response).execution_policy_version).toBe(version);
});

test.each([undefined, -1, 0.5, '1'])(
  'check responses reject a missing or invalid execution policy version %s',
  async (version) => {
    const client = demoClient();
    const response = { ...(await client.check('1')), execution_policy_version: version };
    client.dispose();
    expect(checkSchema.safeParse(response).success).toBe(false);
    expect(checkDetailSchema.safeParse(response).success).toBe(false);
  },
);

test('sample objectives follow their own probes and report consistent rolling budgets', async () => {
  const client = monitoringDemo(async (value) => value);
  for (const [objectiveID, checkID, failures] of [
    ['21', '1', 24],
    ['22', '2', 3],
  ] as const) {
    const objective = await client.slo(objectiveID);
    const indicator = await client.sli(String(objective.sli_id));
    const check = await client.check(checkID);
    expect(indicator.source_config.source_uid).toBe(`check-${check.id}`);

    const budget = await client.budget(objectiveID);
    expect(budget.budget_total).toBeCloseTo(10_000 * (1 - objective.target));
    expect(budget.budget_consumed).toBe(failures);
    expect(budget.budget_remaining).toBe(budget.budget_total - failures);
    expect(budget.burn_rate).toBeCloseTo(failures / budget.budget_total);
    expect(Date.parse(budget.window_end) - Date.parse(budget.window_start)).toBe(168 * 3_600_000);
  }
  const alert = await client.alert('31');
  const current = await client.budget(String(alert.slo_id));
  expect(Date.parse(alert.triggered_at)).toBeLessThan(Date.parse(current.window_end));
  expect(alert.burn_rate).toBeLessThan(current.burn_rate);
});

test('M3 query parameters use exact result identity and paging', async () => {
  const fetcher = vi.fn().mockResolvedValue(new Response('{"items":[]}'));
  vi.stubGlobal('fetch', fetcher);
  await liveClient('key').checkResults({
    check_id: '9007199254740993',
    offset: 20,
    status: 'failed',
  });
  const url = new URL(fetcher.mock.calls[0][0], 'https://same-origin.example');
  expect(url.searchParams.get('check_id')).toBe('9007199254740993');
  expect(url.searchParams.get('offset')).toBe('20');
  expect(url.searchParams.get('status')).toBe('failed');
});

test('Live versioned status responses use ID schema without parameter shadowing', async () => {
  const fetcher = vi
    .fn()
    .mockImplementation(async () => new Response('{"id":123,"status":"paused"}'));
  vi.stubGlobal('fetch', fetcher);
  await liveClient('key').change('checks', '123', 'pause', 9);
  expect(fetcher.mock.calls[0][1]).toMatchObject({ method: 'POST', body: '{"version":9}' });
});

test.each(['metric', 'operator', 'severity', 'threshold'])(
  'rejects a stored assertion response missing its serialized %s field',
  async (field) => {
    const client = demoClient();
    const response = await client.check('1');
    client.dispose();
    const rule: Record<string, unknown> = {
      metric: 'status',
      operator: '==',
      severity: 'warning',
      threshold: 0,
    };
    expect(checkDetailSchema.safeParse({ ...response, assertion_rules: [rule] }).success).toBe(
      true,
    );
    delete rule[field];
    const parsed = checkDetailSchema.safeParse({ ...response, assertion_rules: [rule] });
    expect(parsed.success).toBe(false);
    if (!parsed.success)
      expect(
        parsed.error.issues.some((issue) => issue.path.join('.') === `assertion_rules.0.${field}`),
      ).toBe(true);
  },
);

test('stored assertion responses retain strings without inheriting input-only enums', async () => {
  const client = demoClient();
  const response = await client.check('1');
  client.dispose();
  const rule = { metric: 'status', operator: 'legacy-op', severity: 'legacy-level', threshold: 0 };
  expect(checkDetailSchema.parse({ ...response, assertion_rules: [rule] }).assertion_rules).toEqual(
    [rule],
  );
});
