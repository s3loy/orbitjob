import { Link } from 'react-router-dom';
import { Text } from '@radix-ui/themes';
import { useDefinitionDraft } from './definition-state';
import { useSession } from './session';

export function DefinitionReceipts() {
  const { client } = useSession();
  return client ? <ConnectedReceipts /> : null;
}
function ConnectedReceipts() {
  const { client, mode } = useSession();
  const jobs = useDefinitionDraft(client!, 'jobs');
  const workflows = useDefinitionDraft(client!, 'workflows');
  return (
    <>
      {(
        [
          ['jobs', jobs],
          ['workflows', workflows],
        ] as const
      ).map(
        ([kind, draft]) =>
          (draft.busy || draft.unknown || draft.receipt) && (
            <div className="operation-notice" role="status" key={kind}>
              <Text size="1">
                {draft.name} in {draft.namespace}.{' '}
                {draft.busy
                  ? draft.busy === 'apply'
                    ? 'Saving changes'
                    : draft.busy === 'reconcile'
                      ? 'Checking save outcome'
                      : draft.busy === 'check'
                        ? 'Checking configuration'
                        : 'Loading definition'
                  : draft.unknown
                    ? 'Save outcome unknown'
                    : mode === 'demo'
                      ? 'Saved for this session'
                      : 'Saved'}
                .{' '}
              </Text>
              <Link className="record-link" to={`/definitions/${kind}`}>
                {draft.receipt ? 'Review saved definition' : 'Review draft'}
              </Link>
            </div>
          ),
      )}{' '}
    </>
  );
}
