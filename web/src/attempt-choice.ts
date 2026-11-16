export type RecordedAttempt = {
  attempt_number: number;
  phase: string;
  started_at?: string | null;
  completed_at?: string | null;
};
export function defaultLogAttempt(
  attempts: RecordedAttempt[] | null | undefined,
  last?: number | null,
) {
  if (attempts?.length) {
    const numbers = attempts.map((item) => item.attempt_number);
    return String(last && numbers.includes(last) ? last : Math.max(...numbers));
  }
  if (attempts && !attempts.length) return '';
  return last && Number.isSafeInteger(last) && last > 0 ? String(last) : '';
}
