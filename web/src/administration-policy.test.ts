import { readFileSync } from 'node:fs';
import { expect, test } from 'vitest';
import {
  guidedPolicy,
  formatPolicyDocument,
  parsePolicyDocument,
  policyDocument,
  policyTemplates,
} from './administration-policy';

test('operator template uses37 implemented actions, exact types and own workspace only', () => {
  const document = parsePolicyDocument(policyDocument());
  const actions = document.statement[0].action;
  const registry = readFileSync(
    new URL('../../internal/core/domain/policy/actions.go', import.meta.url),
    'utf8',
  );
  const known = new Set([...registry.matchAll(/"([a-z]+:[A-Za-z]+)"/g)].map((match) => match[1]));
  expect(actions).toHaveLength(37);
  expect(actions.every((action) => known.has(action))).toBe(true);
  expect(actions.some((action) => /^(apikey|tenant|policy|group):|:Delete$/.test(action))).toBe(
    false,
  );
  expect(
    document.statement[0].resource.every((resource) =>
      /^orbitjob:self:\*:[a-z]+\/\*$/.test(resource),
    ),
  ).toBe(true);
  expect(guidedPolicy(policyDocument())?.actions).toEqual(actions);
});

test('advanced Deny and multiple supported statements are retained without conversion', () => {
  const document = {
    version: '1',
    statement: [
      {
        effect: 'Deny',
        action: ['job:Trigger'],
        resource: ['orbitjob:self:production:job/restricted'],
      },
      { effect: 'Allow', action: ['job:Get'], resource: ['orbitjob:self:*:job/*'] },
    ],
  };
  const value = JSON.stringify(document);
  expect(parsePolicyDocument(value)).toEqual(document);
  expect(guidedPolicy(value)).toBeUndefined();
  expect(parsePolicyDocument(JSON.stringify(parsePolicyDocument(value), null, 2))).toEqual(
    document,
  );
});

test('unsupported semantics cannot be formatted or submitted as supported policy', () => {
  const document = {
    version: '1',
    statement: [
      {
        effect: 'Allow',
        action: ['job:Get'],
        resource: ['orbitjob:self:*:job/*'],
        Condition: { network: 'private' },
      },
    ],
  };
  const value = JSON.stringify(document);
  expect(() => formatPolicyDocument(value)).toThrow(
    'Unsupported policy field statement[0].Condition',
  );
  expect(() => parsePolicyDocument(value)).toThrow(
    'Unsupported policy field statement[0].Condition',
  );
  expect(guidedPolicy(value)).toBeUndefined();
  expect(() => parsePolicyDocument('{"Version":"1","statement":[]}')).toThrow('document.Version');
});

test('formatting and creation reject duplicate fields even with equal or escaped values', () => {
  for (const value of [
    '{"version":"1","version":"1","statement":[]}',
    '{"version":"1","statement":[{"effect":"Deny","effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}',
    '{"version":"1","\\u0076ersion":"1","statement":[]}',
  ]) {
    expect(() => formatPolicyDocument(value)).toThrow('Duplicate policy field');
    expect(() => parsePolicyDocument(value)).toThrow('Duplicate policy field');
  }
});

test('guided resource scopes preserve exact group and ID, read-only excludes writes', () => {
  const value = policyDocument(policyTemplates.readonly.actions, {
    group: 'platform-group',
    type: 'job',
    id: 'daily-task',
  });
  expect(guidedPolicy(value)?.scope).toEqual({
    group: 'platform-group',
    type: 'job',
    id: 'daily-task',
  });
  expect(parsePolicyDocument(value).statement[0].resource).toEqual([
    'orbitjob:self:platform-group:job/daily-task',
  ]);
  expect(
    policyTemplates.readonly.actions.every((action) =>
      /:(Get|List|ReadLogs|GetBudget|ListBudgets)$/.test(action),
    ),
  ).toBe(true);
});

test('invalid scope, action wildcard and an empty task cannot be submitted', () => {
  const document = JSON.parse(policyDocument());
  document.statement[0].resource = ['orbitjob:foreign:*:job/*'];
  expect(() => parsePolicyDocument(JSON.stringify(document))).toThrow('this workspace');
  document.statement[0].resource = ['orbitjob:self:*:job/*'];
  document.statement[0].action = ['*'];
  expect(() => parsePolicyDocument(JSON.stringify(document))).toThrow('explicit');
  expect(() => parsePolicyDocument(policyDocument([]))).toThrow('needs actions');
});
