import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { checkDraft, initialDraft, manifestYAML } from './manifests';

describe('CRD manifest export', () => {
  it('preserves literal argument boundaries without shell interpolation', () => {
    const d = initialDraft('ScheduledJob');
    d.args = ['', 'space between', 'a "quote"', 'line1\nline2', '$HOME', '9007199254740993'];
    expect(checkDraft(d)).toEqual([]);
    expect(parse(manifestYAML(d)).spec.jobTemplate.args).toEqual(d.args);
  });
  it('keeps signed int64 rule operands exact and rejects overflow', () => {
    const d = initialDraft('WorkflowJob');
    d.tasks[1].rules[0] = {
      task: 'extract',
      field: 'duration_ms',
      operator: 'ge',
      values: [],
      number: '9223372036854775807',
    };
    expect(checkDraft(d)).toEqual([]);
    expect(
      parse(manifestYAML(d), { intAsBigInt: true }).spec.tasks[1].condition.rules[0].number,
    ).toBe(9223372036854775807n);
    d.tasks[1].rules[0].number = '9223372036854775808';
    expect(checkDraft(d).some((e) => e.message.includes('64-bit'))).toBe(true);
  });
  it('rejects cyclic or undeclared task dependencies and bounded CRD integers', () => {
    const d = initialDraft('WorkflowJob');
    d.tasks[0].dependencies = ['publish'];
    expect(checkDraft(d).some((e) => e.message.includes('cycle'))).toBe(true);
    d.tasks[0].dependencies = ['missing'];
    expect(checkDraft(d).some((e) => e.message.includes('Invalid dependency'))).toBe(true);
    d.timeout = '2147483648';
    expect(checkDraft(d).some((e) => e.path.includes('timeout'))).toBe(true);
  });
});
