export function normalizeSiteOrigin(value) {
  const url = URL.parse(value);
  // Check the original syntax so URL normalization cannot erase a path or delimiter.
  if (!url || !/^https:\/\/[^/\\?#@\s\u0000-\u001f\u007f]+\/?$/i.test(value)) {
    throw new Error('SITE_ORIGIN must be an HTTPS origin without credentials, a path, a query, or a fragment');
  }
  return url.origin;
}

export const siteOrigin = normalizeSiteOrigin(process.env.SITE_ORIGIN ?? 'https://s3loy.github.io');
