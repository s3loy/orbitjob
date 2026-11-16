import { describe, expect, it } from 'vitest';
import { newAccountPasswordError } from './account-validation';

describe('new account password rules', () => {
  it('counts Unicode characters rather than UTF-16 code units', () => {
    expect(newAccountPasswordError('a'.repeat(14))).toContain('15 characters');
    expect(newAccountPasswordError('a'.repeat(15))).toBeUndefined();
    expect(newAccountPasswordError('🔑'.repeat(8))).toContain('15 characters');
    expect(newAccountPasswordError('🔑'.repeat(15))).toBeUndefined();
  });

  it('matches the server UTF-8 byte ceiling for long passphrases', () => {
    expect(newAccountPasswordError('a'.repeat(1024))).toBeUndefined();
    expect(newAccountPasswordError('a'.repeat(1025))).toContain('supported length');
    expect(newAccountPasswordError('🔑'.repeat(256))).toBeUndefined();
    expect(newAccountPasswordError('🔑'.repeat(257))).toContain('supported length');
  });
});
