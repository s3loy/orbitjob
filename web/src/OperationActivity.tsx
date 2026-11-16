import { useState } from 'react';
import { useLocation } from 'react-router-dom';
import { Activity } from 'lucide-react';
import { useSession } from './session';
import { useOperations } from './operation-state';
import { useM4Operations } from './m4-state';
import { useDefinitionDraft } from './definition-state';
import { useAdministrationCredential, useAdministrationCreations } from './administration-state';
import { useMonitoringCreations } from './monitoring-creations';
import { TriggerReceipts } from './TriggerReceipts';
import { M4Receipts, WorkflowCancellationReceipts } from './m4-actions';
import { DefinitionReceipts } from './DefinitionReceipts';
import { AdministrationReceipt } from './AdministrationReceipt';
import { MonitoringReceipts } from './MonitoringReceipts';

export function OperationActivity() {
  const { client } = useSession();
  return client ? <ConnectedActivity /> : null;
}

function ConnectedActivity() {
  const { client } = useSession();
  const { pathname } = useLocation();
  const jobs = useOperations();
  const m4 = useM4Operations();
  const jobDraft = useDefinitionDraft(client!, 'jobs');
  const workflowDraft = useDefinitionDraft(client!, 'workflows');
  const credential = useAdministrationCredential(client);
  const creations = useAdministrationCreations(client);
  const visibleCreations = pathname === '/administration' ? [] : creations;
  const monitoring = useMonitoringCreations(client);
  const events = [
    ...Object.values(jobs.receipts),
    ...Object.values(jobs.intents).filter((i) => !i.receipt && (i.pending || i.error)),
    ...Object.values(m4.intents).filter((i) => i.pending || i.receipt || i.error),
  ];
  const cancellations = Object.entries(m4.cancellations).filter(
    ([, request]) => request.pending || request.receipt || request.error,
  );
  const definitions = [jobDraft, workflowDraft].filter((d) => d.busy || d.unknown || d.receipt);
  const keyActive = ['pending', 'revealed', 'unknown'].includes(credential.stage);
  const count =
    events.length +
    cancellations.length +
    definitions.length +
    visibleCreations.length +
    monitoring.length +
    Number(keyActive);
  const attention =
    events.filter((i) => i.error).length +
    cancellations.filter(([, request]) => request.error || request.unknown).length +
    definitions.filter((d) => d.unknown).length +
    Number(credential.stage === 'unknown');
  // A new operation opens feedback on its current page. Navigating elsewhere
  // collapses the area without unmounting observers or losing uncertain writes.
  const fingerprint = [
    ...events.map((i) => i.key),
    ...cancellations.map(([key]) => `cancel:${key}`),
    ...definitions.map((d) => d.requestID || `${d.namespace}/${d.name}`),
    ...visibleCreations.map((i) => `${i.kind}/${i.id}`),
    ...monitoring.map((i) => i.key),
    keyActive ? `${credential.tenant}/${credential.id ?? 'key-request'}` : '',
  ].join('|');
  const [view, setView] = useState({ pathname, fingerprint, open: Boolean(count) });
  if (view.pathname !== pathname || view.fingerprint !== fingerprint) {
    setView({ pathname, fingerprint, open: view.pathname === pathname && Boolean(count) });
  }
  if (!count) return null;
  return (
    <details
      className="operation-activity"
      aria-label="Operation activity"
      open={view.pathname === pathname && view.fingerprint === fingerprint && view.open}
    >
      <summary
        onClick={(event) => {
          event.preventDefault();
          setView((prior) => ({ pathname, fingerprint, open: !prior.open }));
        }}
      >
        <Activity size={15} aria-hidden="true" />
        <span>
          Activity <span className="activity-count">{count}</span>
        </span>
        {attention > 0 && <span className="activity-caption">{attention} need review</span>}
      </summary>
      <div className="operation-activity-content">
        <AdministrationReceipt />
        <DefinitionReceipts />
        <MonitoringReceipts />
        <TriggerReceipts />
        <M4Receipts />
        <WorkflowCancellationReceipts />
      </div>
    </details>
  );
}
