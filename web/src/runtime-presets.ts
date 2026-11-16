import { executionMethod } from './execution-command';

export type RuntimePresetId = 'shell' | 'python' | 'node';
export type RuntimePresetDraft = { image: string; command: string[]; args: string[] };
export type RuntimePreset = {
  id: RuntimePresetId;
  label: string;
  image: string;
  command: readonly string[];
  args: readonly string[];
};

const presets: Record<RuntimePresetId, RuntimePreset> = {
  shell: {
    id: 'shell',
    label: 'Shell',
    image: 'busybox:1.37.0',
    command: ['sh', '-c'],
    args: ['echo "Hello from OrbitJob"'],
  },
  python: {
    id: 'python',
    label: 'Python',
    image: 'python:3.14-slim',
    command: ['python', '-c'],
    args: ['print("Hello from OrbitJob")'],
  },
  node: {
    id: 'node',
    label: 'Node.js',
    image: 'node:24-bookworm-slim',
    command: ['node', '-e'],
    args: ['console.log("Hello from OrbitJob");'],
  },
};

export function runtimePreset(id: RuntimePresetId): RuntimePreset {
  return presets[id];
}

export function runtimePresetDraft(id: RuntimePresetId = 'shell'): RuntimePresetDraft {
  const preset = runtimePreset(id);
  return { image: preset.image, command: [...preset.command], args: [...preset.args] };
}

export function selectedRuntimePreset(value: RuntimePresetDraft): RuntimePresetId | 'custom' {
  const method = executionMethod(value);
  if (value.image === presets.shell.image && method === 'shell') return 'shell';
  if (value.image === presets.python.image && method === 'python') return 'python';
  if (value.image === presets.node.image && method === 'javascript') return 'node';
  return 'custom';
}

export function sameRuntimePresetDraft(
  left: RuntimePresetDraft,
  right: RuntimePresetDraft,
): boolean {
  return (
    left.image === right.image &&
    left.command.length === right.command.length &&
    left.command.every((token, index) => token === right.command[index]) &&
    left.args.length === right.args.length &&
    left.args.every((token, index) => token === right.args[index])
  );
}

export function runtimePresetReplacesContent(
  value: RuntimePresetDraft,
  id: RuntimePresetId,
): boolean {
  return (
    Boolean(value.image.length || value.command.length || value.args.length) &&
    !Object.values(presets).some((preset) =>
      sameRuntimePresetDraft(value, runtimePresetDraft(preset.id)),
    ) &&
    !sameRuntimePresetDraft(value, runtimePresetDraft(id))
  );
}
