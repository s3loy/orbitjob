import { afterEach, describe, expect, it, vi } from 'vitest';
import { randomUUID } from './uuid';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('UUID v4 browser compatibility', () => {
  it('uses the native generator with its Crypto receiver when available', () => {
    const source = {
      randomUUID: vi.fn(function (this: unknown) {
        expect(this).toBe(source);
        return 'aab7cb6b-88dd-4be3-8a7e-c7ab6d529148';
      }),
      getRandomValues: vi.fn(),
    };
    vi.stubGlobal('crypto', source);
    expect(randomUUID()).toBe('aab7cb6b-88dd-4be3-8a7e-c7ab6d529148');
    expect(source.randomUUID).toHaveBeenCalledExactlyOnceWith();
    expect(source.getRandomValues).not.toHaveBeenCalled();
  });

  it.each([
    [0x00, '00000000-0000-4000-8000-000000000000'],
    [0x40, '40404040-4040-4040-8040-404040404040'],
    [0x80, '80808080-8080-4080-8080-808080808080'],
    [0xff, 'ffffffff-ffff-4fff-bfff-ffffffffffff'],
  ])('preserves random bytes and sets the v4 version and variant for %i', (byte, expected) => {
    const source = {
      getRandomValues: vi.fn(function (this: unknown, bytes: Uint8Array) {
        expect(this).toBe(source);
        expect(bytes).toBeInstanceOf(Uint8Array);
        expect(bytes).toHaveLength(16);
        return bytes.fill(byte);
      }),
    };
    vi.stubGlobal('crypto', source);
    vi.spyOn(Math, 'random').mockImplementation(() => {
      throw new Error('An insecure random source must never be used.');
    });
    expect(randomUUID()).toBe(expected);
    expect(source.getRandomValues).toHaveBeenCalledOnce();
  });

  it('requests fresh cryptographic bytes for each UUID', () => {
    const source = {
      getRandomValues: vi
        .fn<(bytes: Uint8Array) => Uint8Array>()
        .mockImplementationOnce((bytes) => bytes.map((_, index) => index))
        .mockImplementationOnce((bytes) => bytes.map((_, index) => index + 16)),
    };
    vi.stubGlobal('crypto', source);
    expect(randomUUID()).toBe('00010203-0405-4607-8809-0a0b0c0d0e0f');
    expect(randomUUID()).toBe('10111213-1415-4617-9819-1a1b1c1d1e1f');
    expect(source.getRandomValues).toHaveBeenCalledTimes(2);
  });

  it('reports unavailable cryptography instead of generating a weak identifier', () => {
    vi.stubGlobal('crypto', undefined);
    expect(randomUUID).toThrow('Secure random number generation is unavailable');
  });
});
