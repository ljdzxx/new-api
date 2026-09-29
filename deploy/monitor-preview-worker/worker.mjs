// Public HTML previews from a private R2 bucket. Diagnostic files stay private.
export default {
  async fetch(request, env, ctx) {
    if (!['GET', 'HEAD'].includes(request.method)) {
      return new Response('Method not allowed', { status: 405, headers: { Allow: 'GET, HEAD' } });
    }
    const url = new URL(request.url);
    let key;
    try { key = decodeURIComponent(url.pathname.slice(1)); }
    catch { return new Response('Not found', { status: 404 }); }
    const prefix = (env.SVG_PREFIX || 'monitor-svg').replace(/^\/+|\/+$/g, '') + '/';
    if (!key.startsWith(prefix) || !/^[a-f0-9]{32}\/\d{13}-[a-f0-9-]{36}\/artwork\.html$/.test(key.slice(prefix.length))) {
      return new Response('Not found', { status: 404 });
    }
    // Object paths are immutable. Query strings must not fragment this cache.
    url.search = '';
    const cacheKey = new Request(url.toString(), { method: 'GET' });
    const cached = await caches.default.match(cacheKey);
    if (cached) {
      const headers = new Headers(cached.headers);
      headers.set('X-Preview-Cache', 'HIT');
      return new Response(request.method === 'HEAD' ? null : cached.body, { headers });
    }
    const object = request.method === 'HEAD' ? await env.ARTWORKS.head(key) : await env.ARTWORKS.get(key);
    if (!object) return new Response('Not found', { status: 404, headers: { 'Cache-Control': 'no-store' } });
    const headers = new Headers();
    headers.set('Content-Type', 'text/html; charset=utf-8');
    headers.set('Cache-Control', 'public, max-age=300, s-maxage=3600');
    headers.set('ETag', object.httpEtag);
    headers.set('Content-Security-Policy', 'sandbox allow-scripts');
    headers.set('X-Content-Type-Options', 'nosniff');
    headers.set('Referrer-Policy', 'no-referrer');
    headers.set('X-Preview-Cache', 'MISS');
    const response = new Response(request.method === 'HEAD' ? null : object.body, { headers });
    if (request.method === 'GET') ctx.waitUntil(caches.default.put(cacheKey, response.clone()));
    return response;
  },
};
