export function newAccountPasswordError(password: string): string | undefined {
  if ([...password].length < 15) return 'Use at least 15 characters for your new password.';
  if (new TextEncoder().encode(password).byteLength > 1024)
    return 'Your new password exceeds the supported length. Use a shorter passphrase.';
  return undefined;
}
