import { useId, useRef, useState } from 'react';
import { Button, Checkbox, Text, TextField } from '@radix-ui/themes';
import { Check, Search } from 'lucide-react';
import type { Policy } from './api/administration';
import { policySummary } from './administration-policy';

export type NamedOption = { id: string; name: string; description?: string };
export function NamedPicker({
  label,
  value,
  options,
  onChange,
  disabled,
  optional = false,
  loading = false,
  unavailable = false,
  onRetry,
  emptyLabel = 'No restriction',
}: {
  label: string;
  value: string;
  options: NamedOption[];
  onChange(value: string): void;
  disabled?: boolean;
  optional?: boolean;
  loading?: boolean;
  unavailable?: boolean;
  onRetry?(): void;
  emptyLabel?: string;
}) {
  const [search, setSearch] = useState('');
  const [open, setOpen] = useState(false);
  const menuID = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  function choose(id: string) {
    onChange(id);
    setOpen(false);
    trigger.current?.focus();
  }
  const selected = options.find((option) => option.id === value);
  const visible = options.filter((option) =>
    `${option.name} ${option.description ?? ''}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <div className="admin-picker">
      <Text as="p" size="2" weight="medium">
        {label}
      </Text>
      <Button
        type="button"
        variant="outline"
        color="gray"
        className="admin-picker-trigger"
        disabled={disabled}
        aria-expanded={open}
        aria-label={label}
        aria-controls={open ? menuID : undefined}
        ref={trigger}
        onClick={() => setOpen(!open)}
      >
        {selected?.name ||
          (value
            ? `Selected ${value.slice(0, 8)}…`
            : optional
              ? emptyLabel
              : `Choose ${label.toLowerCase()}`)}
      </Button>
      {open && (
        <div
          className="admin-picker-menu"
          id={menuID}
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.preventDefault();
              event.stopPropagation();
              setOpen(false);
              trigger.current?.focus();
            }
          }}
        >
          <TextField.Root
            autoFocus
            aria-label={`Search ${label.toLowerCase()}`}
            value={search}
            placeholder="Search by name…"
            onChange={(event) => setSearch(event.target.value)}
          >
            <TextField.Slot>
              <Search size={14} />
            </TextField.Slot>
          </TextField.Root>
          <div className="admin-option-list" role="group" aria-label={`${label} options`}>
            {optional && (
              <button type="button" disabled={disabled} onClick={() => choose('')}>
                {emptyLabel}
              </button>
            )}
            {visible.map((option) => (
              <button
                type="button"
                key={option.id}
                aria-pressed={option.id === value}
                disabled={disabled}
                onClick={() => choose(option.id)}
              >
                <span>
                  <strong>{option.name}</strong>
                  {option.description && <small>{option.description}</small>}
                </span>
                {option.id === value && <Check size={14} />}
              </button>
            ))}
            {!visible.length && (
              <Text as="p" size="1" color="gray">
                {loading
                  ? 'Loading options…'
                  : unavailable
                    ? 'Names could not be loaded.'
                    : options.length
                      ? 'No matching options. Search another name.'
                      : 'No options available in this workspace.'}
              </Text>
            )}
          </div>
        </div>
      )}
      {loading && (
        <Text as="p" size="1" role="status">
          Loading {label.toLowerCase()}…
        </Text>
      )}
      {unavailable && onRetry && (
        <Button type="button" variant="outline" size="1" onClick={onRetry} disabled={loading}>
          Retry {label.toLowerCase()}
        </Button>
      )}
    </div>
  );
}

export function PolicyPicker({
  policies,
  selected,
  onChange,
  unavailable,
  knownNames = {},
  disabled,
  onCreate,
  loading = false,
  onRetry,
  canCreate = true,
}: {
  policies: Policy[];
  selected: string[];
  onChange(value: string[]): void;
  unavailable: boolean;
  knownNames?: Record<string, string>;
  disabled?: boolean;
  onCreate(): void;
  loading?: boolean;
  onRetry?(): void;
  canCreate?: boolean;
}) {
  const [search, setSearch] = useState('');
  const [manual, setManual] = useState('');
  const visible = policies.filter((policy) =>
    `${policy.name} ${policy.description}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <section className="admin-field-group" aria-label="Permissions for this key">
      <Text as="p" weight="medium">
        Policies
      </Text>
      {(policies.length > 5 || search) && (
        <TextField.Root
          aria-label="Search policies"
          placeholder="Search policy names…"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
      )}
      <div className="admin-policy-options">
        {visible.map((policy) => (
          <label className="admin-policy-option" key={policy.id}>
            <Checkbox
              disabled={disabled}
              checked={selected.includes(policy.id)}
              onCheckedChange={(checked) =>
                onChange(
                  checked ? [...selected, policy.id] : selected.filter((id) => id !== policy.id),
                )
              }
            />
            <span>
              <strong>{policy.name}</strong>
              <small>{policy.description || policySummary(policy.document)}</small>
            </span>
            {policy.platform && <small className="admin-policy-kind">Built in</small>}
          </label>
        ))}
        {!visible.length && (
          <p className="admin-inline-empty" role={loading ? 'status' : undefined}>
            {loading
              ? 'Loading policy directory…'
              : unavailable
                ? 'Policy names could not be loaded. Check your access or try again.'
                : policies.length
                  ? 'No policy names match this search.'
                  : 'No policies available.'}
          </p>
        )}
      </div>
      {unavailable && onRetry && (
        <Button type="button" size="1" variant="outline" disabled={loading} onClick={onRetry}>
          Retry policy directory
        </Button>
      )}
      {!!selected.length &&
        (search || selected.some((id) => !visible.some((policy) => policy.id === id))) && (
          <Text as="p" size="2" role="status">
            Selected:{' '}
            {selected
              .map(
                (id) =>
                  policies.find((policy) => policy.id === id)?.name ??
                  knownNames[id] ??
                  `Policy ${id.slice(0, 8)}…`,
              )
              .join(', ')}
          </Text>
        )}
      {unavailable && (
        <details className="admin-advanced">
          <summary>Use a policy ID from your administrator</summary>
          <Text as="p" size="1">
            List access is unavailable. An exact ID does not grant permission; the server checks
            whether your access allows attaching it.
          </Text>
          <TextField.Root
            aria-label="Policy ID"
            value={manual}
            onChange={(event) => setManual(event.target.value)}
          />
          <Button
            type="button"
            size="1"
            variant="outline"
            disabled={disabled || !manual.trim()}
            onClick={() => {
              const id = manual.trim();
              if (!selected.includes(id)) onChange([...selected, id]);
              setManual('');
            }}
          >
            Add policy
          </Button>
          {selected
            .filter((id) => !policies.some((p) => p.id === id))
            .map((id) => (
              <Button
                key={id}
                type="button"
                variant="ghost"
                size="1"
                onClick={() => onChange(selected.filter((value) => value !== id))}
              >
                Remove {id.slice(0, 8)}…
              </Button>
            ))}
        </details>
      )}
      {canCreate && (
        <Button type="button" variant="outline" size="1" disabled={disabled} onClick={onCreate}>
          Create a policy first
        </Button>
      )}
    </section>
  );
}
