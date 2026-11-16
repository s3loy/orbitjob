import { Button, Flex, Text, TextField } from '@radix-ui/themes';
import { useState, type ReactNode } from 'react';
import { queryWindow, queryWindowParams } from './api/query-window';

type Props = {
  params: URLSearchParams;
  change(next: URLSearchParams): void;
  timestamp?: string;
  disabled?: boolean;
  leading?: ReactNode;
};
export function HistoryWindow(props: Props) {
  const key = `${props.params.get('created_from') ?? ''}\n${props.params.get('created_before') ?? ''}`;
  return <WindowEditor key={key} {...props} />;
}
function WindowEditor({
  params,
  change,
  timestamp = 'Created time',
  disabled = false,
  leading,
}: Props) {
  const [from, setFrom] = useState(params.get('created_from') ?? '');
  const [before, setBefore] = useState(params.get('created_before') ?? '');
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useState(
    () => params.has('created_from') || params.has('created_before'),
  );
  function apply() {
    const next = queryWindowParams(params, from, before);
    const result = queryWindow(next);
    if (result.error) {
      setError(result.error);
      return;
    }
    setError('');
    change(next);
  }
  return (
    <div className="history-window">
      {leading}
      <details
        className="history-range"
        open={expanded}
        onToggle={(event) => setExpanded(event.currentTarget.open)}
      >
        <summary>
          {timestamp} range
          {params.has('created_from') || params.has('created_before') ? ' applied' : ''}
        </summary>
        <form
          aria-label={`${timestamp} range`}
          onSubmit={(e) => {
            e.preventDefault();
            apply();
          }}
        >
          <Flex gap="3" align="end" wrap="wrap">
            <label style={{ display: 'grid', gap: 6, flex: '1 1 220px', minWidth: 0 }}>
              <Text size="1">{timestamp} from (inclusive)</Text>
              <TextField.Root
                aria-label={`${timestamp} from`}
                placeholder="2026-10-19T00:00:00Z"
                value={from}
                disabled={disabled}
                onChange={(e) => setFrom(e.target.value)}
              />
            </label>
            <label style={{ display: 'grid', gap: 6, flex: '1 1 220px', minWidth: 0 }}>
              <Text size="1">{timestamp} before (exclusive)</Text>
              <TextField.Root
                aria-label={`${timestamp} before`}
                placeholder="2026-10-20T00:00:00Z"
                value={before}
                disabled={disabled}
                onChange={(e) => setBefore(e.target.value)}
              />
            </label>
            <Button type="submit" variant="soft" disabled={disabled}>
              Apply range
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={
                disabled ||
                (!from && !before && !params.has('created_from') && !params.has('created_before'))
              }
              onClick={() => {
                setFrom('');
                setBefore('');
                setError('');
                change(queryWindowParams(params, '', ''));
              }}
            >
              Clear range
            </Button>
          </Flex>
          <Text as="p" size="1" color="gray" mt="2">
            Use a date and time with Z (UTC) or a timezone offset. Leave either field empty for an
            open range.
          </Text>
          {error && (
            <Text as="p" size="1" color="red" role="alert" mt="2">
              {error}
            </Text>
          )}
        </form>
      </details>
    </div>
  );
}
