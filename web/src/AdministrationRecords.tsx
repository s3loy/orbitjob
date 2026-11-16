import { useState } from 'react';
import { Button, Dialog, Flex, Table, Text, TextField } from '@radix-ui/themes';
import type { AdministrationClient, Tenant, Key, Group, Policy } from './api/administration';
import { RecordIdentity } from './AdministrationActivity';
import { policySummary } from './administration-policy';
import type { Panel } from './AdministrationPanel';
import { ErrorState } from './components';
type Row = Tenant | Key | Group | Policy;
export function AdministrationRecords({
  panel,
  rows,
  disabled,
  keyActionsDisabled,
  policyDeleteAllowed = true,
  keyManagementAllowed = true,
  onConfirm,
  onRename,
  onWorkspace,
  workspaceSearch = '',
}: {
  panel: Panel;
  rows: Row[];
  disabled: boolean;
  keyActionsDisabled: boolean;
  policyDeleteAllowed?: boolean;
  keyManagementAllowed?: boolean;
  onConfirm(id: string): void;
  onRename(key: Key): void;
  onWorkspace?(workspace: Tenant): void;
  workspaceSearch?: string;
}) {
  return (
    <div className="table-scroll admin-record-table">
      <Table.Root size="1">
        <Table.Header>
          <Table.Row>
            <Table.ColumnHeaderCell>
              {panel === 'keys' ? 'Key name' : 'Name'}
            </Table.ColumnHeaderCell>
            <Table.ColumnHeaderCell>
              {panel === 'policies' ? 'Access' : panel === 'groups' ? 'Slug' : 'Status'}
            </Table.ColumnHeaderCell>
            <Table.ColumnHeaderCell>Actions</Table.ColumnHeaderCell>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {rows.length ? (
            rows.map((row) => {
              const key = 'key_prefix' in row ? row : undefined;
              const name = key ? key.name || 'Unnamed key' : 'name' in row ? row.name : '';
              return (
                <Table.Row key={row.id}>
                  <Table.RowHeaderCell>
                    <strong>{name}</strong>
                    {key && (
                      <Text as="p" size="1" color="gray" className="admin-key-prefix">
                        {key.key_prefix}
                      </Text>
                    )}
                    {'status' in row && row.slug.toLowerCase() !== row.name.toLowerCase() && (
                      <Text as="p" size="1" color="gray">
                        {row.slug}
                      </Text>
                    )}
                  </Table.RowHeaderCell>
                  <Table.Cell>
                    {'status' in row ? (
                      row.status === 'active' ? (
                        'Active'
                      ) : row.status === 'suspended' ? (
                        'Suspended'
                      ) : (
                        row.status
                      )
                    ) : 'platform' in row ? (
                      <>
                        <Text as="p" size="2">
                          {row.platform ? 'Built-in policy' : 'Workspace policy'}
                        </Text>
                        <Text as="p" size="1" color="gray">
                          {policySummary(row.document)}
                        </Text>
                      </>
                    ) : key ? (
                      key.revoked_at ? (
                        'Revoked'
                      ) : (
                        'Active'
                      )
                    ) : 'slug' in row ? (
                      row.slug
                    ) : (
                      'Active'
                    )}
                  </Table.Cell>
                  <Table.Cell>
                    <div className="admin-record-actions">
                      {'status' in row && onWorkspace && (
                        <Button
                          type="button"
                          variant="outline"
                          size="1"
                          disabled={disabled}
                          onClick={() => onWorkspace(row)}
                        >
                          Manage access
                        </Button>
                      )}
                      {key && (
                        <>
                          {!key.revoked_at && (
                            <Button
                              type="button"
                              variant="outline"
                              size="1"
                              disabled={disabled || !keyManagementAllowed}
                              onClick={() => onRename(key)}
                            >
                              Rename
                            </Button>
                          )}
                          {!key.revoked_at && (
                            <Button
                              type="button"
                              color="red"
                              variant="outline"
                              size="1"
                              disabled={keyActionsDisabled || !keyManagementAllowed}
                              onClick={() => onConfirm(row.id)}
                            >
                              Revoke
                            </Button>
                          )}
                        </>
                      )}
                      {key && !key.revoked_at && !keyManagementAllowed && (
                        <Text as="p" size="1" color="gray">
                          Select a workspace where your account has API key management access.
                        </Text>
                      )}
                      {'platform' in row && (
                        <>
                          <details className="admin-record-document">
                            <summary>View document</summary>
                            {row.description && <p>{row.description}</p>}
                            <pre>{JSON.stringify(row.document, null, 2)}</pre>
                          </details>
                          {!row.platform && (
                            <Button
                              type="button"
                              variant="outline"
                              color="red"
                              size="1"
                              disabled={disabled || !policyDeleteAllowed}
                              onClick={() => onConfirm(row.id)}
                            >
                              Delete
                            </Button>
                          )}
                          {!row.platform && !policyDeleteAllowed && (
                            <Text as="p" size="1" color="gray">
                              Connect to this workspace before deleting its policies.
                            </Text>
                          )}
                        </>
                      )}
                      <RecordIdentity id={row.id} />
                    </div>
                  </Table.Cell>
                </Table.Row>
              );
            })
          ) : (
            <Table.Row>
              <Table.Cell colSpan={3} className="admin-table-empty">
                <div>
                  <strong>
                    {workspaceSearch.trim() ? (
                      'No matching workspaces'
                    ) : (
                      <>
                        No{' '}
                        {panel === 'keys'
                          ? 'API keys'
                          : panel === 'policies'
                            ? 'policies'
                            : panel === 'groups'
                              ? 'resource groups'
                              : 'workspaces'}{' '}
                        yet
                      </>
                    )}
                  </strong>
                  {!!workspaceSearch.trim() && (
                    <Text as="p" size="2" color="gray">
                      Try another name or slug.
                    </Text>
                  )}
                </div>
              </Table.Cell>
            </Table.Row>
          )}
        </Table.Body>
      </Table.Root>
    </div>
  );
}
export function AdministrationDialogs({
  panel,
  tenant,
  confirm,
  rename,
  pending,
  unknown,
  onConfirmChange,
  onRenameChange,
  act,
  client,
  error,
}: {
  panel: Panel;
  tenant: string;
  confirm?: string;
  rename?: Key;
  pending: boolean;
  unknown: boolean;
  onConfirmChange(value?: string): void;
  onRenameChange(value?: Key): void;
  act(operation: () => Promise<void>): Promise<void>;
  client: AdministrationClient;
  error?: Error;
}) {
  const [renameDraft, setRenameDraft] = useState<{ id: string; name: string }>();
  const name = renameDraft?.id === rename?.id ? (renameDraft?.name ?? '') : (rename?.name ?? '');
  return (
    <>
      <Dialog.Root
        open={!!confirm}
        onOpenChange={(open) => {
          if (!open && !pending) onConfirmChange();
        }}
      >
        <Dialog.Content maxWidth="480px">
          <Dialog.Title>{panel === 'keys' ? 'Revoke API key' : 'Delete policy'}</Dialog.Title>
          <Dialog.Description>
            {panel === 'keys'
              ? 'Revocation stops authentication and cannot be undone.'
              : 'The API refuses platform presets and policies still bound to API keys. Revoking a key does not remove its policy bindings.'}{' '}
            Confirm record {confirm}.
          </Dialog.Description>
          {error && <ErrorState error={error} />}
          <Flex mt="4" gap="3" justify="end">
            <Button variant="outline" disabled={pending} onClick={() => onConfirmChange()}>
              Cancel
            </Button>
            <Button
              color="red"
              disabled={pending || unknown}
              onClick={() => {
                const target = confirm;
                if (target)
                  void act(async () => {
                    if (panel === 'keys') await client.adminRevokeKey(target, tenant);
                    else await client.adminDeletePolicy(target);
                    onConfirmChange();
                  });
              }}
            >
              Confirm {panel === 'keys' ? 'revoke' : 'delete'}
            </Button>
          </Flex>
        </Dialog.Content>
      </Dialog.Root>
      <Dialog.Root
        open={!!rename}
        onOpenChange={(open) => {
          if (!open && !pending) onRenameChange();
        }}
      >
        <Dialog.Content maxWidth="440px">
          <Dialog.Title>Rename API key</Dialog.Title>
          <Dialog.Description>
            Use a recognizable person or service name. The secret and permissions stay the same.
          </Dialog.Description>
          {error && <ErrorState error={error} />}
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const target = rename;
              if (target)
                void act(async () => {
                  await client.adminRenameKey(target.id, name.trim(), tenant);
                  onRenameChange();
                });
            }}
          >
            <label>
              Key name
              <TextField.Root
                aria-label="New key name"
                required
                maxLength={128}
                pattern=".*\S.*"
                value={name}
                onChange={(event) => {
                  if (rename) setRenameDraft({ id: rename.id, name: event.target.value });
                }}
              />
            </label>
            <Flex gap="2" mt="4" justify="end">
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => onRenameChange()}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={pending || unknown}>
                Save name
              </Button>
            </Flex>
          </form>
        </Dialog.Content>
      </Dialog.Root>
    </>
  );
}
