import { Button, Dialog, Flex, Text, Tooltip } from '@radix-ui/themes';
import { Play } from 'lucide-react';
import { useRef, useState } from 'react';
import type { Job } from '../api/contracts';
import { ErrorState } from '../components';
import { useOperations } from '../operation-state';
import { useSession } from '../session';
import { useAdministrationAuthenticationError } from '../administration-state';

export function TriggerAction({
  job,
  compact = false,
  runOnce = false,
}: {
  job: Job;
  compact?: boolean;
  runOnce?: boolean;
}) {
  const { mode, client } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const operations = useOperations();
  const intent = operations.intents[String(job.id)];
  const [open, setOpen] = useState(false);
  const keep = useRef<HTMLButtonElement>(null);
  const accepted = Boolean(intent?.receipt);
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(value) => {
        if (value && rejected) return;
        if (value) operations.prepare(job);
        setOpen(value);
      }}
    >
      <Tooltip
        content={
          rejected
            ? 'Sign in again before triggering a run.'
            : job.suspend
              ? 'This definition is suspended. Edit definition to resume it before triggering.'
              : `Request a manual run of ${job.name}`
        }
      >
        <span>
          <Dialog.Trigger>
            <Button
              size="1"
              variant={compact ? 'ghost' : 'solid'}
              color="gray"
              disabled={job.suspend || Boolean(rejected)}
              aria-label={`${runOnce ? 'Run once' : 'Trigger'} ${job.name}`}
            >
              {!compact && <Play size={13} />}
              {job.suspend ? 'Paused' : runOnce ? 'Run once' : 'Trigger'}
            </Button>
          </Dialog.Trigger>
        </span>
      </Tooltip>
      <Dialog.Content
        maxWidth="440px"
        onOpenAutoFocus={(event) => {
          event.preventDefault();
          keep.current?.focus();
        }}
        onCloseAutoFocus={(event) => {
          if (intent?.receipt) {
            const target = document.getElementById(`receipt-${intent.key}`);
            if (target && target.getClientRects().length) {
              event.preventDefault();
              target.focus();
            }
          }
        }}
      >
        <Dialog.Title>
          {accepted
            ? runOnce
              ? 'Run request already accepted'
              : 'Trigger already accepted'
            : runOnce
              ? `Run ${job.name} once?`
              : `Trigger ${job.name}?`}
        </Dialog.Title>
        <Dialog.Description size="2">
          {accepted
            ? 'The accepted request is shown above. Check its run before starting another.'
            : `Request one manual run of ${job.name} in ${job.namespace}.`}
        </Dialog.Description>
        {!accepted && (
          <Text as="p" size="1" color="gray" mt="3">
            When the active policy is Forbid, this request waits until earlier executions of the
            same job finish. Acceptance does not mean execution has started.
          </Text>
        )}
        {mode === 'demo' && (
          <Text as="p" size="1" color="gray" mt="3">
            This action only changes the sample data.
          </Text>
        )}
        {(rejected || intent?.error) && <ErrorState error={(rejected || intent?.error)!} />}
        <Flex justify="end" gap="3" mt="5" wrap="wrap">
          <Dialog.Close>
            <Button ref={keep} size="1" variant="soft" color="gray">
              Keep reviewing
            </Button>
          </Dialog.Close>
          {accepted ? (
            <Button
              size="1"
              color="gray"
              disabled={Boolean(rejected)}
              onClick={() => operations.prepare(job, true)}
            >
              {runOnce ? 'Prepare another run' : 'Prepare another trigger'}
            </Button>
          ) : (
            <Button
              size="1"
              color="gray"
              loading={intent?.pending}
              disabled={Boolean(rejected)}
              onClick={async () => {
                if (rejected) return;
                if (await operations.submit(job)) setOpen(false);
              }}
            >
              {intent?.error
                ? runOnce
                  ? 'Retry same run request'
                  : 'Retry same trigger'
                : runOnce
                  ? 'Confirm run'
                  : 'Confirm trigger'}
            </Button>
          )}
        </Flex>
      </Dialog.Content>
    </Dialog.Root>
  );
}
