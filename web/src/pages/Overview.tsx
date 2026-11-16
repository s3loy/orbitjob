import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Flex, Heading, Text } from '@radix-ui/themes';
import { Link } from 'react-router-dom';
import { Activity, ChevronRight, GitBranch, Layers, Network, RefreshCw, Zap } from 'lucide-react';
import { useSession } from '../session';
import { ErrorState, Loading, PageHeading, PhaseBadge } from '../components';
import { when } from '../format';
import { namespaceCatalogKnown } from '../workspace';
import { useAdministrationAuthenticationError } from '../administration-state';
import { withCredential } from '../credential-requests';

const actions = [
  ['/manifests/job', 'Create a job', Layers],
  ['/workflows', 'Manage workflows', GitBranch],
  ['/functions', 'Manage functions', Zap],
  ['/checks', 'Set up monitoring', Activity],
] as const;

function lastDay() {
  const now = Date.now();
  return {
    created_from: new Date(now - 86_400_000).toISOString(),
    created_before: new Date(now).toISOString(),
  };
}

export function Overview() {
  const { client, generation, identity } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [bounds, setBounds] = useState(lastDay);
  const summary = useQuery({
    queryKey: ['overview-summary', generation, bounds],
    queryFn: () => withCredential(client!, () => client!.runSummary(bounds)),
    enabled: !rejected,
  });
  const recent = useQuery({
    queryKey: ['overview-recent', generation],
    queryFn: () => withCredential(client!, () => client!.runs({ limit: 5 })),
    enabled: !rejected,
  });

  if (rejected)
    return (
      <>
        <PageHeading title="Overview" />
        <ErrorState error={rejected} />
      </>
    );

  return (
    <>
      <PageHeading
        title="Overview"
        action={
          <Button
            size="2"
            variant="soft"
            disabled={summary.isFetching || recent.isFetching}
            onClick={() => {
              setBounds(lastDay());
              void recent.refetch();
            }}
          >
            <RefreshCw size={14} aria-hidden />
            Refresh
          </Button>
        }
      />
      {namespaceCatalogKnown(identity) && identity && !identity.workload_namespaces.length && (
        <section className="overview-environment" aria-label="Execution setup needed">
          <Network size={18} aria-hidden />
          <div>
            <Heading as="h2" size="3">
              Execution setup required
            </Heading>
            <Text as="p" size="2" color="gray">
              Ask your administrator to configure an execution namespace for this workspace.
            </Text>
            <Link to="/connection" className="text-link">
              Workspace settings
            </Link>
          </div>
        </section>
      )}
      <div className="overview-content">
        <section
          className="overview-section overview-recent"
          aria-labelledby="overview-recent-title"
        >
          <Flex align="center" justify="between" gap="3" wrap="wrap" mb="3">
            <Heading id="overview-recent-title" as="h2" size="4" weight="medium">
              Recent runs
            </Heading>
            <Link className="text-link" to="/runs">
              View all runs
            </Link>
          </Flex>
          {recent.isPending ? (
            <Loading />
          ) : recent.error ? (
            <ErrorState error={recent.error} retry={() => void recent.refetch()} />
          ) : !recent.data.length ? (
            <div className="first-use">
              <Heading as="h3" size="3" weight="medium">
                No recorded runs
              </Heading>
              <Link to="/jobs" className="text-link">
                Open jobs
              </Link>
            </div>
          ) : (
            <ul className="recent-runs">
              {recent.data.map((run) => (
                <li key={String(run.id)}>
                  <Link to={`/runs/${run.id}`} className="overview-run-link">
                    <span>{run.source_name || `Run #${run.id}`}</span>
                    {run.source_name && (
                      <small>
                        {run.source_namespace
                          ? `Namespace ${run.source_namespace}`
                          : `Run #${run.id}`}
                      </small>
                    )}
                  </Link>
                  <PhaseBadge phase={run.phase} />
                  <time dateTime={run.created_at}>{when(run.created_at)}</time>
                </li>
              ))}
            </ul>
          )}
        </section>
        <section className="overview-section overview-outcomes" aria-label="Execution outcomes">
          <Heading as="h2" size="4" weight="medium" mb="1">
            Last 24 hours
          </Heading>
          <Text as="p" size="1" color="gray" mb="4">
            {when(bounds.created_from)} – {when(bounds.created_before)}
          </Text>
          {summary.isPending ? (
            <Loading />
          ) : summary.error ? (
            <ErrorState error={summary.error} retry={() => void summary.refetch()} />
          ) : (
            <dl className="outcome-strip">
              {(
                [
                  ['Recorded runs', summary.data.total],
                  ['Succeeded', summary.data.phases.Succeeded ?? 0],
                  ['Failed', summary.data.phases.Failed ?? 0],
                  ['Running', summary.data.phases.Running ?? 0],
                ] as const
              ).map(([label, count]) => (
                <div key={label}>
                  <dt>{label}</dt>
                  <dd>{count}</dd>
                </div>
              ))}
            </dl>
          )}
        </section>
        <section className="overview-section overview-shortcuts">
          <Heading as="h2" size="4" weight="medium" mb="3">
            Shortcuts
          </Heading>
          <div className="workspace-actions">
            {actions.map(([path, title, Icon]) => (
              <Link
                to={path}
                className={`workspace-action${path === '/manifests/job' ? ' workspace-action-primary' : ''}`}
                key={path}
              >
                <Icon size={18} aria-hidden className="workspace-action-icon" />
                <Text as="span" size="2" weight="medium">
                  {title}
                </Text>
                <ChevronRight size={16} aria-hidden className="workspace-action-direction" />
              </Link>
            ))}
          </div>
        </section>
      </div>
    </>
  );
}
