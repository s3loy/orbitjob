import { expect, it } from 'vitest';
import { defaultLogAttempt } from './attempt-choice';
it('uses trusted last attempt after a recovered invocation, retaining earlier attempts for selection', () => {
  const attempts = [
    { attempt_number: 1, phase: 'Failed' },
    { attempt_number: 2, phase: 'Succeeded' },
  ];
  expect(defaultLogAttempt(attempts, 2)).toBe('2');
  expect(defaultLogAttempt(attempts.reverse(), null)).toBe('2');
  expect(defaultLogAttempt([{ attempt_number: 4, phase: 'Succeeded' }], null)).toBe('4');
});
it('distinguishes a confirmed empty attempt list from unavailable metadata without guessing attempt one', () => {
  expect(defaultLogAttempt([], null)).toBe('');
  expect(defaultLogAttempt(null, null)).toBe('');
  expect(defaultLogAttempt(undefined, undefined)).toBe('');
  expect(defaultLogAttempt(null, 2)).toBe('2');
  expect(defaultLogAttempt(null, -1)).toBe('');
});
