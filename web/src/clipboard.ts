// A synchronous copy keeps the user's gesture intact in embedded WebKit.
// Async Clipboard remains the fallback for browsers without legacy copy support.
export async function copyText(text: string): Promise<boolean> {
  const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const selection = window.getSelection();
  const ranges = selection
    ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange())
    : [];
  const input = document.createElement('textarea');
  input.value = text;
  input.readOnly = true;
  input.tabIndex = -1;
  input.setAttribute('aria-hidden', 'true');
  input.style.cssText =
    'position:fixed;left:0;top:0;width:1px;height:1px;opacity:0;pointer-events:none';
  document.body.append(input);
  let copied = false;
  try {
    input.focus({ preventScroll: true });
    input.select();
    copied = typeof document.execCommand === 'function' && document.execCommand('copy');
  } catch {
    /* Continue with the supported Clipboard API. */
  } finally {
    input.remove();
    active?.focus({ preventScroll: true });
    if (selection) {
      selection.removeAllRanges();
      for (const range of ranges) selection.addRange(range);
    }
  }
  if (copied) return true;
  try {
    if (!navigator.clipboard?.writeText) return false;
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

export function selectTextArea(id: string) {
  const input = document.getElementById(id);
  if (!(input instanceof HTMLTextAreaElement)) return;
  input.focus();
  input.select();
  input.setSelectionRange(0, input.value.length);
}
