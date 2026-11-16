import { useEffect, useId, useRef, useState } from 'react';
import { Button, Flex, Text, TextField } from '@radix-ui/themes';
import { Check, Search } from 'lucide-react';
import { APIError, PAGE_SIZE } from './api/transport';
import type { Workspace } from './api/administration';
import { ErrorState } from './components';
import { useWorkspaceDirectory } from './workspace-directory';
import './administration.css';

export type WorkspaceLabel = Pick<Workspace, 'id' | 'name' | 'slug'>;

export function WorkspacePicker({
  value,
  selected,
  onChange,
  disabled = false,
  onAuthenticationError,
  label = 'Workspace',
}: {
  value: string;
  selected?: WorkspaceLabel;
  onChange(workspace: Workspace): void;
  disabled?: boolean;
  onAuthenticationError?(error: APIError): void;
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState({ search: '', offset: 0 });
  const menuID = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const query = useWorkspaceDirectory(filter.search, filter.offset, open && !disabled);
  const rejected = query.error instanceof APIError && query.error.status === 401;
  useEffect(() => {
    if (query.error instanceof APIError && query.error.status === 401)
      onAuthenticationError?.(query.error);
  }, [query.error, onAuthenticationError]);
  const rows = query.error ? [] : (query.data?.items ?? []);
  const current =
    rows.find((row) => row.id === value) ?? (selected?.id === value ? selected : undefined);
  function choose(workspace: Workspace) {
    if (disabled || rejected || query.isFetching) return;
    onChange(workspace);
    setOpen(false);
    trigger.current?.focus();
  }
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
        aria-label={label}
        aria-expanded={open}
        aria-controls={open ? menuID : undefined}
        disabled={disabled || rejected}
        ref={trigger}
        onClick={() => setOpen(!open)}
      >
        {rejected
          ? 'Sign in again to choose a workspace'
          : current?.name || (value ? 'Selected workspace' : 'Choose a workspace')}
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
          {!rejected && (
            <TextField.Root
              autoFocus
              type="search"
              aria-label="Search workspace"
              placeholder="Search workspace names or slugs…"
              value={filter.search}
              disabled={disabled}
              onChange={(event) => {
                const search = event.target.value;
                if (Array.from(search.trim()).length <= 128) setFilter({ search, offset: 0 });
              }}
            >
              <TextField.Slot>
                <Search size={14} />
              </TextField.Slot>
            </TextField.Root>
          )}
          {query.error ? (
            <ErrorState
              error={query.error}
              retry={rejected ? undefined : () => void query.refetch()}
            />
          ) : query.isPending || query.isFetching ? (
            <Text as="p" size="2" role="status">
              Loading workspaces…
            </Text>
          ) : (
            <div className="admin-option-list" role="group" aria-label="Workspace options">
              {rows.map((workspace) => (
                <button
                  type="button"
                  key={workspace.id}
                  aria-pressed={workspace.id === value}
                  disabled={disabled}
                  onClick={() => choose(workspace)}
                >
                  <span>
                    <strong>{workspace.name || workspace.slug}</strong>
                    {workspace.slug.toLowerCase() !== workspace.name.toLowerCase() && (
                      <small>{workspace.slug}</small>
                    )}
                  </span>
                  {workspace.id === value && <Check size={14} />}
                </button>
              ))}
              {!rows.length && (
                <Text as="p" size="2" color="gray">
                  {filter.search.trim()
                    ? 'No matching workspaces. Try another name or slug.'
                    : 'No workspaces are available. Ask your administrator to check your access.'}
                </Text>
              )}
            </div>
          )}
          {!rejected && (filter.offset > 0 || query.data?.has_more) && (
            <Flex gap="2" mt="3" wrap="wrap">
              <Button
                type="button"
                size="1"
                variant="outline"
                disabled={!filter.offset || query.isFetching}
                onClick={() =>
                  setFilter({ ...filter, offset: Math.max(0, filter.offset - PAGE_SIZE) })
                }
              >
                Previous workspaces
              </Button>
              <Button
                type="button"
                size="1"
                variant="outline"
                disabled={!!query.error || !query.data?.has_more || query.isFetching}
                onClick={() => setFilter({ ...filter, offset: filter.offset + PAGE_SIZE })}
              >
                More workspaces
              </Button>
            </Flex>
          )}
        </div>
      )}
    </div>
  );
}
