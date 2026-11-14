import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import vm from 'node:vm';

const source = await readFile(new URL('./index.js', import.meta.url), 'utf8');
const env = { BACKEND_HOST: 'origin.example.test', BACKEND_PROTO: 'https', BACKEND_PORT: '8443' };

function loadWorker(fetch) {
  const context = vm.createContext({ URL, Request, Response, Headers, fetch });
  vm.runInContext(source.replace('export default', 'globalThis.worker ='), context, { filename: 'index.js' });
  return context.worker;
}

for (const path of ['/v1/responses', '/v1/realtime']) {
  test('preserves the upstream WebSocket response for ' + path, async () => {
    // Node does not implement Workers' Response.webSocket. Use a sentinel
    // response and require identity: wrapping it would drop the live socket.
    const socket = {};
    const upstreamResponse = {
      status: 101, statusText: 'Switching Protocols', body: null,
      headers: new Headers({ Upgrade: 'websocket', Connection: 'Upgrade' }),
      webSocket: socket,
    };
    let sent;
    const worker = loadWorker(async (request) => { sent = request; return upstreamResponse; });
    const response = await worker.fetch(new Request('https://api.example.test' + path + '?model=test-model', {
      headers: {
        Upgrade: 'websocket', Connection: 'Upgrade', Authorization: 'Bearer test-only',
        'OpenAI-Beta': 'test-beta', 'Sec-WebSocket-Protocol': 'test-protocol',
      },
    }), env, {});
    assert.equal(response, upstreamResponse);
    assert.equal(response.webSocket, socket);
    assert.equal(sent.url, 'https://origin.example.test:8443' + path + '?model=test-model');
    assert.equal(sent.method, 'GET');
    assert.equal(sent.headers.get('Upgrade'), 'websocket');
    assert.equal(sent.headers.get('Connection'), 'Upgrade');
    assert.equal(sent.headers.get('Authorization'), 'Bearer test-only');
    assert.equal(sent.headers.get('OpenAI-Beta'), 'test-beta');
    assert.equal(sent.headers.get('Sec-WebSocket-Protocol'), 'test-protocol');
    assert.equal(sent.headers.get('X-Forwarded-Host'), 'api.example.test');
    assert.equal(sent.headers.get('X-Forwarded-Proto'), 'https');
    assert.equal(sent.redirect, 'manual');
  });
}

test('preserves POST body and streaming response without buffering', async () => {
  const payload = '{"model":"test-model","stream":true}';
  const firstEvent = 'data: {"type":"response.created"}\n\n';
  const lastEvent = 'data: {"type":"response.completed"}\n\n';
  let streamController;
  const upstreamBody = new ReadableStream({ start(controller) { streamController = controller; } });
  let sent;
  const worker = loadWorker(async (request) => {
    sent = request;
    return new Response(upstreamBody, { headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'public' } });
  });
  const response = await worker.fetch(new Request('https://api.example.test/v1/responses?http=1', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: payload,
  }), env, {});
  assert.equal(response.status, 200);
  assert.equal(response.body, upstreamBody);
  assert.equal(response.headers.get('Cache-Control'), 'no-store, no-cache, must-revalidate');
  assert.equal(response.headers.get('Content-Type'), 'text/event-stream');
  assert.equal(sent.method, 'POST');
  assert.equal(await sent.text(), payload);
  assert.equal(sent.url, 'https://origin.example.test:8443/v1/responses?http=1');
  const reader = response.body.getReader();
  streamController.enqueue(new TextEncoder().encode(firstEvent));
  assert.equal(new TextDecoder().decode((await reader.read()).value), firstEvent);
  streamController.enqueue(new TextEncoder().encode(lastEvent));
  streamController.close();
  assert.equal(new TextDecoder().decode((await reader.read()).value), lastEvent);
  assert.equal((await reader.read()).done, true);
});

test('preserves upstream handshake rejection and error body', async () => {
  const worker = loadWorker(async () => new Response('denied', {
    status: 401, statusText: 'Unauthorized', headers: { 'WWW-Authenticate': 'Bearer' },
  }));
  const response = await worker.fetch(new Request('https://api.example.test/v1/responses', {
    headers: { Upgrade: 'websocket' },
  }), env, {});
  assert.equal(response.status, 401);
  assert.equal(response.statusText, 'Unauthorized');
  assert.equal(response.headers.get('WWW-Authenticate'), 'Bearer');
  assert.equal(response.headers.get('Cache-Control'), 'no-store, no-cache, must-revalidate');
  assert.equal(await response.text(), 'denied');
});
