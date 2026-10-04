import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import worker from './worker.mjs';

const secret = 'test-secret-that-is-at-least-32-characters';
const image = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0]);
const objectKey = 'generated-images/20261003/1/request-0.png';

function environment(overrides = {}) {
  const writes = [];
  return {
    writes,
    env: {
      IMPORT_SECRET: secret,
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
    body: JSON.stringify({ source_url: 'https://images.vendor.com/image.png', object_key: objectKey, bucket: 'images', ...payload }),
  });
}

test('downloads the source directly and stores its bytes and content type', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, options) => {
    assert.equal(url, 'https://images.vendor.com/image.png');
    assert.deepEqual(options, { redirect: 'follow' });
    return new Response(image, { headers: { 'Content-Type': 'image/png' } });
  };
  try {
    const response = await worker.fetch(request(), env);
    assert.equal(response.status, 200);
    assert.equal(response.headers.get('Cache-Control'), 'no-store');
    assert.deepEqual(await response.json(), { object_key: objectKey });
    assert.equal(writes.length, 1);
    assert.equal(writes[0][0], objectKey);
    assert.deepEqual(new Uint8Array(writes[0][1]), image);
    assert.deepEqual(writes[0][2], { httpMetadata: { contentType: 'image/png' } });
  } finally { globalThis.fetch = originalFetch; }
});

test('does not apply host, IP, port, credentials, DNS or legacy configuration restrictions', async () => {
  const { env, writes } = environment({
    ALLOWED_SOURCE_HOSTS: 'unrelated.vendor.com',
    MAX_IMAGE_BYTES: '1',
    OBJECT_PREFIX: 'unrelated-prefix/',
  });
  const originalFetch = globalThis.fetch;
  const visited = [];
  globalThis.fetch = async (url, options) => {
    visited.push(url);
    assert.deepEqual(options, { redirect: 'follow' });
    return new Response(image);
  };
  const sources = [
    'http://changing-123.vendor.com/image.png',
    'https://changing-456.other-vendor.com/image.png',
    'https://changing.vendor.com:8443/image.png',
    'http://changing.vendor.com:8080/image.png',
    'http://127.0.0.1:8080/image.png',
    'http://192.168.1.2/image.png',
    'http://[::1]:8080/image.png',
    'http://localhost:8080/image.png',
    'https://username:password@changing.vendor.com/image.png',
  ];
  try {
    for (const sourceUrl of sources) {
      assert.equal((await worker.fetch(request({ source_url: sourceUrl, object_key: 'any-prefix/image.png' }), env)).status, 200);
    }
    assert.deepEqual(visited, sources);
    assert.equal(writes.length, sources.length);
    assert.equal(writes[0][0], 'any-prefix/image.png');
  } finally { globalThis.fetch = originalFetch; }
});

test('follows more than five HTTP redirects using the native fetch implementation', async () => {
  const { env, writes } = environment();
  const visited = [];
  const server = createServer((incoming, outgoing) => {
    visited.push(incoming.url);
    const step = Number(incoming.url.slice(1));
    if (step < 8) {
      outgoing.writeHead(302, { Location: `/${step + 1}` });
      outgoing.end();
    } else {
      outgoing.writeHead(200, { 'Content-Type': 'image/avif' });
      outgoing.end(image);
    }
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  try {
    const sourceUrl = `http://127.0.0.1:${server.address().port}/0`;
    assert.equal((await worker.fetch(request({ source_url: sourceUrl }), env)).status, 200);
    assert.equal(visited.length, 9);
    assert.equal(writes.length, 1);
    assert.deepEqual(new Uint8Array(writes[0][1]), image);
    assert.equal(writes[0][2].httpMetadata.contentType, 'image/avif');
  } finally {
    await new Promise((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
      server.closeAllConnections();
    });
  }
});

test('does not impose the former 32 MiB image limit', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  const bytes = new Uint8Array(33 * 1024 * 1024);
  globalThis.fetch = async () => new Response(bytes);
  try {
    assert.equal((await worker.fetch(request(), env)).status, 200);
    assert.equal(writes.length, 1);
    assert.equal(writes[0][1].byteLength, bytes.byteLength);
  } finally { globalThis.fetch = originalFetch; }
});

test('preserves any source format and handles missing content type and content length', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  const bytes = new Uint8Array([0, 1, 2, 3]);
  try {
    for (const contentType of ['image/avif', 'image/svg+xml', 'application/octet-stream', null]) {
      globalThis.fetch = async () => new Response(new ReadableStream({
        start(controller) { controller.enqueue(bytes); controller.close(); },
      }), { headers: contentType ? { 'Content-Type': contentType } : {} });
      assert.equal((await worker.fetch(request(), env)).status, 200);
      assert.deepEqual(new Uint8Array(writes.at(-1)[1]), bytes);
      assert.equal(writes.at(-1)[2].httpMetadata.contentType, contentType || 'application/octet-stream');
    }
  } finally { globalThis.fetch = originalFetch; }
});

