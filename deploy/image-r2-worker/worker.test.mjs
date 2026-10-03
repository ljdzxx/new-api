import { test } from 'node:test';
import assert from 'node:assert/strict';
import worker from './worker.mjs';

const secret = 'test-secret-that-is-at-least-32-characters';
const image = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0]);
const objectKey = 'generated-images/20261003/1/request-0.png';

function mockImageFetch(handler, records = { A: ['93.184.216.34'], AAAA: [] }) {
  return async (value, options) => {
    const url = new URL(value);
    if (url.hostname === 'cloudflare-dns.com') {
      assert.equal(options.headers.Accept, 'application/dns-json');
      assert.equal(options.redirect, 'error');
      const type = url.searchParams.get('type');
      return Response.json({ Status: 0, Answer: records[type].map((data) => ({ type: type === 'A' ? 1 : 28, data })) });
    }
    return handler(value, options);
  };
}

function environment(overrides = {}) {
  const writes = [];
  return {
    writes,
    env: {
      IMPORT_SECRET: secret,
      ALLOWED_SOURCE_HOSTS: 'images.example.com,cdn.example.com',
      R2_BUCKET_NAME: 'images',
      IMAGES: { async put(...args) { writes.push(args); } },
      ...overrides,
    },
  };
}

function request(payload = {}, auth = secret) {
  return new Request('https://worker.example.com/import', {
    method: 'POST',
    headers: { Authorization: `Bearer ${auth}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ source_url: 'https://images.example.com/image.png', object_key: objectKey, bucket: 'images', ...payload }),
  });
}

test('fetch returns a Promise of Response for early route and authentication exits', async () => {
  const { env } = environment();
  for (const [incoming, expectedStatus] of [
    [new Request('https://worker.example.com/other'), 404],
    [new Request('https://worker.example.com/import'), 405],
    [new Request('https://worker.example.com/import', { method: 'POST' }), 401],
    [request({}, 'wrong'), 401],
  ]) {
    const pending = worker.fetch(incoming, env);
    assert.ok(pending instanceof Promise);
    const response = await pending;
    assert.ok(response instanceof Response);
    assert.equal(response.status, expectedStatus);
  }
});

test('downloads a URL into the configured bucket with the actual content type', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  globalThis.fetch = mockImageFetch(async (url, options) => {
    assert.equal(url, 'https://images.example.com/image.png');
    assert.equal(options.redirect, 'manual');
    return new Response(image, { headers: { 'Content-Type': 'application/octet-stream' } });
  });
  try {
    const pending = worker.fetch(request(), env);
    assert.ok(pending instanceof Promise);
    const response = await pending;
    assert.equal(response.status, 200);
    assert.deepEqual(await response.json(), { object_key: objectKey });
    assert.equal(writes.length, 1);
    assert.equal(writes[0][0], objectKey);
    assert.deepEqual(writes[0][1], image);
    assert.equal(writes[0][2].httpMetadata.contentType, 'image/png');
  } finally { globalThis.fetch = originalFetch; }
});

test('rejects unauthenticated requests, unconfigured buckets and invalid destinations', async () => {
  const { env, writes } = environment();
  assert.equal((await worker.fetch(request({}, 'wrong'), env)).status, 401);
  assert.equal((await worker.fetch(request(), { ...env, IMAGES: undefined })).status, 503);
  for (const payload of [
    { object_key: 'other-prefix/file.png' },
    { object_key: 'generated-images/../private.png' },
    { object_key: 'generated-images//file.png' },
    { bucket: 'other-bucket' },
  ]) {
    assert.equal((await worker.fetch(request(payload), env)).status, 400);
  }
  assert.equal(writes.length, 0);
});

test('blocks disallowed source URLs before making a network request', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => { calls++; throw new Error('must not fetch'); };
  try {
    for (const sourceUrl of [
      'https://evil.example.com/image.png',
      'ftp://images.example.com/image.png',
      'https://images.example.com:8443/image.png',
      'https://user:password@images.example.com/image.png',
      'https://127.0.0.1/image.png',
      'https://images.example.com.evil.example.com/image.png',
    ]) {
      assert.equal((await worker.fetch(request({ source_url: sourceUrl }), env)).status, 502);
    }
    assert.equal(calls, 0);
    assert.equal(writes.length, 0);
  } finally { globalThis.fetch = originalFetch; }
});

test('validates every redirect and accepts redirects only to approved hosts', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = mockImageFetch(async () => {
    calls++;
    return new Response(null, { status: 302, headers: { Location: 'https://evil.example.com/image.png' } });
  });
  try {
    assert.equal((await worker.fetch(request(), env)).status, 502);
    assert.equal(calls, 1);
    calls = 0;
    globalThis.fetch = mockImageFetch(async (url) => {
      calls++;
      return url.includes('images.example.com')
        ? new Response(null, { status: 302, headers: { Location: 'https://cdn.example.com/image.png' } })
        : new Response(image);
    });
    assert.equal((await worker.fetch(request(), env)).status, 200);
    assert.equal(calls, 2);
    assert.equal(writes.length, 1);
  } finally { globalThis.fetch = originalFetch; }
});

test('rejects oversized images with and without Content-Length and non-image payloads', async () => {
  const { env, writes } = environment({ MAX_IMAGE_BYTES: '8' });
  const originalFetch = globalThis.fetch;
  try {
    for (const headers of [{}, { 'Content-Length': '100' }]) {
      globalThis.fetch = mockImageFetch(async () => new Response(image, { headers }));
      assert.equal((await worker.fetch(request(), env)).status, 502);
    }
    globalThis.fetch = mockImageFetch(async () => new Response('<html>not an image</html>', { headers: { 'Content-Type': 'image/png' } }));
    assert.equal((await worker.fetch(request(), { ...env, MAX_IMAGE_BYTES: '1024' })).status, 502);
    assert.equal(writes.length, 0);
  } finally { globalThis.fetch = originalFetch; }
});

test('reports storage failures and limits import request size', async () => {
  const { env } = environment();
  const originalFetch = globalThis.fetch;
  globalThis.fetch = mockImageFetch(async () => new Response(image));
  try {
    env.IMAGES.put = async () => { throw new Error('R2 failed'); };
    assert.equal((await worker.fetch(request(), env)).status, 502);
    assert.equal((await worker.fetch(request({ padding: 'x'.repeat(17000) }), env)).status, 413);
    assert.equal((await worker.fetch(new Request('https://worker.example.com/import'), env)).status, 405);
    assert.equal((await worker.fetch(new Request('https://worker.example.com/other'), env)).status, 404);
  } finally { globalThis.fetch = originalFetch; }
});

test('reports safe failure stages and reasons without logging source URLs or exception messages', async () => {
  const originalFetch = globalThis.fetch;
  const originalError = console.error;
  const logs = [];
  const sensitive = `https://sensitive.vendor.com/private.png?token=${secret}`;
  console.error = (...args) => logs.push(args);
  try {
    for (const scenario of [
      {
        payload: { source_url: 'not a URL' },
        expected: { stage: 'source_validation', reason: 'invalid_source_url' },
      },
      {
        payload: { source_url: sensitive },
        expected: { stage: 'source_validation', reason: 'source_host_not_allowed' },
      },
      {
        fetch: async () => new Response('forbidden', { status: 403 }),
        expected: { stage: 'dns_lookup', reason: 'dns_http_error', upstream_status: 403 },
      },
      {
        fetch: async () => Response.json({ Status: 3 }),
        expected: { stage: 'dns_lookup', reason: 'dns_lookup_failed' },
      },
      {
        fetch: async () => new Response('<html>not DNS JSON</html>'),
        expected: { stage: 'dns_lookup', reason: 'invalid_dns_response' },
      },
      {
        fetch: mockImageFetch(async () => new Response(image), { A: ['10.0.0.1'], AAAA: [] }),
        expected: { stage: 'dns_lookup', reason: 'non_public_dns' },
      },
      {
        fetch: async () => { throw new TypeError(sensitive); },
        expected: { stage: 'dns_lookup', reason: 'dns_fetch_failed' },
      },
      {
        fetch: mockImageFetch(async () => { throw new TypeError(sensitive); }),
        expected: { stage: 'source_download', reason: 'source_fetch_failed' },
      },
      {
        fetch: mockImageFetch(async () => new Response('forbidden', { status: 403 })),
        expected: { stage: 'source_download', reason: 'source_http_error', upstream_status: 403 },
      },
      {
        fetch: mockImageFetch(async () => new Response(null, { status: 302 })),
        expected: { stage: 'redirect_validation', reason: 'invalid_redirect' },
      },
      {
        fetch: mockImageFetch(async () => new Response(null, { status: 302, headers: { Location: sensitive } })),
        expected: { stage: 'redirect_validation', reason: 'source_host_not_allowed' },
      },
      {
        fetch: mockImageFetch(async () => new Response(image)),
        overrides: { MAX_IMAGE_BYTES: '8' },
        expected: { stage: 'image_read', reason: 'image_too_large' },
      },
      {
        fetch: mockImageFetch(async () => new Response('<html>not an image</html>')),
        expected: { stage: 'image_validation', reason: 'unsupported_image' },
      },
      {
        overrides: { IMAGES: { async put() { throw new Error(sensitive); } } },
        expected: { stage: 'r2_upload', reason: 'r2_put_failed' },
      },
      {
        incoming: new Request('https://worker.example.com/import', {
          method: 'POST', headers: { Authorization: `Bearer ${secret}` }, body: '{invalid',
        }),
        expected: { stage: 'request_parse', reason: 'invalid_request_json' },
      },
    ]) {
      const { env } = environment(scenario.overrides);
      globalThis.fetch = scenario.fetch || mockImageFetch(async () => new Response(image));
      const response = await worker.fetch(scenario.incoming || request(scenario.payload), env);
      assert.equal(response.status, 502);
      assert.equal(response.headers.get('Cache-Control'), 'no-store');
      const details = { error: 'image_import_failed', ...scenario.expected };
      assert.deepEqual(await response.json(), details);
      assert.deepEqual(logs.at(-1), ['[image r2] import failed', details]);
    }
    const serialized = JSON.stringify(logs);
    assert.ok(!serialized.includes(secret));
    assert.ok(!serialized.includes('sensitive.vendor.com'));
    assert.ok(!serialized.includes('private.png'));
  } finally {
    globalThis.fetch = originalFetch;
    console.error = originalError;
  }
});

test('reports download timeouts safely and keeps the failure response at 502', async () => {
  const { env } = environment();
  const originalFetch = globalThis.fetch;
  const originalSetTimeout = globalThis.setTimeout;
  const originalError = console.error;
  globalThis.setTimeout = (callback, milliseconds) => {
    assert.equal(milliseconds, 120000);
    return originalSetTimeout(callback, 0);
  };
  console.error = () => {};
  globalThis.fetch = mockImageFetch(async (url, { signal }) => {
    await new Promise((resolve, reject) => {
      signal.addEventListener('abort', () => reject(new DOMException('request aborted', 'AbortError')), { once: true });
    });
  });
  try {
    const response = await worker.fetch(request(), env);
    assert.equal(response.status, 502);
    assert.deepEqual(await response.json(), {
      error: 'image_import_failed', stage: 'source_download', reason: 'source_timeout',
    });
  } finally {
    globalThis.fetch = originalFetch;
    globalThis.setTimeout = originalSetTimeout;
    console.error = originalError;
  }
});

test('empty or omitted host list accepts changing public hosts and cross-host redirects', async () => {
  const originalFetch = globalThis.fetch;
  try {
    for (const hosts of ['', undefined]) {
      const { env, writes } = environment({ ALLOWED_SOURCE_HOSTS: hosts });
      const visited = [];
      globalThis.fetch = mockImageFetch(async (url) => {
        visited.push(new URL(url).hostname);
        return visited.length === 1
          ? new Response(null, { status: 302, headers: { Location: 'https://random-823.other-cdn.net/image.png' } })
          : new Response(image);
      });
      assert.equal((await worker.fetch(request({ source_url: 'https://dynamic-591.vendor-storage.com/image.png' }), env)).status, 200);
      assert.deepEqual(visited, ['dynamic-591.vendor-storage.com', 'random-823.other-cdn.net']);
      assert.equal(writes.length, 1);
    }
  } finally { globalThis.fetch = originalFetch; }
});

test('dynamic mode rejects local names, IP literals, credentials and unsupported protocols before fetching', async () => {
  const { env, writes } = environment({ ALLOWED_SOURCE_HOSTS: '' });
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => { calls++; throw new Error('must not fetch'); };
  try {
    for (const sourceUrl of [
      'https://localhost/image.png', 'https://host.local/image.png',
      'https://host.internal/image.png', 'https://host.lan/image.png',
      'https://127.0.0.1/image.png', 'https://2130706433/image.png',
      'https://0x7f000001/image.png', 'https://93.184.216.34/image.png',
      'https://[::1]/image.png', 'https://[::ffff:127.0.0.1]/image.png',
      'ftp://dynamic.vendor.com/image.png', 'https://dynamic.vendor.com:8443/image.png',
      'http://dynamic.vendor.com:8080/image.png',
      'https://username:password@dynamic.vendor.com/image.png',
    ]) {
      assert.equal((await worker.fetch(request({ source_url: sourceUrl }), env)).status, 502, sourceUrl);
    }
    assert.equal(calls, 0);
    assert.equal(writes.length, 0);
  } finally { globalThis.fetch = originalFetch; }
});

test('accepts HTTP and HTTPS sources and redirects between the two protocols', async () => {
  const originalFetch = globalThis.fetch;
  try {
    for (const [sourceUrl, redirectUrl] of [
      ['http://dynamic.vendor.com/image.png', null],
      ['http://dynamic.vendor.com:80/image.png', 'https://dynamic.other-cdn.com:443/image.png'],
      ['https://dynamic.vendor.com/image.png', 'http://dynamic.other-cdn.com/image.png'],
    ]) {
      const { env, writes } = environment({ ALLOWED_SOURCE_HOSTS: '' });
      const visited = [];
      globalThis.fetch = mockImageFetch(async (url) => {
        visited.push(url);
        return redirectUrl && visited.length === 1
          ? new Response(null, { status: 302, headers: { Location: redirectUrl } })
          : new Response(image);
      });
      assert.equal((await worker.fetch(request({ source_url: sourceUrl }), env)).status, 200);
      assert.equal(visited.length, redirectUrl ? 2 : 1);
      assert.equal(writes.length, 1);
    }
  } finally { globalThis.fetch = originalFetch; }
});

test('DNS checks reject private, reserved and mixed public/private IPv4 and IPv6 answers', async () => {
  const { env, writes } = environment({ ALLOWED_SOURCE_HOSTS: '' });
  const originalFetch = globalThis.fetch;
  let downloads = 0;
  try {
    for (const records of [
      { A: ['10.0.0.1'], AAAA: [] }, { A: ['127.0.0.1'], AAAA: [] },
      { A: ['169.254.169.254'], AAAA: [] }, { A: ['172.16.0.1'], AAAA: [] },
      { A: ['192.168.0.1'], AAAA: [] }, { A: ['100.64.0.1'], AAAA: [] },
      { A: ['0.0.0.0'], AAAA: [] }, { A: ['224.0.0.1'], AAAA: [] },
      { A: ['198.18.0.1'], AAAA: [] }, { A: ['192.0.2.1'], AAAA: [] },
      { A: ['93.184.216.34', '10.0.0.1'], AAAA: [] },
      { A: ['93.184.216.34'], AAAA: ['::1'] },
      { A: [], AAAA: ['fc00::1'] }, { A: [], AAAA: ['fe80::1'] },
      { A: [], AAAA: ['::ffff:7f00:1'] }, { A: [], AAAA: ['::ffff:127.0.0.1'] },
      { A: [], AAAA: ['64:ff9b::7f00:1'] }, { A: [], AAAA: ['2002:7f00:1::'] },
      { A: [], AAAA: ['2001:db8::1'] }, { A: [], AAAA: ['3fff::1'] },
      { A: [], AAAA: [] },
    ]) {
      globalThis.fetch = mockImageFetch(async () => { downloads++; return new Response(image); }, records);
      assert.equal((await worker.fetch(request(), env)).status, 502, JSON.stringify(records));
    }
    assert.equal(downloads, 0);
    assert.equal(writes.length, 0);
    globalThis.fetch = mockImageFetch(async () => new Response(image), { A: [], AAAA: ['2606:4700:4700::1111'] });
    assert.equal((await worker.fetch(request(), env)).status, 200);
  } finally { globalThis.fetch = originalFetch; }
});

test('rechecks DNS for redirect destinations and fails closed when DNS lookup fails', async () => {
  const { env, writes } = environment({ ALLOWED_SOURCE_HOSTS: '' });
  const originalFetch = globalThis.fetch;
  let downloads = 0;
  globalThis.fetch = async (value) => {
    const url = new URL(value);
    if (url.hostname === 'cloudflare-dns.com') {
      const destination = url.searchParams.get('name') === 'private-target.vendor.com';
      const answer = url.searchParams.get('type') === 'A' ? [{ type: 1, data: destination ? '10.0.0.1' : '93.184.216.34' }] : [];
      return Response.json({ Status: 0, Answer: answer });
    }
    downloads++;
    return new Response(null, { status: 302, headers: { Location: 'https://private-target.vendor.com/image.png' } });
  };
  try {
    assert.equal((await worker.fetch(request(), env)).status, 502);
    assert.equal(downloads, 1);
    for (const dnsResponse of [
      () => new Response('unavailable', { status: 503 }),
      () => Response.json({ Status: 3 }),
      () => Response.json({ Status: 0, TC: true, Answer: [{ type: 1, data: '93.184.216.34' }] }),
    ]) {
      globalThis.fetch = async () => dnsResponse();
      assert.equal((await worker.fetch(request(), env)).status, 502);
    }
    assert.equal(writes.length, 0);
  } finally { globalThis.fetch = originalFetch; }
});
