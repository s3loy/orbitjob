import { useEffect, useRef, useState } from 'react';
import { Button, Flex, Heading, Text } from '@radix-ui/themes';
import { CheckCircle2, Copy } from 'lucide-react';
import { useSession } from './session';
import { copyText } from './clipboard';
import {
  dismissAdministrationCreation,
  useAdministrationCreations,
  type AdministrationCreation,
} from './administration-state';

export function RecordIdentity({ id, label = 'ID' }: { id: string; label?: string }) {
  const [copy, setCopy] = useState('');
  return (
    <details className="admin-record-identity">
      <summary>{label === 'ID' ? 'Reference' : label}</summary>
      <code className="admin-full-id" aria-label={label}>
        {id}
      </code>
      <Button
        type="button"
        size="1"
        variant="ghost"
        aria-label={`Copy ${label}`}
        onClick={() => {
          void copyText(id).then((copied) =>
            setCopy(copied ? 'ID copied.' : 'Select the ID above and copy manually.'),
          );
        }}
      >
        <Copy size={12} /> Copy ID
      </Button>
      {copy && (
        <Text as="p" size="1" role="status">
          {copy}
        </Text>
      )}
    </details>
  );
}
export function AdministrationActivity({
  onContinue,
  highlight = false,
  disabled = false,
}: {
  onContinue(receipt: AdministrationCreation): void;
  highlight?: boolean;
  disabled?: boolean;
}) {
  const { client } = useSession();
  const receipts = useAdministrationCreations(client);
  const section = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!highlight) return;
    const frame = requestAnimationFrame(() => {
      section.current?.scrollIntoView({ block: 'start' });
      section.current?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [highlight]);
  if (!receipts.length && !highlight) return null;
  return (
    <section
      className="admin-activity"
      aria-label="Recent changes"
      id="administration-activity"
      ref={section}
      tabIndex={-1}
    >
      <Heading as="h2" size="3">
        Recent changes
      </Heading>
      {!receipts.length && (
        <Text as="p" size="2" color="gray">
          No changes in this session.
        </Text>
      )}
      <div className="admin-activity-list">
        {[...receipts].reverse().map((receipt) => (
          <CreationReceipt
            key={`${receipt.kind}/${receipt.id}`}
            receipt={receipt}
            onContinue={onContinue}
            highlight={highlight}
            client={client}
            disabled={disabled}
          />
        ))}
      </div>
    </section>
  );
}
function CreationReceipt({
  receipt,
  onContinue,
  highlight,
  client,
  disabled,
}: {
  receipt: AdministrationCreation;
  onContinue(receipt: AdministrationCreation): void;
  highlight: boolean;
  client: object | null;
  disabled: boolean;
}) {
  const kind = { tenants: 'workspace', policies: 'policy', groups: 'resource group' }[receipt.kind];
  return (
    <details className="admin-activity-entry" open={highlight || undefined}>
      <summary>
        <CheckCircle2 size={16} aria-hidden="true" />
        <strong>{receipt.name}</strong>
        <small>{kind[0].toUpperCase() + kind.slice(1)} created</small>
      </summary>
      <section aria-label={`Created ${kind} receipt`}>
        <RecordIdentity label={`Created ${kind} ID`} id={receipt.id} />
        <Text as="p" size="1" color="gray">
          Saved. Reading this record requires separate permission.
        </Text>
        <Flex gap="2" mt="3" wrap="wrap">
          <Button
            type="button"
            size="1"
            variant="outline"
            disabled={disabled}
            onClick={() => onContinue(receipt)}
          >
            {receipt.kind === 'tenants'
              ? 'Create a key in this workspace'
              : receipt.kind === 'policies'
                ? 'Use this policy for a key'
                : 'Scope a key to this group'}
          </Button>
          <Button
            type="button"
            size="1"
            variant="ghost"
            onClick={() => {
              if (client) dismissAdministrationCreation(client, receipt.kind, receipt.id);
            }}
          >
            Dismiss {kind} receipt
          </Button>
        </Flex>
      </section>
    </details>
  );
}
