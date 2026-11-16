import { useEffect, useRef, useState } from 'react';
import { AlertDialog, Button, Flex, Heading, Text, TextField } from '@radix-ui/themes';
import type { LogParent } from './api/definitions';
import type { ArchivePage, ArchiveRecord } from './api/archive';
import { useSession } from './session';
import { ErrorState } from './components';
import { APIError } from './api/transport';
import { when } from './format';
import {
  rejectAdministrationCredential,
  useAdministrationAuthenticationError,
} from './administration-state';
import { copyText } from './clipboard';
import { prepareArchiveExport } from './archive-export';
import './execution-logs.css';

const stageNames: Record<string, string> = {
  accepted: 'Request accepted',
  configuration: 'Container and command',
  preparing: 'Scheduling and preparation',
  running: 'Command output',
  completed: 'Exit result',
};
const stages = Object.keys(stageNames);
const collectionLabels: Record<ArchivePage['state'], string> = {
  collecting: 'Collecting output',
  sealed: 'Collection finished',
  partial: 'Output incomplete',
  capacity: 'Storage limit reached',
  expired: 'Logs expired',
  deleted: 'Logs deleted',
  unavailable: 'Logs unavailable',
};
const MAX_LOADED = 4000;
function inert(text: string) {
  const escape = String.fromCharCode(27),
    bell = String.fromCharCode(7);
  return text
    .replace(new RegExp(`${escape}\\[[0-?]*[ -/]*[@-~]`, 'g'), '')
    .replace(new RegExp(`${escape}\\][^${bell}]*(?:${bell}|$)`, 'g'), '');
}

