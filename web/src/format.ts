export function when(value?: string | null) {
  if (!value) return 'Not recorded';
  return new Date(value).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

const runPhaseLabels: Record<string, string> = {
  RetryWaiting: 'Retry waiting',
  CancelRequested: 'Stop requested',
  CancelUnknown: 'Stop unconfirmed',
  ExecutionUnknown: 'Outcome unknown',
  CreatingAttempt: 'Preparing attempt',
};

export function runPhaseLabel(phase: string) {
  return runPhaseLabels[phase] ?? phase;
}
