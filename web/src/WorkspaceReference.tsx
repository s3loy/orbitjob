import { useRef, useState } from 'react';
import { Button, Text } from '@radix-ui/themes';
import { Copy } from 'lucide-react';
import { copyText } from './clipboard';

// An identifier is selectable text, never an editable-looking field.
export function WorkspaceReference({ value, label }: { value: string; label: string }) {
  const reference = useRef<HTMLElement>(null);
  const [notice, setNotice] = useState('');
  return (
    <span className="workspace-reference">
      <code ref={reference}>{value}</code>
      <Button
        type="button"
        variant="ghost"
        size="1"
        aria-label={`Copy ${label[0].toLowerCase()}${label.slice(1)}`}
        onClick={() => {
          void copyText(value).then((copied) => {
            if (copied) setNotice(`${label} copied.`);
            else {
              if (reference.current) {
                const range = document.createRange();
                range.selectNodeContents(reference.current);
                const selection = window.getSelection();
                selection?.removeAllRanges();
                selection?.addRange(range);
              }
              setNotice('Copy unavailable. Select this reference and copy manually.');
            }
          });
        }}
      >
        <Copy size={12} />
        Copy
      </Button>
      {notice && (
        <Text as="span" size="1" role="status">
          {notice}
        </Text>
      )}
    </span>
  );
}
