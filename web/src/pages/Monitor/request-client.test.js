import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createMonitorClient } from './request-client.js';

function harness(
  respond = async () => ({ data: { success: true, data: [] } }),
) {
  let time = 1000;
  let lockTail = Promise.resolve();
  const values = new Map(),
    calls = [];
  const options = {
    now: () => time,
    sleep: async (ms) => {
      time += ms;
    },
    storage: {
      getItem: (key) => values.get(key),
      setItem: (key, value) => values.set(key, value),
    },
    lock: (_name, _signal, run) => {
      const result = lockTail.then(run);
      lockTail = result.catch(() => {});
      return result;
    },
    request: async (path, config) => {
      calls.push({ path, config, at: time });
      return respond(path, config);
    },
  };
  return {
    options,
    calls,
    advance: (ms) => {
      time += ms;
    },
  };
}

test('12 tabs repeatedly mounting the aggregate monitor share one page request', async () => {
  const h = harness();
  const tabs = Array.from({ length: 12 }, () => createMonitorClient(h.options));
  const readPage = (client) => [
    client.get('/api/monitor', { params: { hours: 1, models: '{}' } }),
  ];
  await Promise.all(tabs.flatMap(readPage));
  await Promise.all(tabs.flatMap(readPage));
  // A freshly constructed client simulates reload / hot replacement.
  await Promise.all(readPage(createMonitorClient(h.options)));
  assert.equal(h.calls.length, 1);
  for (let i = 1; i < h.calls.length; i++)
    assert.ok(h.calls[i].at - h.calls[i - 1].at >= 1000);
  h.advance(31_000);
  await Promise.all(tabs.flatMap(readPage));
  assert.equal(h.calls.length, 2);
  assert.equal(h.calls[0].config.skipErrorHandler, true);
});

test('account and group changes isolate cached page reads while sharing pacing', async () => {
  let responseNumber = 0;
  let userScope;
  const h = harness(async () => ({
    data: { success: true, data: { ratio: ++responseNumber } },
  }));
  const options = { ...h.options, cacheScope: () => userScope };
  const firstTab = createMonitorClient(options);
  const secondTab = createMonitorClient(options);
  const scopes = ['[null,null]', '[1,"vip"]', '[2,"svip"]', '[1,"svip"]'];
  const responses = [];
  for (const scope of scopes) {
    userScope = scope;
    responses.push(await firstTab.get('/api/monitor'));
  }
  assert.equal(h.calls.length, scopes.length);
  assert.deepEqual(
    responses.map((res) => res.data.data.ratio),
    [1, 2, 3, 4],
  );
  for (let i = 0; i < scopes.length; i++) {
    userScope = scopes[i];
    const cached = await secondTab.get('/api/monitor');
    assert.deepEqual(cached.data, responses[i].data);
    assert.equal(h.calls[i].config.cacheScope, undefined);
    if (i > 0) assert.ok(h.calls[i].at - h.calls[i - 1].at >= 1000);
  }
  assert.equal(h.calls.length, scopes.length);
});

test('account changes cancel queued reads without merging them with the new account', async () => {
  const h = harness();
  let userScope = 'guest';
  const client = createMonitorClient({
    ...h.options,
    cacheScope: () => userScope,
  });
  const previous = client.get('/api/monitor');
  const rejected = assert.rejects(previous, { name: 'AbortError' });
  userScope = 'vip';
  await Promise.all([rejected, client.get('/api/monitor')]);
  assert.equal(h.calls.length, 1);
  assert.equal(client.pauseRemaining(), 0);
});

test('account changes discard in-flight responses before caching or displaying them', async () => {
  let done;
  const h = harness(
    () =>
      new Promise((resolve) => {
        done = resolve;
      }),
  );
  let userScope = 'guest';
  const client = createMonitorClient({
    ...h.options,
    cacheScope: () => userScope,
  });
  const previous = client.get('/api/monitor');
  const rejected = assert.rejects(previous, { name: 'AbortError' });
  for (let i = 0; i < 10 && !done; i++) await Promise.resolve();
  assert.equal(h.calls.length, 1);
  userScope = 'vip';
  done({ data: { success: true, data: { ratio: 2 } } });
  await rejected;
  assert.equal(client.pauseRemaining(), 0);
  const state = JSON.parse(
    h.options.storage.getItem('new-api:monitor-reads:v1:'),
  );
  assert.deepEqual(state.entries, {});
});

test('429 clears queued demand and shares a three minute pause across tabs/reloads', async () => {
  let failing = true;
  const h = harness(async () => {
    if (failing)
      throw Object.assign(new Error('limited'), {
        response: { status: 429, headers: {} },
      });
    return { data: { success: true, data: [] } };
  });
  const clients = Array.from({ length: 12 }, () =>
    createMonitorClient(h.options),
  );
  await Promise.allSettled(
    clients.flatMap((client) => [
      client.get('/api/monitor'),
      client.get('/api/monitor', { params: { hours: 6 } }),
    ]),
  );
  assert.equal(h.calls.length, 1);
  assert.equal(clients[0].pauseRemaining(), 180_000);
  h.advance(179_000);
  await assert.rejects(createMonitorClient(h.options).get('/api/monitor'));
  assert.equal(h.calls.length, 1);
  h.advance(1001);
  failing = false;
  await clients[0].get('/api/monitor');
  assert.equal(h.calls.length, 2);
});

test('Retry-After and repeated backend outages back off instead of retrying immediately', async () => {
  const h = harness(async () => {
    throw Object.assign(new Error('limited'), {
      response: { status: 429, headers: { 'retry-after': '600' } },
    });
  });
  const client = createMonitorClient(h.options);
  await assert.rejects(client.get('/api/monitor'));
  assert.equal(client.pauseRemaining(), 600_000);
  const offline = harness(async () => {
    throw new Error('offline');
  });
  const reconnecting = createMonitorClient(offline.options);
  await assert.rejects(reconnecting.get('/api/monitor'));
  assert.equal(reconnecting.pauseRemaining(), 30_000);
  offline.advance(30_001);
  await assert.rejects(reconnecting.get('/api/monitor'));
  assert.equal(reconnecting.pauseRemaining(), 60_000);
});

test('cancelled and hidden consumers never send queued requests', async () => {
  const h = harness();
  const client = createMonitorClient(h.options);
  const controllers = Array.from({ length: 50 }, () => new AbortController());
  const promises = controllers.map((c, i) =>
    client.get('/api/monitor/record', {
      params: { group: i },
      signal: c.signal,
    }),
  );
  controllers.forEach((c) => c.abort());
  await Promise.allSettled(promises);
  assert.equal(h.calls.length, 0);
  const hidden = createMonitorClient({ ...h.options, visible: () => false });
  await assert.rejects(hidden.get('/api/monitor'), { name: 'AbortError' });
  assert.equal(h.calls.length, 0);
});

test('one subscriber cancelling does not cancel a still-mounted subscriber', async () => {
  let done;
  const h = harness(
    () =>
      new Promise((resolve) => {
        done = resolve;
      }),
  );
  const client = createMonitorClient(h.options);
  const controller = new AbortController();
  const cancelled = client.get('/api/monitor', { signal: controller.signal });
  const remaining = client.get('/api/monitor');
  const rejected = assert.rejects(cancelled, { name: 'AbortError' });
  controller.abort();
  for (let i = 0; i < 10 && !done; i++) await Promise.resolve();
  assert.equal(h.calls.length, 1);
  assert.equal(h.calls[0].config.signal.aborted, false);
  done({ data: { success: true, data: [] } });
  await Promise.all([rejected, remaining]);
});
