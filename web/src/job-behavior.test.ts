import { describe, expect, it } from 'vitest';
import { jobBehavior } from './job-behavior';

describe('Job authoring behavior', () => {
  it('preserves literal argument boundaries and whitespace without inventing a shell', () => {
    const command = ['/usr/bin/report', '--label=two words', '$(TOKEN)', ''];
    const args = ['one two', 'line 1\nline 2', '9007199254740993'];
    const behavior = jobBehavior(
      JSON.stringify({
        jobTemplate: { image: 'registry.example/report:v1', command, args },
      }),
      'create',
    );
    expect(behavior.command).toEqual({ state: 'explicit', values: command });
    expect(behavior.args).toEqual({ state: 'explicit', values: args });
    expect(behavior.image).toBe('registry.example/report:v1');
  });

  it('keeps image defaults unknown instead of synthesizing an executable or arguments', () => {
    const behavior = jobBehavior(
      '{"jobTemplate":{"image":"example:v1","command":[],"args":[]}}',
      'create',
    );
    expect(behavior.command).toEqual({ state: 'inherited', values: [] });
    expect(behavior.args).toEqual({ state: 'inherited', values: [] });
    expect(jobBehavior('{}', 'create').image).toBeUndefined();
    expect(
      jobBehavior('{"jobTemplate":{"command":"echo hello","args":[1]}}', 'create').command.state,
    ).toBe('invalid');
    expect(
      jobBehavior('{"jobTemplate":{"command":"echo hello","args":[1]}}', 'create').args.state,
    ).toBe('invalid');
  });

  it('uses one attempt by default and treats an explicit zero as one with a correction', () => {
    expect(jobBehavior('{}', 'create').attempts).toContain(
      'One platform attempt, including the first',
    );
    const zero = jobBehavior('{"retryPolicy":{"maxAttempts":0}}', 'edit');
    expect(zero.attempts).toContain('One platform attempt');
    expect(zero.issues.some((issue) => issue.message.includes('at least 1'))).toBe(true);
  });

  it('distinguishes per-attempt deadlines, platform attempts and Pod backoff', () => {
    const behavior = jobBehavior(
      `{
      "timeoutSeconds":120,"retryPolicy":{"maxAttempts":3},
      "executionPolicyVersion":1,"retrySafe":false,
      "jobTemplate":{"image":"worker:v1","backoffLimit":2}
    }`,
      'create',
    );
    expect(behavior.deadline).toContain('Each attempt has a 120-second Kubernetes Job deadline');
    expect(behavior.deadlineDetail).toContain('not a total Run deadline');
    expect(behavior.attempts).toContain('3 platform attempts, including the first');
    expect(behavior.backoff).toContain('within one attempt, separately');
    expect(behavior.repeatWarning).toBe(true);
    expect(behavior.repeat).toContain('requires this task to be marked safe to repeat');
    expect(jobBehavior('{"timeoutSeconds":0}', 'create').deadlineDetail).toBeUndefined();
  });

  it('does not turn malformed or out-of-range numeric drafts into valid defaults', () => {
    for (const value of ['"120"', 'null', '1.5', '-1', '9223372036854775807']) {
      const behavior = jobBehavior(
        `{"timeoutSeconds":${value},"retryPolicy":{"maxAttempts":${value}}}`,
        'create',
      );
      expect(behavior.issues.some((issue) => issue.message.includes('timeout'))).toBe(true);
      expect(behavior.issues.some((issue) => issue.message.includes('maximum attempts'))).toBe(
        true,
      );
      expect(behavior.deadline).not.toContain('9223372036854776000');
      expect(behavior.repeat).toContain('Correct the retry settings');
    }
    expect(jobBehavior('{"retryPolicy":false}', 'create').attempts).toContain('Set a whole-number');
  });

  it('preserves exact configured quantity strings without guessing missing resources', () => {
    const behavior = jobBehavior(
      `{
      "executionPolicyVersion":1,
      "jobTemplate":{"resources":{"requests":{"cpu":"250m","memory":"9007199254740993"}}}
    }`,
      'create',
    );
    expect(behavior.resources).toEqual([
      { name: 'cpu', group: 'requests', value: '250m' },
      { name: 'memory', group: 'requests', value: '9007199254740993' },
    ]);
    expect(behavior.resourceNote).toContain('installation policy defaults');
    expect(behavior.resourceNote).toContain('unknown here');
    expect(jobBehavior('{}', 'create').resources).toEqual([]);
    expect(jobBehavior('{}', 'create').resourceNote).toContain('unknown here');
  });

  it('keeps repeat behavior unresolved when a safe-to-repeat v1 draft has invalid limits', () => {
    const behavior = jobBehavior(
      `{
      "executionPolicyVersion":1,"retrySafe":true,
      "retryPolicy":{"maxAttempts":"3"},"jobTemplate":{"backoffLimit":2}
    }`,
      'create',
    );
    expect(behavior.repeatWarning).toBe(true);
    expect(behavior.repeat).toContain('Correct the retry settings');
    expect(behavior.repeat).not.toContain('Marked safe to repeat');
  });

  it('uses shared Forbid admission and explains trigger-specific Replace behavior', () => {
    const forbid = jobBehavior('{"concurrencyPolicy":"Forbid"}', 'create').overlap.join(' ');
    expect(forbid).toContain('Scheduled, manual and workflow-task');
    expect(forbid).toContain('Scheduled starts are skipped');
    expect(forbid).toContain('Accepted manual starts wait');
    const replace = jobBehavior('{"concurrencyPolicy":"Replace"}', 'edit').overlap.join(' ');
    expect(replace).toContain('scheduled start requests cancellation');
    expect(replace).toContain('waits for them to finish');
    expect(replace).toContain('Manual and workflow-task starts leave running work in place');
  });

  it('explains save effects and missed starts without calculating upcoming schedule times', () => {
    const manual = jobBehavior('{}', 'create');
    expect(manual.save).toContain('Saving does not request a run');
    expect(manual.misfire).toBeUndefined();
    const scheduled = jobBehavior('{"schedule":"0 2 * * *","misfirePolicy":"Skip"}', 'edit');
    expect(scheduled.save).toContain('Once the saved version is active');
    expect(scheduled.schedule).toBe('0 2 * * *');
    expect(scheduled.misfire).toContain('Skip missed schedule times');
    expect(jobBehavior('{"suspend":true}', 'edit').save).toContain(
      'Automatic and manual starts stay suspended',
    );
    expect(jobBehavior('{"schedule":"* * * * *","suspend":"false"}', 'edit').trigger).toContain(
      'incomplete',
    );
  });

  it('does not explain unreadable JSON or non-object drafts as executable configuration', () => {
    for (const spec of ['{"jobTemplate":', '[]', 'null', '9007199254740993']) {
      const behavior = jobBehavior(spec, 'create');
      expect(behavior.readable).toBe(false);
      expect(behavior.save).toBe('');
      expect(behavior.issues[0].section).toBe('general');
    }
  });
});