test('does not impose the former import request size limit', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response(image);
  try {
    const sourceUrl = `https://images.vendor.com/image.png?signature=${'x'.repeat(20000)}`;
    assert.equal((await worker.fetch(request({ source_url: sourceUrl }), env)).status, 200);
    assert.equal(writes.length, 1);
  } finally { globalThis.fetch = originalFetch; }
});

test('keeps authentication, bucket binding and required request fields', async () => {
  const { env, writes } = environment();
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => { throw new Error('must not fetch'); };
  try {
    assert.equal((await worker.fetch(request({}, 'wrong'), env)).status, 401);
    assert.equal((await worker.fetch(request(), { ...env, IMPORT_SECRET: '' })).status, 401);
    assert.equal((await worker.fetch(request(), { ...env, IMAGES: undefined })).status, 503);
    assert.equal((await worker.fetch(request(), { ...env, R2_BUCKET_NAME: undefined })).status, 503);
    for (const payload of [
      { source_url: null }, { source_url: '' }, { source_url: ' ' },
      { object_key: null }, { object_key: '' }, { bucket: 'other-bucket' },
    ]) {
      assert.equal((await worker.fetch(request(payload), env)).status, 400);
    }
    assert.equal(writes.length, 0);
  } finally { globalThis.fetch = originalFetch; }
});

test('keeps safe failure diagnostics without exposing source URLs or exception messages', async () => {
  const originalFetch = globalThis.fetch;
  const originalError = console.error;
  const logs = [];
  const sensitive = `https://private.vendor.com/image.png?token=${secret}`;
  console.error = (...args) => logs.push(args);
  try {
    for (const scenario of [
      {
        incoming: new Request('https://worker.example.com/import', {
          method: 'POST', headers: { Authorization: `Bearer ${secret}` }, body: '{invalid',
        }),
        expected: { stage: 'request_parse', reason: 'invalid_request_json' },
      },
      {
        fetch: async () => { throw new TypeError(sensitive); },
        expected: { stage: 'source_download', reason: 'source_fetch_failed' },
      },
      {
        fetch: async () => new Response(sensitive, { status: 403 }),
        expected: { stage: 'source_download', reason: 'source_http_error', upstream_status: 403 },
      },
      {
        fetch: async () => new Response(new ReadableStream({ start(controller) { controller.error(new Error(sensitive)); } })),
        expected: { stage: 'image_read', reason: 'image_read_failed' },
      },
      {
        overrides: { IMAGES: { async put() { throw new Error(sensitive); } } },
        expected: { stage: 'r2_upload', reason: 'r2_put_failed' },
      },
    ]) {
      const { env } = environment(scenario.overrides);
      globalThis.fetch = scenario.fetch || (async () => new Response(image));
      const response = await worker.fetch(scenario.incoming || request({ source_url: sensitive }), env);
      assert.equal(response.status, 502);
      assert.equal(response.headers.get('Cache-Control'), 'no-store');
      const details = { error: 'image_import_failed', ...scenario.expected };
      assert.deepEqual(await response.json(), details);
      assert.deepEqual(logs.at(-1), ['[image r2] import failed', details]);
    }
    const serialized = JSON.stringify(logs);
    assert.ok(!serialized.includes(secret));
    assert.ok(!serialized.includes('private.vendor.com'));
  } finally {
    globalThis.fetch = originalFetch;
    console.error = originalError;
  }
});

test('returns a Promise of Response for route and authentication exits', async () => {
  const { env } = environment();
  for (const [incoming, expectedStatus] of [
    [new Request('https://worker.example.com/other'), 404],
    [new Request('https://worker.example.com/import'), 405],
    [new Request('https://worker.example.com/import', { method: 'POST' }), 401],
  ]) {
    const pending = worker.fetch(incoming, env);
    assert.ok(pending instanceof Promise);
    const response = await pending;
    assert.ok(response instanceof Response);
    assert.equal(response.status, expectedStatus);
  }
});
