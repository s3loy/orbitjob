import type { ArchivePage } from './api/archive';

export async function prepareArchiveExport(
  identity: string,
  read: (after: number) => Promise<ArchivePage>,
): Promise<{ chunks: string[]; notice: string }> {
  const chunks: string[] = [];
  const gaps = new Set<string>();
  const records = new Set<number>();
  const encoder = new TextEncoder();
  let size = 0;
  let after = 0;
  let collection = '';
  const append = (text: string) => {
    size += encoder.encode(text).length;
    if (size > 40 * 1024 * 1024)
      throw new Error(
        'Export exceeds this browser’s bounded download size. Use the authenticated API export.',
      );
    chunks.push(text);
  };
  append(`# OrbitJob ${identity}\n`);
  for (let count = 0; count < 100000; count++) {
    const result = await read(after);
    const current = JSON.stringify([result.state, result.reason]);
    if (current !== collection) {
      append(`# Collection: ${result.state} — ${result.reason}\n`);
      collection = current;
    }
    for (const gap of result.gaps) {
      const key = JSON.stringify([gap.from, gap.through, gap.reason]);
      if (gaps.has(key)) continue;
      append(`# OUTPUT GAP ${gap.from}-${gap.through}: ${gap.reason}\n`);
      gaps.add(key);
    }
    for (const row of result.records) {
      if (records.has(row.sequence)) continue;
      append(
        row.kind === 'event' ? `\n# ${row.time} [attempt ${row.attempt}] ${row.text}\n` : row.text,
      );
      records.add(row.sequence);
    }
    if (['expired', 'deleted', 'unavailable'].includes(result.state)) {
      append(
        `\n# EXPORT INCOMPLETE: logs ${result.state}; only output fetched above is included.\n`,
      );
      return {
        chunks,
        notice: `Download incomplete: logs ${result.state}. Previously fetched output and reported omissions are included in the file.`,
      };
    }
    if (!result.has_more) {
      append('\n# END OF RETAINED LOG EXPORT\n');
      if (result.state === 'collecting')
        append('# Collection is still in progress; this export contains the retained snapshot.\n');
      return {
        chunks,
        notice:
          result.state === 'collecting'
            ? 'Retained log snapshot downloaded. Collection is still in progress.'
            : 'Retained logs downloaded. Reported collection changes and omitted ranges are included in the file.',
      };
    }
    if (result.next <= after) throw new Error('Log export cursor did not advance.');
    after = result.next;
  }
  throw new Error('Log export exceeded its bounded page count.');
}
