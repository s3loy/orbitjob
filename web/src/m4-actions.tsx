import { Button, Dialog, Flex, Text } from '@radix-ui/themes';
import { useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useSession } from './session';
import { useM4Operations, cancellationKey, sameTarget, type Kind, type Target } from './m4-state';
import { ErrorState } from './components';
import { M4ReceiptObservation } from './M4ReceiptObservation';
import type { WorkflowRunDetail } from './api/m4';
import { useAdministrationAuthenticationError } from './administration-state';

export function M4Action({
  kind,
  item,
  disabled = false,
}: {
  kind: Kind;
  item: Target;
  disabled?: boolean;
}) {
  const ops = useM4Operations();
  const { mode, client } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [key, setKey] = useState<string | null>(null);
  const keep = useRef<HTMLButtonElement>(null);
  const receiptFocus = useRef<string | null>(null);
  const intent = key ? ops.intents[key] : undefined;
  const verb = kind === 'workflow' ? 'Trigger' : 'Invoke';
  const blocked =
    disabled ||
    Boolean(rejected) ||
    Boolean(ops.limitedUntil) ||
    item.status === 'paused' ||
    Object.values(ops.intents).some(
      (i) => i.kind === kind && sameTarget(kind, i.target, item) && (i.pending || i.unknown),
    );
  return (
    <Dialog.Root
      open={key !== null}
      onOpenChange={(open) => {
        if (open) {
          if (!blocked) setKey(ops.prepare(kind, item).key);
        } else if (!intent?.pending) {
          receiptFocus.current = intent?.receipt || intent?.error ? key : null;
          if (key && !intent?.error && !intent?.receipt) ops.dismiss(key);
          setKey(null);
        }
      }}
    >
      <Dialog.Trigger>
        <Button size="1" color="gray" disabled={blocked} aria-label={`${verb} ${item.name}`}>
          {verb}
        </Button>
      </Dialog.Trigger>
      <Dialog.Content
        maxWidth="450px"
        className="console-dialog"
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          keep.current?.focus();
        }}
        onCloseAutoFocus={(event) => {
          if (receiptFocus.current) {
            const el = document.getElementById(`m4-receipt-${receiptFocus.current}`);
            if (el) {
              event.preventDefault();
              el.focus();
            }
          }
        }}
        onEscapeKeyDown={(e) => {
          if (intent?.pending) e.preventDefault();
        }}
        onPointerDownOutside={(e) => {
          if (intent?.pending) e.preventDefault();
        }}
      >
        <Dialog.Title size="4">
          {verb} {intent?.target.name ?? item.name}?
        </Dialog.Title>
        <Dialog.Description size="2">
          {kind === 'workflow'
            ? 'Start one manual execution of this workflow.'
            : 'Start one invocation with the saved container settings.'}
        </Dialog.Description>
        <Text as="p" size="1" color="gray" mt="3">
          {kind === 'workflow'
            ? `Definition revision #${item.id}.`
            : `Only definition version ${intent?.target.version ?? item.version} can be invoked from this request.`}
        </Text>
        {mode === 'demo' && (
          <Text as="p" size="1" mt="3">
            This action only changes the sample data.
          </Text>
        )}
        {intent?.error && <ErrorState error={intent.error} />}
        {intent?.unknown && (
          <Text as="p" size="2" mt="3">
            The previous result is unknown. Close this dialog and use Retry same request on its
            receipt.
          </Text>
        )}
        {intent?.error && !intent.unknown && (
          <Text as="p" size="2" mt="3">
            This request was rejected. Close, refresh the definition and review before confirming
            another request.
          </Text>
        )}
        <Flex gap="3" justify="end" mt="5" wrap="wrap">
          <Dialog.Close>
            <Button ref={keep} size="1" color="gray" variant="soft" disabled={intent?.pending}>
              Keep reviewing
            </Button>
          </Dialog.Close>
          <Button
            size="1"
            color="gray"
            loading={intent?.pending}
            disabled={
              Boolean(intent?.error) ||
              Boolean(rejected) ||
              Boolean(ops.limitedUntil) ||
              disabled ||
              item.status === 'paused'
            }
            onClick={async () => {
              if (key && (await ops.submit(key))) {
                receiptFocus.current = key;
                setKey(null);
              }
            }}
          >
            Confirm {verb.toLowerCase()}
          </Button>
        </Flex>
      </Dialog.Content>
    </Dialog.Root>
  );
}

