import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { AlertDialog, Button, Flex, TextField } from '@radix-ui/themes';
import { Search } from 'lucide-react';
import type { Draft } from './api/manifests';
import { PAGE_SIZE } from './api/transport';
import {
  administrationAuthenticationError,
  administrationCredential,
  useAdministrationAuthenticationError,
  useAdministrationCredential,
} from './administration-state';
import { ErrorState } from './components';
import { JobTaskTemplates } from './JobTaskTemplates';
import { useSession } from './session';
import {
  helloJobDraft,
  loadJobStartingSources,
  resolveJobStartingPoint,
  type JobStartingSource,
} from './job-starting-point';
import './job-starting-point.css';

type Props = {
  namespace: string;
  disabled?: boolean;
  hasContent: boolean;
  onApply(draft: Draft, sourceLabel: string): void;
  onCustom?(): void;
};
type Candidate = { draft: Draft; label: string };

export function JobStartingPoint(props: Props) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const credential = useAdministrationCredential(client);
  return (
    <StartingPoint
      key={`${generation}/${props.namespace}/${Boolean(props.disabled)}/${Boolean(rejected)}/${credential.stage === 'closed'}`}
      {...props}
    />
  );
}

function StartingPoint({ namespace, disabled = false, hasContent, onApply, onCustom }: Props) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const credential = useAdministrationCredential(client);
  const blocked = disabled || !!rejected || credential.stage === 'closed';
  const id = useId();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const [choosing, setChoosing] = useState('');
  const [failure, setFailure] = useState<Error | null>(null);
  const [notice, setNotice] = useState('');
  const [candidate, setCandidate] = useState<Candidate | null>(null);
  const request = useRef(0);
  const startingPoint = useRef<HTMLElement>(null);
  const replacementFocus = useRef<HTMLElement | null>(null);
  const target = useRef({ blocked, hasContent, onApply });
  useLayoutEffect(() => {
    target.current = { blocked, hasContent, onApply };
  }, [blocked, hasContent, onApply]);
  useEffect(
    () => () => {
      request.current++;
    },
    [client, generation],
  );
  const query = useQuery({
    queryKey: ['job-starting-sources', generation, search, offset],
    enabled: open && !!client && !blocked,
    queryFn: () => loadJobStartingSources(client!, offset, search),
    retry: false,
  });
  const busy = Boolean(choosing);

  function usable() {
    return (
      !target.current.blocked &&
      (!client ||
        (!administrationAuthenticationError(client) &&
          administrationCredential(client).stage !== 'closed'))
    );
  }
  function apply(next: Candidate) {
    if (!usable()) return;
    setCandidate(null);
    setFailure(null);
    setOpen(false);
    target.current.onApply(next.draft, next.label);
    setNotice('');
  }
  function propose(next: Candidate) {
    if (!usable()) return;
    if (target.current.hasContent) setCandidate(next);
    else apply(next);
  }
  async function choose(source: JobStartingSource) {
    if (!client || !usable() || busy) return;
    const intent = ++request.current;
    const current = () => intent === request.current && usable();
    setChoosing(String(source.id));
    setFailure(null);
    setNotice('');
    try {
      const draft = await resolveJobStartingPoint(client, source, namespace);
      if (current()) propose({ draft, label: `Job ${source.namespace}/${source.name}` });
    } catch (error) {
      if (current())
        setFailure(
          error instanceof Error ? error : new Error('The Job definition could not be read.'),
        );
    } finally {
      if (intent === request.current) setChoosing('');
    }
  }
  function cancelSelection() {
    request.current++;
    setChoosing('');
    setCandidate(null);
    setFailure(null);
    setNotice('');
  }

  return (
    <section className="job-starting-point" aria-labelledby={`${id}-title`} ref={startingPoint}>
      <h2 id={`${id}-title`}>Start with…</h2>
      <p>Choose a task with guided inputs, reuse a Job, or write your own code.</p>
      <Flex gap="2" wrap="wrap" align="center" className="job-starting-actions">
        <JobTaskTemplates
          namespace={namespace}
          disabled={blocked}
          onOpen={() => {
            if (document.activeElement instanceof HTMLElement)
              replacementFocus.current = document.activeElement;
            cancelSelection();
            setOpen(false);
          }}
          onApply={(draft, label) => {
            cancelSelection();
            setOpen(false);
            propose({ draft, label });
          }}
        />
        <Button
          type="button"
          size="2"
          variant="outline"
          disabled={blocked}
          aria-expanded={open}
          aria-controls={`${id}-sources`}
          onClick={(event) => {
            replacementFocus.current = event.currentTarget;
            cancelSelection();
            setOpen(!open);
          }}
        >
          {open ? 'Close Job browser' : 'Reuse a Job'}
        </Button>
        <Button
          type="button"
          size="2"
          variant="outline"
          disabled={blocked}
          onClick={(event) => {
            replacementFocus.current = event.currentTarget;
            cancelSelection();
            propose({ draft: helloJobDraft(namespace), label: 'Hello example' });
          }}
        >
          Use hello example
        </Button>
        <Button
          type="button"
          size="2"
          variant="outline"
          disabled={blocked}
          onClick={() => {
            cancelSelection();
            setOpen(false);
            onCustom?.();
          }}
        >
          Configure my own image
        </Button>
      </Flex>
      <p className="job-starting-hint">
        These options prepare a manual draft. The Hello example prints one line using{' '}
        <code>busybox:1.37.0</code>; your cluster must be able to pull this public image.
      </p>

      {open && (
        <div id={`${id}-sources`} className="job-starting-sources">
          <h3>Choose a readable Job</h3>
          <p>
            Jobs you can list are shown here. Reuse also needs permission to read the Job
            definition. The new draft keeps your target namespace and has no schedule.
          </p>
          {rejected ? (
            <ErrorState error={rejected} />
          ) : !client || credential.stage === 'closed' ? (
            <p>Connect to a workspace to reuse a Job, or configure your own image below.</p>
          ) : (
            <>
              <label className="form-field" htmlFor={`${id}-search`}>
                Search Jobs
                <TextField.Root
                  id={`${id}-search`}
                  type="search"
                  placeholder="Search Job names…"
                  maxLength={128}
                  value={search}
                  disabled={blocked || busy || !!candidate}
                  onChange={(event) => {
                    cancelSelection();
                    setSearch(event.target.value);
                    setOffset(0);
                  }}
                >
                  <TextField.Slot>
                    <Search size={15} aria-hidden />
                  </TextField.Slot>
                </TextField.Root>
              </label>
              {query.error ? (
                <>
                  <ErrorState error={query.error} retry={() => void query.refetch()} />
                  <p>
                    Try another readable source, use the hello example, or configure your own image.
                  </p>
                </>
              ) : query.isPending ? (
                <p role="status">Loading Jobs…</p>
              ) : (
                <>
                  {!query.data?.items.length && (
                    <p role="status">
                      No reusable Jobs on this page. Try another name
                      {query.data?.hasMore ? ' or the next page' : ''}, use the hello example, or
                      configure your own image.
                    </p>
                  )}
                  <ul className="job-starting-source-list">
                    {query.data?.items.map((source) => (
                      <li key={String(source.id)}>
                        <div>
                          <strong>{source.name}</strong>
                          <span>{source.namespace}</span>
                        </div>
                        <Button
                          type="button"
                          size="1"
                          variant="outline"
                          disabled={blocked || busy || !!candidate || query.isFetching}
                          aria-label={`Reuse Job ${source.namespace}/${source.name}`}
                          onClick={() => void choose(source)}
                        >
                          {choosing === String(source.id) ? 'Reading definition…' : 'Reuse Job'}
                        </Button>
                      </li>
                    ))}
                  </ul>
                  <Flex gap="3" wrap="wrap" align="center" mt="3">
                    <Button
                      type="button"
                      size="1"
                      variant="ghost"
                      disabled={blocked || !offset || query.isFetching || busy || !!candidate}
                      onClick={() => {
                        cancelSelection();
                        setOffset(offset - PAGE_SIZE);
                      }}
                    >
                      Previous Jobs
                    </Button>
                    <span className="job-starting-page">
                      Page {Math.floor(offset / PAGE_SIZE) + 1}
                    </span>
                    <Button
                      type="button"
                      size="1"
                      variant="ghost"
                      disabled={
                        blocked || !query.data?.hasMore || query.isFetching || busy || !!candidate
                      }
                      onClick={() => {
                        cancelSelection();
                        setOffset(offset + PAGE_SIZE);
                      }}
                    >
                      Next Jobs
                    </Button>
                    <Button
                      type="button"
                      size="1"
                      variant="ghost"
                      disabled={blocked || query.isFetching || busy || !!candidate}
                      onClick={() => {
                        cancelSelection();
                        void query.refetch();
                      }}
                    >
                      Reload Jobs
                    </Button>
                  </Flex>
                </>
              )}
              {failure && (
                <div className="job-starting-failure">
                  <ErrorState error={failure} />
                  <p>Your draft has been kept. Choose another Job or configure its fields below.</p>
                </div>
              )}
            </>
          )}
        </div>
      )}
      {notice && !rejected && credential.stage !== 'closed' && (
        <p className="job-starting-notice" role="status">
          {notice}
        </p>
      )}
      <AlertDialog.Root
        open={!!candidate && !blocked}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setCandidate(null);
        }}
      >
        <AlertDialog.Content
          maxWidth="470px"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            const target = replacementFocus.current;
            if (target?.isConnected) target.focus();
            else startingPoint.current?.querySelector('button')?.focus();
          }}
        >
          <AlertDialog.Title>Replace this Job draft?</AlertDialog.Title>
          <AlertDialog.Description>
            {candidate?.label} will replace this Job draft, including its name, execution content,
            policies and resource overrides. Your target namespace stays {namespace || 'unselected'}
            . The new draft has no schedule and is not suspended. Review it before saving.
          </AlertDialog.Description>
          <Flex justify="end" gap="3" mt="4" wrap="wrap">
            <AlertDialog.Cancel>
              <Button type="button" variant="soft" color="gray">
                Keep current draft
              </Button>
            </AlertDialog.Cancel>
            <AlertDialog.Action>
              <Button
                type="button"
                disabled={blocked}
                onClick={() => {
                  if (candidate) apply(candidate);
                }}
              >
                Replace draft
              </Button>
            </AlertDialog.Action>
          </Flex>
        </AlertDialog.Content>
      </AlertDialog.Root>
    </section>
  );
}
