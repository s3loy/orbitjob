import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Flex, TextField } from '@radix-ui/themes';
import { Boxes, ExternalLink, Search } from 'lucide-react';
import { useSession } from './session';
import { PAGE_SIZE } from './api/transport';
import { useAdministrationAuthenticationError } from './administration-state';
import { ErrorState } from './components';
import {
  imageCatalogProblem,
  loadImageSources,
  publicImageSearch,
  resolveSourceImage,
} from './image-catalog';
import type { ImageSource, ImageSourceKind } from './image-catalog';
import './container-image.css';

type Props = {
  label?: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  required?: boolean;
  error?: string;
};
export function ContainerImageField(props: Props) {
  const { generation } = useSession();
  return <ImageField key={generation} {...props} />;
}
function ImageField({
  label = 'Container image',
  value,
  onChange,
  disabled: providedDisabled = false,
  required = false,
  error,
}: Props) {
  const { client, generation, identity } = useSession();
  const rejected = useAdministrationAuthenticationError(client);
  const disabled = providedDisabled || !!rejected;
  const id = useId();
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<ImageSourceKind>(identity?.group_id ? 'functions' : 'jobs');
  const [search, setSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const [publicSearch, setPublicSearch] = useState('');
  const [choosing, setChoosing] = useState('');
  const [notice, setNotice] = useState('');
  const selection = useRef(0);
  const target = useRef({ value, disabled });
  useLayoutEffect(() => {
    target.current = { value, disabled };
  }, [value, disabled]);
  useEffect(
    () => () => {
      selection.current++;
    },
    [client, generation],
  );
  const query = useQuery({
    queryKey: ['container-images', generation, kind, offset, search],
    enabled: open && Boolean(client) && !disabled,
    retry: false,
    queryFn: () => loadImageSources(client!, kind, offset, search),
  });
  async function choose(source: ImageSource) {
    if (!client || disabled) return;
    const intent = ++selection.current;
    const baseline = target.current;
    const current = () =>
      intent === selection.current && target.current === baseline && !target.current.disabled;
    setChoosing(source.id);
    setNotice('');
    try {
      const image = await resolveSourceImage(client, source);
      if (!current()) return;
      onChange(image);
      setNotice(`Image copied from ${source.name}. Review your command and arguments.`);
    } catch (error) {
      if (current()) setNotice(imageCatalogProblem(error));
    } finally {
      if (intent === selection.current) setChoosing('');
    }
  }
  return (
    <div className="container-image-field">
      <label className="form-field" htmlFor={`${id}-reference`}>
        {label}
        <TextField.Root
          id={`${id}-reference`}
          aria-label={label}
          value={value}
          required={required}
          disabled={disabled}
          placeholder="registry.example.com/team/image:version"
          onChange={(event) => {
            selection.current++;
            setChoosing('');
            onChange(event.target.value);
            setNotice('');
          }}
          aria-invalid={Boolean(error) || undefined}
          aria-describedby={error ? `${id}-error` : undefined}
        />
      </label>
      {error && (
        <p id={`${id}-error`} className="authoring-field-error">
          {error}
        </p>
      )}
      <Button
        type="button"
        size="2"
        variant="outline"
        disabled={disabled}
        aria-expanded={open}
        aria-controls={`${id}-catalog`}
        onClick={() => {
          setOpen(!open);
          selection.current++;
          setChoosing('');
        }}
      >
        <Boxes size={16} aria-hidden /> {open ? 'Close image browser' : 'Browse images'}
      </Button>
      {open && (
        <section id={`${id}-catalog`} className="image-catalog" aria-label="Image browser">
          <h3>Workspace images</h3>

          <Flex gap="2" wrap="wrap" mb="3">
            {(['jobs', 'functions'] as const).map((source) => (
              <Button
                key={source}
                type="button"
                size="1"
                variant={kind === source ? 'solid' : 'soft'}
                aria-pressed={kind === source}
                disabled={disabled || Boolean(choosing)}
                onClick={() => {
                  setKind(source);
                  setSearch('');
                  setOffset(0);
                  setNotice('');
                }}
              >
                {source === 'jobs' ? 'Jobs' : 'Functions'}
              </Button>
            ))}
          </Flex>
          <label className="form-field" htmlFor={`${id}-search`}>
            Search workloads
            <TextField.Root
              id={`${id}-search`}
              aria-label="Search image source names"
              value={search}
              placeholder={
                kind === 'jobs'
                  ? 'Find a job to reuse its image…'
                  : 'Find a function to reuse its image…'
              }
              maxLength={128}
              disabled={disabled || Boolean(choosing)}
              onChange={(event) => {
                setSearch(event.target.value);
                setOffset(0);
              }}
            >
              <TextField.Slot>
                <Search size={15} aria-hidden />
              </TextField.Slot>
            </TextField.Root>
          </label>
          {rejected ? (
            <ErrorState error={rejected} />
          ) : !client ? (
            <p>
              Connect to a workspace to browse its workloads. You can still enter an image reference
              above.
            </p>
          ) : query.isPending ? (
            <p role="status">Loading workloads…</p>
          ) : query.error ? (
            <div role="status">
              <p>{imageCatalogProblem(query.error)}</p>
              <Button
                type="button"
                size="1"
                variant="outline"
                disabled={query.isFetching}
                onClick={() => void query.refetch()}
              >
                Try loading again
              </Button>
            </div>
          ) : (
            <>
              {!query.data?.items.length && (
                <p>
                  No reusable images found on this page. Try another name or source, or enter an
                  image reference above.
                </p>
              )}
              <ul className="image-source-list">
                {query.data?.items.map((source) => (
                  <li key={source.id}>
                    <div>
                      <strong>{source.name}</strong>
                      {'namespace' in source && source.namespace && <span>{source.namespace}</span>}
                      {'image' in source && source.image && <code>{source.image}</code>}
                    </div>
                    <Button
                      type="button"
                      size="1"
                      variant="outline"
                      disabled={disabled || Boolean(choosing)}
                      aria-label={`Reuse image from ${source.name}`}
                      onClick={() => void choose(source)}
                    >
                      {choosing === source.id ? 'Reading image…' : 'Reuse image'}
                    </Button>
                  </li>
                ))}
              </ul>
              <Flex gap="3" align="center" wrap="wrap" mt="3">
                <Button
                  type="button"
                  size="1"
                  variant="ghost"
                  disabled={!offset || query.isFetching || Boolean(choosing)}
                  onClick={() => setOffset(offset - PAGE_SIZE)}
                >
                  Previous sources
                </Button>
                <span className="image-page-label">Page {Math.floor(offset / PAGE_SIZE) + 1}</span>
                <Button
                  type="button"
                  size="1"
                  variant="ghost"
                  disabled={!query.data?.hasMore || query.isFetching || Boolean(choosing)}
                  onClick={() => setOffset(offset + PAGE_SIZE)}
                >
                  Next sources
                </Button>
              </Flex>
            </>
          )}
          <div className="public-image-search">
            <h3>Public images</h3>
            <p>
              Open Docker Hub, choose a version, then paste its full image reference above. The link
              shares only the public search text below.
            </p>
            <label className="form-field" htmlFor={`${id}-public`}>
              Public image search
              <TextField.Root
                id={`${id}-public`}
                aria-label="Public image search"
                value={publicSearch}
                placeholder="For example, python or busybox"
                maxLength={128}
                disabled={disabled}
                onChange={(event) => setPublicSearch(event.target.value)}
              />
            </label>
            {publicSearch.trim() && (
              <a
                href={publicImageSearch(publicSearch)}
                target="_blank"
                rel="noopener noreferrer"
                referrerPolicy="no-referrer"
              >
                Search Docker Hub <ExternalLink size={13} aria-hidden />
              </a>
            )}
            <p>
              For private images, ask your administrator to configure registry access for the
              cluster.
            </p>
          </div>
        </section>
      )}
      {notice && (
        <p className="image-selection-notice" role="status">
          {notice}
        </p>
      )}
    </div>
  );
}
