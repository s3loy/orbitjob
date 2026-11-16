import { useQuery } from '@tanstack/react-query';
import { Button, Text } from '@radix-ui/themes';
import { Link, useLocation } from 'react-router-dom';
import { forwardState } from './monitoring-state';
import { resolveMonitoringSource } from './monitoring-source';
import { useSession } from './session';
import { useAdministrationAuthenticationError } from './administration-state';
import { ErrorState } from './components';

export function MonitoringSourceLink({ value }: { value: unknown }) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const location = useLocation();
  const uid = typeof value === 'string' ? value : '';
  const query = useQuery({
    queryKey: ['monitoring-source', generation, uid],
    enabled: Boolean(uid) && !rejected,
    retry: false,
    queryFn: () => resolveMonitoringSource(client!, uid),
  });
  if (rejected) return <ErrorState error={rejected} />;
  if (!uid) return <>No source recorded</>;
  return (
    <>
      {query.data ? (
        <>
          <Link className="record-link" to={query.data.to} state={forwardState(location)}>
            {query.data.label}
          </Link>
          <Text className="monitoring-source-kind" size="1" color="gray">
            {query.data.kind === 'checks'
              ? 'Check'
              : query.data.kind === 'functions'
                ? 'Function'
                : 'Job'}
          </Text>
        </>
      ) : (
        <Text size="2">{query.isPending ? 'Loading source…' : 'Source unavailable.'}</Text>
      )}
      {query.error && (
        <>
          <Link className="record-link" to="/connection">
            Review source access
          </Link>
          <Button type="button" size="1" variant="ghost" onClick={() => void query.refetch()}>
            Retry source lookup
          </Button>
        </>
      )}
      <details>
        <summary>Source ID</summary>
        <code>{uid}</code>
      </details>
    </>
  );
}
