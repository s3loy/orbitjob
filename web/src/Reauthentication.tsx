import { useState } from 'react';
import { Button, Dialog, Flex, Text, TextField } from '@radix-ui/themes';
import type { AccountClient, AccountSnapshot } from './api/accounts';
import { ErrorState } from './components';

export function Reauthentication({
  accounts,
  onDone,
  onCancel,
}: {
  accounts: AccountClient;
  onDone(snapshot: AccountSnapshot): void;
  onCancel(): void;
}) {
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error>();
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !pending) onCancel();
      }}
    >
      <Dialog.Content maxWidth="420px">
        <Dialog.Title>Confirm your password</Dialog.Title>
        <Dialog.Description>
          Confirm your password to manage access. After confirmation, choose the action again.
        </Dialog.Description>
        <form
          onSubmit={async (event) => {
            event.preventDefault();
            if (pending) return;
            setPending(true);
            setError(undefined);
            try {
              onDone(await accounts.reauth(password));
              setPassword('');
            } catch (cause) {
              setError(
                cause instanceof Error ? cause : new Error('Could not confirm your password.'),
              );
            } finally {
              setPending(false);
            }
          }}
        >
          <Text as="label" htmlFor="reauth-password" size="2">
            Password
          </Text>
          <TextField.Root
            id="reauth-password"
            autoComplete="current-password"
            type="password"
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            mt="2"
          />
          {error && <ErrorState error={error} />}
          <Flex gap="3" mt="4" justify="end">
            <Button variant="outline" type="button" disabled={pending} onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending}>
              Confirm password
            </Button>
          </Flex>
        </form>
      </Dialog.Content>
    </Dialog.Root>
  );
}
