import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Flex, Text, TextField } from '@radix-ui/themes';
import { useSession } from './session';
import { useAdministrationAuthenticationError } from './administration-state';
import { withCredential } from './credential-requests';
import { PAGE_SIZE } from './api/transport';
import { ErrorState } from './components';
import { workloadOrigin } from './workload-origin';

type Props = {
  namespace: string;
  value: string;
  onChoose: (name: string) => void;
  disabled?: boolean;
};

export function WorkflowJobPicker(props: Props) {
  const { generation } = useSession();
  return <JobPicker key={`${generation}/${props.namespace}`} {...props} />;
}

function JobPicker({ namespace, value, onChoose, disabled = false }: Props) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: ['workflow-job-options', generation, namespace, search, offset],
    enabled: open && !!client && !!namespace && !disabled && !rejected,
    queryFn: () => withCredential(client!, () => client!.jobs(offset, search, { namespace })),
    retry: false,
  });
  const error = rejected ?? query.error;
  const rows = query.data ?? [];
  const jobs = rows.filter((job) => job.namespace === namespace && workloadOrigin(job).actionable);
  return (
    <div className="workflow-job-picker">
      <Button
        type="button"
        size="1"
        variant="outline"
        disabled={disabled || !!rejected}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {open ? 'Close job browser' : 'Choose an existing job'}
      </Button>
      {open && (
        <section className="m4-section" aria-label="Jobs in workflow namespace">
          <Text as="p" size="2" mb="2">
            {namespace ? `Jobs in ${namespace}` : 'Choose an execution namespace first.'}
          </Text>
          {namespace && (
            <TextField.Root
              type="search"
              aria-label="Search jobs in workflow namespace"
              placeholder="Search jobs…"
              maxLength={128}
              value={search}
              disabled={disabled || !!rejected}
              onChange={(event) => {
                setSearch(event.target.value);
                setOffset(0);
              }}
            />
          )}
          {error ? (
            <>
              <ErrorState error={error} retry={() => void query.refetch()} />
              {!rejected && (
                <Text as="p" size="1" color="gray" mt="2">
                  You can enter a known job name and use Check against cluster. Browsing and saving
                  require separate permissions.
                </Text>
              )}
            </>
          ) : namespace && query.isPending ? (
            <Text as="p" size="1" role="status" mt="3">
              Loading jobs…
            </Text>
          ) : namespace ? (
            <>
              {!jobs.length && (
                <Text as="p" size="1" role="status" mt="3">
                  No matching jobs on this page.{' '}
                  {rows.length >= PAGE_SIZE
                    ? 'Try another name or the next page, or enter a known Job name manually.'
                    : 'Try another name, or enter a known Job name manually.'}
                </Text>
              )}
              <Flex direction="column" align="start" gap="2" mt="3">
                {jobs.map((job) => (
                  <Button
                    key={String(job.id)}
                    type="button"
                    size="1"
                    variant={job.name === value ? 'soft' : 'outline'}
                    disabled={disabled || !!rejected}
                    aria-label={`Use job ${job.name}`}
                    aria-pressed={job.name === value}
                    onClick={() => onChoose(job.name)}
                  >
                    {job.name}
                    {job.suspend ? ' (suspended)' : ''}
                  </Button>
                ))}
              </Flex>
              <Flex gap="3" wrap="wrap" align="center" mt="3">
                <Button
                  type="button"
                  size="1"
                  variant="ghost"
                  disabled={!offset || query.isFetching || disabled}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                >
                  Previous jobs
                </Button>
                <Text size="1" color="gray">
                  Page {Math.floor(offset / PAGE_SIZE) + 1}
                </Text>
                <Button
                  type="button"
                  size="1"
                  variant="ghost"
                  disabled={rows.length < PAGE_SIZE || query.isFetching || disabled}
                  onClick={() => setOffset(offset + PAGE_SIZE)}
                >
                  More jobs
                </Button>
              </Flex>
            </>
          ) : null}
        </section>
      )}
    </div>
  );
}
