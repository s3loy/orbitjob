import { parse, stringify, LosslessNumber } from 'lossless-json';

const missing = Symbol('missing');
type Value = unknown;
type Choice = 'local' | 'live';
export type FieldConflict = {
  path: string;
  base: string;
  local: string;
  live: string;
};
export type DefinitionMerge = {
  base: string;
  local: string;
  live: string;
  choices: Record<string, Choice>;
  conflicts: FieldConflict[];
  spec: string;
};

function read(input: string): unknown {
  return parse(input, undefined, {
    parseNumber(raw) {
      if (!/^-?\d+$/.test(raw)) throw new Error('Configuration numbers must be integers.');
      return BigInt(raw);
    },
    onDuplicateKey({ key }) {
      throw new Error(`Duplicate configuration field ${key}.`);
    },
  });
}
function object(value: Value): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
function equal(a: Value, b: Value): boolean {
  if (a === b) return true;
  if (Array.isArray(a) && Array.isArray(b))
    return a.length === b.length && a.every((v, i) => equal(v, b[i]));
  if (object(a) && object(b)) {
    const keys = Object.keys(a);
    return (
      keys.length === Object.keys(b).length &&
      keys.every((key) => Object.hasOwn(b, key) && equal(a[key], b[key]))
    );
  }
  return false;
}
function encode(value: Value): string {
  if (value === missing) return 'Field removed';
  return (
    stringify(
      value,
      (_key, v) => (typeof v === 'bigint' ? new LosslessNumber(v.toString()) : v),
      2,
    ) ?? 'null'
  );
}

/** Merge disjoint edits; arrays are a single field until their identity is defined. */
export function mergeDefinition(
  base: string,
  local: string,
  live: string,
  choices: Record<string, Choice> = {},
): DefinitionMerge {
  const conflicts: FieldConflict[] = [];
  function merge(a: Value, b: Value, c: Value, path: string): Value {
    if (equal(b, c)) return b;
    if (equal(b, a)) return c;
    if (equal(c, a)) return b;
    if ((object(a) || a === missing) && object(b) && object(c)) {
      const before = object(a) ? a : (Object.create(null) as Record<string, unknown>);
      const result: Record<string, unknown> = Object.create(null);
      const keys = new Set([...Object.keys(before), ...Object.keys(b), ...Object.keys(c)]);
      for (const key of keys) {
        const value = merge(
          Object.hasOwn(before, key) ? before[key] : missing,
          Object.hasOwn(b, key) ? b[key] : missing,
          Object.hasOwn(c, key) ? c[key] : missing,
          `${path}/${key.replaceAll('~', '~0').replaceAll('/', '~1')}`,
        );
        if (value !== missing) result[key] = value;
      }
      return result;
    }
    conflicts.push({ path: path || '/', base: encode(a), local: encode(b), live: encode(c) });
    return choices[path || '/'] === 'live' ? c : b;
  }
  const value = merge(read(base), read(local), read(live), '');
  return { base, local, live, choices, conflicts, spec: encode(value) };
}

export function mergeResolved(merge: DefinitionMerge) {
  return merge.conflicts.every((field) => Boolean(merge.choices[field.path]));
}