export function M4Receipts() {
  const ops = useM4Operations();
  const { client } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const latest = Object.values(ops.intents)
    .filter((i) => i.pending || i.receipt || i.error)
    .at(-1)?.key;
  return (
    <>
      {Object.values(ops.intents)
        .filter((i) => i.pending || i.receipt || i.error)
        .map((i) => (
          <section
            key={i.key}
            className="operation-notice"
            aria-label={`${i.kind === 'workflow' ? 'Workflow trigger' : 'Function invocation'} receipt for ${i.target.name}`}
            id={`m4-receipt-${i.key}`}
            tabIndex={-1}
          >
            <details open={!i.receipt || i.key === latest}>
              <summary>
                {i.kind === 'workflow' ? 'Workflow trigger' : 'Function invocation'}{' '}
                {i.pending
                  ? 'pending'
                  : i.receipt
                    ? i.receipt.phase === 'Rejected'
                      ? 'rejected'
                      : 'accepted'
                    : i.unknown
                      ? 'outcome unknown'
                      : 'rejected'}{' '}
                for {i.target.name}
              </summary>
              <Text as="p" size="1" mt="2">
                {i.receipt
                  ? i.receipt.phase === 'Rejected'
                    ? 'This request was rejected because another workflow execution is still open. No steps were started.'
                    : 'Acceptance does not confirm completion. The matching execution appears below once it is recorded.'
                  : i.unknown
                    ? 'No response confirmed this request. Retry the same request before starting another execution of this target.'
                    : 'Review this request before proceeding.'}
              </Text>
              <details>
                <summary>Request identity and original response</summary>
                <dl className="definition-list">
                  <dt>Request key</dt>
                  <dd className="mono">{i.key}</dd>
                  {i.receipt && (
                    <>
                      <dt>Resource</dt>
                      <dd className="mono">
                        {i.receipt.namespace}/{i.receipt.name}
                      </dd>
                      <dt>Occurrence key</dt>
                      <dd className="mono">{i.receipt.occurrence_key}</dd>
                      <dt>Phase at response</dt>
                      <dd>{i.receipt.phase || 'Not observed'}</dd>
                    </>
                  )}
                </dl>
              </details>
              {i.receipt && <M4ReceiptObservation intent={i} />}
              {i.error && <ErrorState error={i.error} />}
              <Flex gap="3" wrap="wrap" mt="3">
                {i.unknown && (
                  <Button
                    size="1"
                    color="gray"
                    variant="outline"
                    loading={i.pending}
                    disabled={Boolean(ops.limitedUntil) || !!rejected}
                    onClick={() => void ops.submit(i.key)}
                  >
                    Retry same request
                  </Button>
                )}
                <Button asChild size="1" color="gray" variant="ghost">
                  <Link
                    to={
                      i.kind === 'workflow'
                        ? `/workflows/${i.target.id}?tab=runs`
                        : `/functions/${i.target.id}`
                    }
                  >
                    Open {i.kind === 'workflow' ? 'workflow' : 'invocation'} history
                  </Link>
                </Button>
                <Button
                  size="1"
                  color="gray"
                  variant="ghost"
                  disabled={i.pending || i.unknown}
                  onClick={() => ops.dismiss(i.key)}
                >
                  Dismiss
                </Button>
              </Flex>
            </details>
          </section>
        ))}
    </>
  );
}

