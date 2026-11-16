import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Flex, Text, TextField } from '@radix-ui/themes';
import { useSession } from './session';
import { ErrorState } from './components';
import { useAdministrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';
import { workloadOrigin } from './workload-origin';
import {
  monitoringSourceKind,
  type MonitoringSource,
  type MonitoringSourceKind,
} from './monitoring-source';

type Kind = 'jobs' | 'checks' | 'functions' | 'slis';
export function ResourcePicker({
  value,
  change,
  label,
  indicatorOnly = false,
  sourceKind,
  changeKind,
  selected,
}: {
  value: string;
  change: (value: string) => void;
  label: string;
  indicatorOnly?: boolean;
  sourceKind?: MonitoringSourceKind;
  changeKind?: (kind: MonitoringSourceKind) => void;
  selected?: (source: MonitoringSource | undefined) => void;
}) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [localKind, setKind] = useState<Kind>(indicatorOnly ? 'slis' : monitoringSourceKind(value));
  const kind = indicatorOnly ? 'slis' : (sourceKind ?? localKind);
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: ['resource-picker', generation, kind, search, offset],
    enabled: !rejected,
    queryFn: () =>
      withCredential(client!, async () => {
        if (kind === 'jobs') {
          const rows = await client!.jobs(offset, search);
          return {
            count: rows.length,
            items: rows
              .filter((item) => workloadOrigin(item).actionable)
              .map((item) => ({
                value: item.source_uid,
                label: `${item.name} (${item.namespace})`,
              })),
          };
        }
        if (kind === 'checks') {
          const rows = await client!.checks({ offset, search });
          return {
            count: rows.length,
            items: rows.map((item) => ({ value: `check-${item.id}`, label: item.name })),
          };
        }
        if (kind === 'functions') {
          const rows = await client!.functions({ offset, search });
          return {
            count: rows.length,
            items: rows.map((item) => ({ value: `function-${item.id}`, label: item.name })),
          };
        }
        const rows = await client!.slis({ offset, search });
        return {
          count: rows.length,
          items: rows.map((item) => ({ value: String(item.id), label: item.name })),
        };
      }),
  });
  if (rejected) return <ErrorState error={rejected} />;
  return (
    <section className="resource-picker" aria-label={label}>
      <Text as="p" size="2" weight="medium" mb="2">
        {label}
      </Text>
      {!indicatorOnly && (
        <label className="form-field">
          Source type
          <select
            aria-label="Source type"
            value={kind}
            onChange={(event) => {
              setKind(event.target.value as Kind);
              changeKind?.(event.target.value as MonitoringSourceKind);
              change('');
              selected?.(undefined);
              setOffset(0);
              setSearch('');
            }}
          >
            <option value="jobs">Job</option>
            <option value="checks">Check</option>
            <option value="functions">Function</option>
          </select>
        </label>
      )}
      <TextField.Root
        aria-label={`Search ${label.toLowerCase()}`}
        placeholder="Search resources…"
        value={search}
        maxLength={128}
        onChange={(event) => {
          setSearch(event.target.value);
          setOffset(0);
        }}
        mb="2"
      />
      {query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : (
        <label className="form-field">
          Choose a resource
          <select
            aria-label={label}
            value={value}
            disabled={query.isFetching}
            onChange={(event) => {
              const value = event.target.value;
              change(value);
              const option = query.data?.items.find((item) => item.value === value);
              selected?.(option && kind !== 'slis' ? { ...option, kind, to: '' } : undefined);
            }}
          >
            <option value="">{query.isPending ? 'Loading resources…' : 'Select a resource'}</option>
            {value && !query.data?.items.some((item) => item.value === value) && (
              <option value={value}>Selected source ({value})</option>
            )}
            {query.data?.items.map((item) => (
              <option key={item.value} value={item.value}>
                {item.label}
              </option>
            ))}
          </select>
        </label>
      )}
      {query.data && !query.data.items.length && (
        <Text as="p" size="1" color="gray">
          No matching resources.
        </Text>
      )}
      <Flex gap="3" mt="2">
        <Button
          type="button"
          size="1"
          variant="ghost"
          disabled={!offset || query.isFetching}
          onClick={() => setOffset(Math.max(0, offset - 20))}
        >
          Previous resources
        </Button>
        <Button
          type="button"
          size="1"
          variant="ghost"
          disabled={!query.data || query.data.count < 20 || query.isFetching}
          onClick={() => setOffset(offset + 20)}
        >
          More resources
        </Button>
      </Flex>
    </section>
  );
}
