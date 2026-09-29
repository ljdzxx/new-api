import { test } from 'node:test';
import assert from 'node:assert/strict';
import worker from './worker.mjs';

test('only artwork HTML is public and cached; HEAD cannot poison GET', async () => {
  const objects = new Map();
  const cached = new Map();
  const pending = [];
  const key = 'monitor-svg/' + 'a'.repeat(32) + '/1790664060445-2dc2245a-7dcd-4e19-aab6-38d0ba9ca078/artwork.html';
  objects.set(key, '<html>preview</html>');
  let reads = 0;
  const previous = globalThis.caches;
  globalThis.caches = { default: {
    async match(request) { return cached.get(request.url)?.clone(); },
    async put(request, response) { cached.set(request.url, response); },
  } };
  const env = { ARTWORKS: {
    async get(key) { reads++; return objects.has(key) ? { body: objects.get(key), httpEtag: '"etag"' } : null; },
    async head(key) { reads++; return objects.has(key) ? { httpEtag: '"etag"' } : null; },
  } };
  const ctx = { waitUntil(promise) { pending.push(promise); } };
  const fetch = (path, method = 'GET') => worker.fetch(new Request('https://preview.example.com/' + path, { method }), env, ctx);
  try {
    for (const path of [key.replace('artwork.html', 'response.txt'), key.replace('artwork.html', 'result.json'), 'another-prefix/' + key]) {
      assert.equal((await fetch(path)).status, 404);
    }
    assert.equal(reads, 0);
    assert.equal((await fetch(key, 'PUT')).status, 405);
    const head = await fetch(key, 'HEAD');
    assert.equal(await head.text(), '');
    assert.equal(cached.size, 0);
    const first = await fetch(key + '?v=1');
    assert.equal(await first.text(), '<html>preview</html>');
    assert.equal(first.headers.get('X-Preview-Cache'), 'MISS');
    assert.equal(first.headers.get('Content-Security-Policy'), 'sandbox allow-scripts');
    await Promise.all(pending);
    const second = await fetch(key + '?v=2');
    assert.equal(await second.text(), '<html>preview</html>');
    assert.equal(second.headers.get('X-Preview-Cache'), 'HIT');
    assert.equal(reads, 2);
  } finally { globalThis.caches = previous; }
});
