import { describe, expect, it } from 'vitest';
import { mergeDefinition, mergeResolved } from './definition-merge';

describe('three-way definition merge', () => {
  it('combines disjoint edits and preserves exact integers', () => {
    const merged = mergeDefinition(
      '{"timeout":10,"image":"a","count":9007199254740993}',
      '{"timeout":20,"image":"a","count":9007199254740993}',
      '{"image":"b","timeout":10,"count":9007199254740993}',
    );
    expect(merged.conflicts).toEqual([]);
    expect(merged.spec).toContain('9007199254740993');
    expect(merged.spec).toContain('"timeout": 20');
    expect(merged.spec).toContain('"image": "b"');
    expect(mergeResolved(merged)).toBe(true);
  });
  it('requires an explicit choice for conflicting edits and arrays', () => {
    const original = '{"image":"a","args":["one","two"]}';
    const local = '{"image":"b","args":["changed","two"]}';
    const live = '{"image":"c","args":["one","other"]}';
    const merged = mergeDefinition(original, local, live);
    expect(merged.conflicts.map((c) => c.path)).toEqual(['/image', '/args']);
    expect(mergeResolved(merged)).toBe(false);
    const chosen = mergeDefinition(original, local, live, { '/image': 'local', '/args': 'live' });
    expect(mergeResolved(chosen)).toBe(true);
    expect(JSON.parse(chosen.spec)).toEqual({ image: 'b', args: ['one', 'other'] });
  });
  it('distinguishes a removed field from null, and rejects duplicate keys', () => {
    const merged = mergeDefinition('{"value":1}', '{}', '{"value":null}');
    expect(merged.conflicts[0]).toMatchObject({
      path: '/value',
      local: 'Field removed',
      live: 'null',
    });
    expect(() => mergeDefinition('{}', '{"image":"a","image":"b"}', '{}')).toThrow(/Duplicate/);
  });
});
