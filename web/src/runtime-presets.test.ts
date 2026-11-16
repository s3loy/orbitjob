import { describe, expect, it } from 'vitest';
import { executionCommandInputError, executionMethod } from './execution-command';
import {
  runtimePreset,
  runtimePresetDraft,
  runtimePresetReplacesContent,
  sameRuntimePresetDraft,
  selectedRuntimePreset,
  type RuntimePresetId,
} from './runtime-presets';

describe('runtime presets', () => {
  it.each([
    ['shell', 'busybox:1.37.0', 'shell'],
    ['python', 'python:3.14-slim', 'python'],
    ['node', 'node:24-bookworm-slim', 'javascript'],
  ] as const)('seeds a valid editable %s script with its runtime image', (id, image, method) => {
    const value = runtimePresetDraft(id);
    expect(value.image).toBe(image);
    expect(value.image).toBe(runtimePreset(id).image);
    expect(selectedRuntimePreset(value)).toBe(id);
    expect(executionMethod(value)).toBe(method);
    expect(executionCommandInputError(undefined, value)).toBe('');
  });

  it('defaults new drafts to Shell and returns independent execution arrays', () => {
    const first = runtimePresetDraft();
    const second = runtimePresetDraft('shell');
    expect(first).toEqual(second);
    first.command[0] = 'changed';
    first.args.push('extra');
    expect(second.command).toEqual(['sh', '-c']);
    expect(runtimePresetDraft('shell')).toEqual(second);
  });

  it.each(['shell', 'python', 'node'] as const)(
    'keeps %s selected while its code and positional arguments change',
    (id) => {
      const value = runtimePresetDraft(id);
      value.args = ['custom code\nsecond line', '', 'two words'];
      expect(selectedRuntimePreset(value)).toBe(id);
    },
  );

  it('shows Custom image for an unknown image or an unsupported execution override', () => {
    const value = runtimePresetDraft('python');
    const custom = { ...value, image: 'registry.example.com/team/python:custom' };
    const before = structuredClone(custom);
    expect(selectedRuntimePreset(custom)).toBe('custom');
    expect(custom).toEqual(before);
    expect(selectedRuntimePreset({ ...value, command: [], args: [] })).toBe('custom');
    expect(selectedRuntimePreset({ ...value, command: ['python', '-u', '-c'] })).toBe('custom');
    expect(selectedRuntimePreset({ ...value, command: ['sh', '-c'] })).toBe('custom');
  });

  it('requires replacement confirmation for existing image or execution content', () => {
    const value = { image: 'custom:1', command: ['runner', ''], args: ['two words', ''] };
    const before = structuredClone(value);
    for (const id of ['shell', 'python', 'node'] as RuntimePresetId[])
      expect(runtimePresetReplacesContent(value, id)).toBe(true);
    expect(value).toEqual(before);
    expect(runtimePresetReplacesContent({ image: '', command: [], args: [''] }, 'shell')).toBe(
      true,
    );
    expect(
      runtimePresetReplacesContent({ image: 'custom:1', command: [], args: [] }, 'shell'),
    ).toBe(true);
  });

  it('avoids replacement for an empty draft or the exact preset already applied', () => {
    expect(runtimePresetReplacesContent({ image: '', command: [], args: [] }, 'shell')).toBe(false);
    const original = runtimePresetDraft('python');
    expect(runtimePresetReplacesContent(original, 'python')).toBe(false);
    expect(
      runtimePresetReplacesContent({ ...original, args: ['print("changed")'] }, 'python'),
    ).toBe(true);
    expect(runtimePresetReplacesContent(original, 'node')).toBe(false);
  });

  it('detects stale replacement candidates by exact image and token placement', () => {
    const value = { image: 'custom:1', command: ['runner', 'flag'], args: ['', 'two words'] };
    expect(sameRuntimePresetDraft(value, structuredClone(value))).toBe(true);
    expect(sameRuntimePresetDraft(value, { ...value, image: 'custom:2' })).toBe(false);
    expect(
      sameRuntimePresetDraft(value, {
        ...value,
        command: ['runner'],
        args: ['flag', '', 'two words'],
      }),
    ).toBe(false);
    expect(sameRuntimePresetDraft(value, { ...value, args: ['two words'] })).toBe(false);
  });
});