export function WorkflowCancel({
  workflowId,
  run,
  refresh,
  disabled = false,
}: {
  workflowId: string;
  run: WorkflowRunDetail;
  refresh: () => unknown;
  disabled?: boolean;
}) {
  const ops = useM4Operations();
  const { mode, client } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const [open, setOpen] = useState(false);
  const keep = useRef<HTMLButtonElement>(null);
  const cancellation = ops.cancellations[cancellationKey(run)];
  const ended = ['Succeeded', 'Failed', 'Canceled', 'Rejected'].includes(run.phase);
  const blocked =
    disabled ||
    Boolean(rejected) ||
    Boolean(ops.limitedUntil) ||
    ended ||
    run.phase === 'CancelRequested' ||
    run.phase === 'CancelUnknown' ||
    Boolean(cancellation?.pending) ||
    Boolean(cancellation?.receipt);
  return (
    <div>
      <Dialog.Root
        open={open}
        onOpenChange={(v) => {
          if (!cancellation?.pending) setOpen(v);
        }}
      >
        <Dialog.Trigger>
          <Button size="1" variant="outline" color="gray" disabled={blocked}>
            {cancellation?.receipt
              ? 'Cancel requested'
              : cancellation?.unknown
                ? 'Review cancel request'
                : 'Request cancel'}
          </Button>
        </Dialog.Trigger>
        <Dialog.Content
          maxWidth="450px"
          className="console-dialog"
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            keep.current?.focus();
          }}
          onCloseAutoFocus={(e) => {
            if (cancellation?.receipt || cancellation?.error) {
              const target = document.getElementById(`m4-cancel-${workflowId}-${run.id}`);
              if (target) {
                e.preventDefault();
                target.focus();
              }
            }
          }}
          onEscapeKeyDown={(e) => {
            if (cancellation?.pending) e.preventDefault();
          }}
          onPointerDownOutside={(e) => {
            if (cancellation?.pending) e.preventDefault();
          }}
        >
          <Dialog.Title size="4">Request cancellation?</Dialog.Title>
          <Dialog.Description size="2">
            Request a stop for workflow run #{run.id}. It may continue until all running steps are
            confirmed stopped.
          </Dialog.Description>
          {mode === 'demo' && (
            <Text as="p" size="1" mt="3">
              This action only changes the sample data.
            </Text>
          )}
          {cancellation?.error && <ErrorState error={cancellation.error} />}
          {cancellation?.unknown && (
            <Text as="p" size="2" mt="3">
              The request outcome is unknown. Refresh observed state before deliberately repeating
              this stop request.
            </Text>
          )}
          <Flex gap="3" justify="end" mt="5" wrap="wrap">
            <Dialog.Close>
              <Button
                ref={keep}
                size="1"
                color="gray"
                variant="soft"
                disabled={cancellation?.pending}
              >
                Keep observing
              </Button>
            </Dialog.Close>
            <Button
              size="1"
              color="red"
              loading={cancellation?.pending}
              disabled={blocked}
              onClick={async () => {
                if (await ops.cancel(workflowId, run)) {
                  setOpen(false);
                  void refresh();
                }
              }}
            >
              Confirm cancel request
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
    </div>
  );
}

export function WorkflowCancelNotice({
  workflowId,
  run,
}: {
  workflowId: string;
  run: WorkflowRunDetail;
}) {
  const ops = useM4Operations();
  const cancellation = ops.cancellations[cancellationKey(run)];
  if (!cancellation || !(cancellation.receipt || cancellation.error)) return null;
  return (
    <section
      className="operation-notice"
      role="status"
      id={`m4-cancel-${workflowId}-${run.id}`}
      tabIndex={-1}
    >
      <Text as="p" size="2">
        {cancellation.receipt
          ? 'Cancel request accepted. The observed workflow phase determines the outcome.'
          : cancellation.unknown
            ? 'Cancellation request outcome unknown. A stop has not been confirmed.'
            : 'Cancellation request rejected.'}
      </Text>
      {cancellation.receipt && (
        <Text as="p" size="1">
          Phase at response: {cancellation.receipt.phase || 'Not observed'}
        </Text>
      )}
      {cancellation.error && <ErrorState error={cancellation.error} />}
    </section>
  );
}

export function WorkflowCancellationReceipts() {
  const { cancellations } = useM4Operations();
  const { pathname } = useLocation();
  return Object.entries(cancellations)
    .filter(([, request]) => request.pending || request.receipt || request.error)
    .map(([key, request]) => {
      const path =
        request.workflowId && request.runId
          ? `/workflows/${encodeURIComponent(request.workflowId)}/runs/${encodeURIComponent(request.runId)}`
          : undefined;
      return (
        <section
          className="operation-notice"
          aria-label={`Workflow cancellation receipt for run ${request.runId || 'with unavailable context'}`}
          key={key}
        >
          <details open={pathname !== path}>
            <summary>
              Workflow cancellation{' '}
              {request.pending
                ? 'pending'
                : request.receipt
                  ? 'accepted'
                  : request.unknown
                    ? 'outcome unknown'
                    : 'rejected'}
              {request.runId ? ` for run #${request.runId}` : ''}
            </summary>
            <Text as="p" size="2" mt="2">
              {request.pending
                ? 'Waiting for the cancellation response. Leaving this page does not send another request.'
                : request.receipt
                  ? 'The stop request is accepted. Check the run to see whether execution has stopped.'
                  : request.unknown
                    ? 'The stop request has not been confirmed. Read the original execution before deliberately repeating a cancellation.'
                    : 'The stop request was rejected. Review the error and the original execution before continuing.'}
            </Text>
            {request.receipt && (
              <Text as="p" size="1" mt="2">
                Cancellation response phase: {request.receipt.phase || 'Not observed'}
              </Text>
            )}
            {request.error && <ErrorState error={request.error} />}
            {path ? (
              <Button asChild size="1" variant="outline" mt="3">
                <Link to={path}>Review workflow run #{request.runId}</Link>
              </Button>
            ) : (
              <Text as="p" size="1" color="gray" mt="2">
                The original run context is unavailable in this connection.
              </Text>
            )}
          </details>
        </section>
      );
    });
}
