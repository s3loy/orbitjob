const base = import.meta.env.BASE_URL.replace(/\/$/, '');

export function uiLocale(pathname: string) {
  const relative = pathname.slice(base.length);
  const zh = /^\/zh-cn(?:\/|$)/.test(relative);
  const prefix = `${base}${zh ? '/zh-cn' : ''}`;
  return {
    base, zh, prefix, lang: zh ? 'zh-CN' : 'en',
    t: (english: string, chinese: string) => zh ? chinese : english,
    link: (route = '') => `${prefix}/${route.replace(/^\/|\/$/g, '')}${route ? '/' : ''}`,
    counterpart: `${base}${zh ? '' : '/zh-cn'}${relative.replace(/^\/zh-cn(?=\/|$)/, '') || '/'}`,
  };
}
