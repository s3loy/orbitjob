export type JobSection = 'general' | 'container' | 'schedule' | 'policies' | 'resources';

export function focusJobSection(root: HTMLElement | null, section: JobSection) {
  const target = root?.querySelector<HTMLElement>(`[data-job-section="${section}"]`);
  if (!target) return false;
  let ancestor: HTMLElement | null = target;
  while (ancestor && ancestor !== root) {
    if (ancestor instanceof HTMLDetailsElement) ancestor.open = true;
    ancestor = ancestor.parentElement;
  }
  target.scrollIntoView({ block: 'center', behavior: 'instant' });
  const control =
    (section === 'container'
      ? target.querySelector<HTMLElement>('input[aria-label="Container image"]:not(:disabled)')
      : null) ??
    target.querySelector<HTMLElement>(
      'input:not(:disabled), textarea:not(:disabled), select:not(:disabled), button:not(:disabled)',
    ) ??
    target.querySelector<HTMLElement>('summary');
  (control ?? target).focus({ preventScroll: true });
  return true;
}
