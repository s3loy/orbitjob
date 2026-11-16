import { useId, useState } from 'react';
import { AlertDialog, Button, Flex } from '@radix-ui/themes';
import {
  runtimePreset,
  runtimePresetDraft,
  runtimePresetReplacesContent,
  sameRuntimePresetDraft,
  selectedRuntimePreset,
  type RuntimePresetDraft,
  type RuntimePresetId,
} from './runtime-presets';
import './runtime-presets.css';

type Props = RuntimePresetDraft & {
  disabled?: boolean;
  onChange(image: string, command: string[], args: string[]): void;
};

export function RuntimePresetField({ image, command, args, disabled = false, onChange }: Props) {
  const id = useId();
  const value = { image, command, args };
  const [custom, setCustom] = useState(false);
  const [candidate, setCandidate] = useState<{
    id: RuntimePresetId;
    previous: RuntimePresetDraft;
  }>();
  const selected = custom ? 'custom' : selectedRuntimePreset(value);
  const pending =
    candidate && sameRuntimePresetDraft(candidate.previous, value) ? candidate : undefined;
  if (candidate && !pending) setCandidate(undefined);

  function apply(presetId: RuntimePresetId) {
    if (disabled) return;
    const next = runtimePresetDraft(presetId);
    setCustom(false);
    setCandidate(undefined);
    onChange(next.image, next.command, next.args);
  }

  function choose(next: RuntimePresetId | 'custom') {
    if (disabled || next === selected) return;
    if (next === 'custom') {
      setCustom(true);
      return;
    }
    if (runtimePresetReplacesContent(value, next)) {
      setCandidate({ id: next, previous: { image, command: [...command], args: [...args] } });
      return;
    }
    apply(next);
  }

  return (
    <div className="runtime-preset-field">
      <label className="form-field" htmlFor={`${id}-runtime`}>
        Runtime
        <select
          id={`${id}-runtime`}
          aria-label="Runtime"
          aria-describedby={`${id}-help`}
          value={selected}
          disabled={disabled}
          onChange={(event) => choose(event.target.value as RuntimePresetId | 'custom')}
        >
          <option value="shell">Shell</option>
          <option value="python">Python</option>
          <option value="node">Node.js</option>
          <option value="custom">Custom image</option>
        </select>
      </label>
      <p id={`${id}-help`} className="runtime-preset-hint">
        {selected === 'custom' ? (
          'Choose an image that includes your executable and dependencies, then set its execution command.'
        ) : (
          <>
            Runtime image: <code>{runtimePreset(selected).image}</code>. Edit the code below.
          </>
        )}
      </p>
      <AlertDialog.Root
        open={Boolean(pending) && !disabled}
        onOpenChange={(open) => {
          if (!open) setCandidate(undefined);
        }}
      >
        <AlertDialog.Content maxWidth="460px">
          <AlertDialog.Title>Replace the runtime and code?</AlertDialog.Title>
          <AlertDialog.Description>
            {pending ? runtimePreset(pending.id).label : 'The selected runtime'} will replace the
            current image, command and arguments with a Hello example. Review the code before
            saving.
          </AlertDialog.Description>
          <Flex justify="end" gap="3" mt="4" wrap="wrap">
            <AlertDialog.Cancel>
              <Button type="button" variant="soft" color="gray">
                Keep current code
              </Button>
            </AlertDialog.Cancel>
            <AlertDialog.Action>
              <Button
                type="button"
                disabled={disabled || !pending}
                onClick={() => {
                  if (pending) apply(pending.id);
                }}
              >
                Replace runtime and code
              </Button>
            </AlertDialog.Action>
          </Flex>
        </AlertDialog.Content>
      </AlertDialog.Root>
    </div>
  );
}