function OutputWindow({ records, follow }: { records: ArchiveRecord[]; follow: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const [top, setTop] = useState(0);
  const rows = records.flatMap((record) =>
    inert(record.text)
      .split('\n')
      .filter((text, index, all) => text || index < all.length - 1)
      .map((text, index) => ({ record, text, index })),
  );
  const height = 23;
  const start = Math.max(0, Math.floor(top / height) - 15);
  const end = Math.min(rows.length, start + 65);
  useEffect(() => {
    if (follow && ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [records, follow]);
  return (
    <div
      ref={ref}
      className="execution-output"
      aria-label="Execution log output"
      tabIndex={0}
      onScroll={(event) => setTop(event.currentTarget.scrollTop)}
    >
      <div style={{ height: rows.length * height, position: 'relative' }}>
        <div style={{ position: 'absolute', top: start * height, minWidth: '100%' }}>
          {rows.slice(start, end).map(({ record, text, index }) => (
            <div
              className={`execution-line ${record.kind === 'stderr' ? 'stderr' : ''}`}
              key={`${record.sequence}-${index}`}
              id={`log-${record.sequence}-${index}`}
            >
              <a
                href={`#log-${record.sequence}-${index}`}
                aria-label={`Log line ${record.sequence}.${index + 1}`}
              >
                {record.sequence}
                {index > 0 ? `.${index + 1}` : ''}
              </a>
              <time
                title={`${record.time_source === 'observed' ? 'Observed: ' : ''}${record.time}`}
              >
                {new Date(record.time).toLocaleTimeString(undefined, { hour12: false })}
              </time>
              <span className="execution-line-kind">
                {record.kind === 'event' ? 'event' : record.kind}
              </span>
              <code>{text || ' '}</code>
            </div>
          ))}
        </div>
      </div>
      {!rows.length && (
        <p className="execution-empty">No output observed for this selection yet.</p>
      )}
    </div>
  );
}

export function ExecutionLogs({ parent }: { parent: LogParent }) {
  const { generation } = useSession();
  const identity =
    parent.kind === 'instance' ? `instance/${parent.run}` : `function/${parent.id}/${parent.run}`;
  return <LogView key={`${generation}:${identity}`} parent={parent} />;
}

function LogView({ parent }: { parent: LogParent }) {
  const { client, generation } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [cache, setCache] = useState<{ records: ArchiveRecord[]; hidden: boolean }>({
    records: [],
    hidden: false,
  });
  const records = cache.records;
  const [page, setPage] = useState<ArchivePage>();
  const [error, setError] = useState<Error>();
  const [follow, setFollow] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const [stage, setStage] = useState('all');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [exporting, setExporting] = useState(false);
  const [notice, setNotice] = useState('');
  const [readNotice, setReadNotice] = useState('');
  const [knownAttempts, setKnownAttempts] = useState<number[]>([]);
  const [deleting, setDeleting] = useState(false);
  const hidden = cache.hidden;
  const cursor = useRef(0);
  const previousQuery = useRef('');
  const identity =
    parent.kind === 'instance' ? `instance/${parent.run}` : `function/${parent.id}/${parent.run}`;
  useEffect(() => {
    const timer = setTimeout(() => setQuery(search), 350);
    return () => clearTimeout(timer);
  }, [search]);
  useEffect(() => {
    if (!client?.archiveLogs || rejected) return;
    let active = true;
    let failureSince = 0;
    let timer: ReturnType<typeof setTimeout>;
    const selection = `${identity}:${attempt}:${query}:${refresh}`;
    let initial = previousQuery.current !== selection;
    previousQuery.current = selection;
    if (initial) cursor.current = 0;
    async function read() {
      try {
        const value = await client!.archiveLogs!(parent, {
          after: cursor.current,
          limit: 500,
          ...(attempt ? { attempt } : {}),
          ...(query ? { search: query } : {}),
        });
        if (!active) return;
        failureSince = 0;
        cursor.current = value.next;
        setPage(value);
        setError(undefined);
        setReadNotice('');
        setKnownAttempts((old) =>
          [...new Set([...old, ...value.records.map((r) => r.attempt).filter((n) => n > 0)])].sort(
            (a, b) => a - b,
          ),
        );
        const replace = initial || ['expired', 'deleted', 'unavailable'].includes(value.state);
        initial = false;
        setCache((old) => {
          const base = replace ? [] : old.records;
          const seen = new Set(base.map((r) => r.sequence));
          const next = [...base, ...value.records.filter((r) => !seen.has(r.sequence))];
          let units = next.reduce((sum, row) => sum + row.text.length, 0),
            start = 0;
          while (next.length - start > MAX_LOADED || units > 4 * 1024 * 1024) {
            units -= next[start].text.length;
            start++;
          }
          return { records: next.slice(start), hidden: (!replace && old.hidden) || start > 0 };
        });
        if (value.has_more || follow) timer = setTimeout(read, value.has_more ? 50 : 1500);
      } catch (cause) {
        if (!active) return;
        const failure = cause instanceof Error ? cause : new Error('Execution logs unavailable');
        rejectAdministrationCredential(client!, failure);
        setError(failure);
        setCache({ records: [], hidden: false });
        // A reconnect must reload evidence cleared after a failed read. Keeping
        // the old cursor with an empty cache would permanently hide history.
        cursor.current = 0;
        initial = true;
        if (
          follow &&
          failure instanceof APIError &&
          [0, 404, 429, 500, 502, 503, 504].includes(failure.status)
        ) {
          if (!failureSince) failureSince = Date.now();
          if (Date.now() - failureSince < 120000) {
            setReadNotice('Waiting for the execution archive. Reconnecting automatically…');
            timer = setTimeout(read, Math.max(2000, failure.retryAfter));
          }
        }
      }
    }
    void read();
    return () => {
      active = false;
      clearTimeout(timer);
    };
    // The parent identity is the stable key; it does not change on run refresh.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, generation, identity, attempt, query, follow, refresh, rejected]);

  async function download() {
    if (!client?.archiveLogs) return;
    setExporting(true);
    setNotice('Preparing retained log download…');
    try {
      const { chunks, notice } = await prepareArchiveExport(identity, (after) =>
        client.archiveLogs!(parent, { after, limit: 1000 }),
      );
      const url = URL.createObjectURL(new Blob(chunks, { type: 'text/plain;charset=utf-8' }));
      const a = document.createElement('a');
      a.href = url;
      a.download = `orbitjob-${parent.run}-logs.txt`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      setNotice(notice);
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : 'Download failed.');
    } finally {
      setExporting(false);
    }
  }

  if (!client?.archiveLogs || rejected) return null;
  const attempts = knownAttempts;
  const selected = stage === 'all' ? records : records.filter((r) => r.stage === stage);
  return (
    <section className="execution-log-view" aria-label="Execution logs">
      <Flex justify="between" align="center" gap="3" wrap="wrap" mb="3">
        <Heading as="h2" size="4">
          Execution logs
        </Heading>
        <Flex gap="2" wrap="wrap">
          <Button size="1" variant="outline" onClick={() => setFollow((value) => !value)}>
            {follow ? 'Pause following' : 'Follow output'}
          </Button>
          <Button size="1" variant="outline" onClick={() => setRefresh((value) => value + 1)}>
            Refresh output
          </Button>
          <Button
            size="1"
            variant="outline"
            disabled={!records.length}
            onClick={() => {
              void copyText(records.map((row) => row.text).join('')).then((copied) =>
                setNotice(
                  copied
                    ? 'Loaded log text copied.'
                    : 'Clipboard access was refused. Select output text or download the logs.',
                ),
              );
            }}
          >
            Copy loaded output
          </Button>
          <Button
            size="1"
            variant="outline"
            disabled={exporting || !page}
            onClick={() => void download()}
          >
            {exporting ? 'Preparing download…' : 'Download logs'}
          </Button>
          {client.deleteArchiveLogs &&
            page?.protected_until &&
            !['deleted', 'expired'].includes(page.state) && (
              <details className="execution-log-actions">
                <summary>Log actions</summary>
                <div className="execution-log-action-panel">
                  <AlertDialog.Root>
                    <AlertDialog.Trigger>
                      <Button size="1" variant="outline" color="red" disabled={deleting}>
                        Delete retained logs
                      </Button>
                    </AlertDialog.Trigger>
                    <AlertDialog.Content maxWidth="450px">
                      <AlertDialog.Title>Delete this execution’s logs?</AlertDialog.Title>
                      <AlertDialog.Description>
                        This requests permanent deletion of retained output and events, including
                        logs inside the 15-day protection period. The execution summary stays
                        available. The request is audited and requires DeleteLogs permission.
                      </AlertDialog.Description>
                      <Flex gap="3" justify="end" mt="4">
                        <AlertDialog.Cancel>
                          <Button variant="soft" color="gray">
                            Keep logs
                          </Button>
                        </AlertDialog.Cancel>
                        <AlertDialog.Action>
                          <Button
                            color="red"
                            onClick={() => {
                              setDeleting(true);
                              void client.deleteArchiveLogs!(parent)
                                .then(
                                  () => {
                                    setNotice(
                                      'Log deletion requested. The archive will update after cleanup.',
                                    );
                                    setRefresh((n) => n + 1);
                                  },
                                  (cause: unknown) =>
                                    setNotice(
                                      cause instanceof Error
                                        ? cause.message
                                        : 'Log deletion failed.',
                                    ),
                                )
                                .finally(() => setDeleting(false));
                            }}
                          >
                            Delete logs
                          </Button>
                        </AlertDialog.Action>
                      </Flex>
                    </AlertDialog.Content>
                  </AlertDialog.Root>
                </div>
              </details>
            )}
        </Flex>
      </Flex>
      <Text as="p" size="1" color="gray" mb="3">
        Container output and platform events across all attempts.
      </Text>
      <Flex gap="3" align="center" wrap="wrap" mb="3">
        <TextField.Root
          aria-label="Search execution logs"
          placeholder="Search retained logs…"
          value={search}
          maxLength={256}
          onChange={(event) => setSearch(event.target.value)}
        />
        <label className="execution-attempt-label">
          Attempt{' '}
          <select
            aria-label="Execution log attempt"
            value={attempt}
            onChange={(event) => setAttempt(Number(event.target.value))}
          >
            <option value={0}>All attempts</option>
            {attempts.map((n) => (
              <option key={n} value={n}>
                Attempt {n}
              </option>
            ))}
            {attempt > 0 && !attempts.includes(attempt) && (
              <option value={attempt}>Attempt {attempt}</option>
            )}
          </select>
        </label>
        {page && (
          <Text
            size="1"
            color={page.state === 'partial' || page.state === 'capacity' ? 'amber' : 'gray'}
            role="status"
          >
            {collectionLabels[page.state]}
          </Text>
        )}
      </Flex>
      {page && !['collecting', 'sealed'].includes(page.state) && (
        <p className={`execution-collection-state ${page.state === 'partial' ? 'has-gap' : ''}`}>
          {page.reason}
        </p>
      )}
      {page?.gaps.map((gap, index) => (
        <p className="execution-gap" key={index} role="status">
          Output gap {gap.from > 0 ? `${gap.from}–${gap.through}` : '(range unknown)'}: {gap.reason}
        </p>
      ))}
      {error && <ErrorState error={error} retry={() => setRefresh((value) => value + 1)} />}
      {hidden && (
        <p className="execution-gap">
          Earlier retained output is outside this view’s {MAX_LOADED}-record or 4 MiB limit. Search
          or download the logs to inspect it.
        </p>
      )}
      <div className="execution-log-layout">
        <nav aria-label="Execution log stages">
          <button
            className={stage === 'all' ? 'selected' : ''}
            aria-pressed={stage === 'all'}
            onClick={() => setStage('all')}
          >
            All stages
          </button>
          {stages.map((name) => (
            <button
              className={stage === name ? 'selected' : ''}
              aria-pressed={stage === name}
              key={name}
              onClick={() => setStage(name)}
            >
              <span>{stageNames[name]}</span>
              <small>{records.filter((r) => r.stage === name).length}</small>
            </button>
          ))}
        </nav>
        <OutputWindow records={selected} follow={follow} />
      </div>
      {page && (
        <details className="execution-collection-details">
          <summary>Collection details</summary>
          <dl className="definition-list">
            <dt>Collection state</dt>
            <dd>{page.state}</dd>
            <dt>Details</dt>
            <dd>{page.reason || 'No additional details'}</dd>
            <dt>Retained output</dt>
            <dd>
              {(page.retained_bytes / 1024).toFixed(1)} KiB of{' '}
              {(page.limit_bytes / 1024 / 1024).toFixed(0)} MiB
            </dd>
            {page.protected_until && (
              <>
                <dt>Protected until</dt>
                <dd>{when(page.protected_until)}</dd>
              </>
            )}
          </dl>
          <Text as="p" size="1" color="gray">
            Missing output does not confirm whether the command ran or finished.
          </Text>
        </details>
      )}
      {readNotice && <p role="status">{readNotice}</p>}
      {notice && <p role="status">{notice}</p>}
    </section>
  );
}
