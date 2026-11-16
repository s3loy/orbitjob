type TitleEntry = { title: string; route: string };
type ResultsMessage = { element: HTMLElement; noResults: boolean };

// Pagefind remains the full-text engine. Title matches recover a query whose
// browser word segmentation differs from the indexer's Chinese segmentation,
// including a query that finds unrelated full-text results.
export function supplementChineseTitles(host: HTMLElement): () => void {
  const sourcePath = host.dataset.titleIndex;
  const dialog = host.querySelector('dialog');
  const root = host.querySelector<HTMLElement>('#starlight__search');
  if (!sourcePath || !dialog || !root) return () => {};
  const source = new URL(sourcePath, location.href);
  if (source.origin !== location.origin) return () => {};
  const prefix = source.pathname.replace(/search-titles\.json$/, '');
  const region = document.createElement('section');
  region.className = 'title-search-fallback';
  region.setAttribute('aria-label', '标题匹配');
  region.hidden = true;
  const heading = document.createElement('h2');
  heading.textContent = '标题匹配';
  const list = document.createElement('ul');
  region.append(heading, list);
  root.after(region);

  let titles: TitleEntry[] | undefined;
  let request: AbortController | undefined;
  let revision = 0;
  let renderedQuery = '';
  let hiddenMessage: HTMLElement | undefined;
  const query = () => root.querySelector<HTMLInputElement>('input')?.value.trim() || '';
  const normalize = (text: string) => text.toLocaleLowerCase().replace(/\s+/g, '');
  const clear = () => {
    if (hiddenMessage) hiddenMessage.hidden = false;
    hiddenMessage = undefined;
    region.hidden = true;
    list.replaceChildren();
    renderedQuery = '';
  };
  const cancel = () => {
    revision++;
    request?.abort();
    request = undefined;
    clear();
  };
  const completedResults = (term: string): ResultsMessage | undefined => {
    if (!dialog.open || !term) return;
    const message = root.querySelector<HTMLElement>('.pagefind-ui__message');
    if (!message) return;
    const text = message.textContent?.trim() || '';
    const completedTerm = text.match(/^(?:未找到 |找到 \d[\d,]* 个 )(.+) 的相关结果$/)?.[1].trim();
    if (completedTerm !== term) return;
    const noResults = text.startsWith('未找到 ');
    if (noResults && root.querySelector('.pagefind-ui__result')) return;
    return { element: message, noResults };
  };
  const render = (term: string, message: ResultsMessage) => {
    const needle = normalize(term);
    const matches = (titles || []).filter((entry) => normalize(entry.title).includes(needle))
      .sort((a, b) => Number(normalize(b.title) === needle) - Number(normalize(a.title) === needle))
      .slice(0, 8);
    if (!matches.length) { clear(); return; }
    if (renderedQuery !== term) {
      list.replaceChildren(...matches.map((entry) => {
        const item = document.createElement('li');
        const link = document.createElement('a');
        link.href = entry.route;
        link.textContent = entry.title;
        item.append(link);
        return item;
      }));
      renderedQuery = term;
    }
    if (hiddenMessage) hiddenMessage.hidden = false;
    hiddenMessage = message.noResults ? message.element : undefined;
    if (hiddenMessage) hiddenMessage.hidden = true;
    region.hidden = false;
  };
  const update = async () => {
    const term = query();
    const message = completedResults(term);
    if (!message) { clear(); return; }
    if (titles) { render(term, message); return; }
    if (request) return;
    const token = revision;
    const controller = new AbortController();
    request = controller;
    try {
      const response = await fetch(source, { credentials: 'omit', signal: controller.signal });
      if (!response.ok) throw new Error(`Title catalog returned ${response.status}`);
      const data: unknown = await response.json();
      if (!Array.isArray(data)) throw new Error('Title catalog is not an array');
      titles = data.filter((entry): entry is TitleEntry => typeof entry?.title === 'string' && typeof entry?.route === 'string'
        && entry.route.startsWith(prefix) && new URL(entry.route, location.origin).origin === location.origin);
      if (token !== revision || !dialog.open || term !== query()) return;
      const currentMessage = completedResults(term);
      if (currentMessage) render(term, currentMessage);
    } catch (error) {
      // Keep Pagefind's current result or no-match message intact on failure.
      if (!controller.signal.aborted) console.warn('OrbitJob title search supplement could not load:', error);
    } finally {
      if (request === controller) request = undefined;
    }
  };
  const onInput = () => { cancel(); };
  const onOpenChange = () => { if (dialog.open) void update(); else cancel(); };
  const resultsObserver = new MutationObserver(() => { void update(); });
  resultsObserver.observe(root, { childList: true, subtree: true, characterData: true });
  const openObserver = new MutationObserver(onOpenChange);
  openObserver.observe(dialog, { attributes: true, attributeFilter: ['open'] });
  root.addEventListener('input', onInput);
  dialog.addEventListener('close', cancel);
  return () => {
    cancel();
    resultsObserver.disconnect();
    openObserver.disconnect();
    root.removeEventListener('input', onInput);
    dialog.removeEventListener('close', cancel);
    region.remove();
  };
}
