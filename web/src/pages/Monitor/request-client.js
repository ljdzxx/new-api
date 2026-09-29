const CACHE_MS = 30_000;
const RATE_LIMIT_PAUSE_MS = 180_000;
const STORAGE_PREFIX = 'new-api:monitor-reads:v1:';

export const monitorCancelled = () =>
  Object.assign(new Error('Monitor read cancelled'), { name: 'AbortError' });

// One paced queue for every monitor read. Web Locks + a short-lived public-data
// cache share this budget across tabs, remounts and development hot reloads.
export function createMonitorClient({
  request,
  storage,
  lock,
  scope = '',
  now = Date.now,
  sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
  visible = () => true,
}) {
  const storageKey = STORAGE_PREFIX + scope;
  const pending = new Map();
  let local = { entries: {}, nextStart: 0, blockedUntil: 0, failures: 0 };
  let tail = Promise.resolve();
  const readState = () => {
    try {
      return JSON.parse(storage?.getItem(storageKey)) || local;
    } catch {
      return local;
    }
  };
  const saveState = (state) => {
    // Cache only background public summaries, never record bodies or credentials.
    state.entries = Object.fromEntries(
      Object.entries(state.entries)
        .filter(([, entry]) => entry.until > now())
        .slice(-60),
    );
    local = state;
    try {
      storage?.setItem(storageKey, JSON.stringify(state));
    } catch {
      // Still share pacing/backoff if the browser's storage quota is full.
      state.entries = {};
      try {
        storage?.setItem(storageKey, JSON.stringify(state));
      } catch {
        /* memory fallback */
      }
    }
  };
  const execute = async (path, params, key, controller) => {
    if (controller.signal.aborted || !visible()) throw monitorCancelled();
    let state = readState();
    if (state.entries[key]?.until > now())
      return { data: state.entries[key].data };
    if (state.blockedUntil > now()) {
      throw Object.assign(new Error('Monitor polling paused'), {
        monitorPaused: true,
      });
    }
    if (state.nextStart > now()) await sleep(state.nextStart - now());
    if (controller.signal.aborted || !visible()) throw monitorCancelled();
    state.nextStart = now() + 1000;
    saveState(state);
    try {
      const response = await request(path, {
        params,
        signal: controller.signal,
        timeout: 20_000,
        skipErrorHandler: true,
        disableDuplicate: true,
      });
      if (!response.data?.success)
        throw new Error(response.data?.message || 'Monitor read failed');
      state = readState();
      state.failures = 0;
      if (
        path !== '/api/monitor/record' &&
        JSON.stringify(response.data).length <= 128_000
      ) {
        state.entries[key] = { data: response.data, until: now() + CACHE_MS };
      }
      saveState(state);
      return response;
    } catch (error) {
      if (controller.signal.aborted) throw monitorCancelled();
      state = readState();
      state.failures = Math.min((state.failures || 0) + 1, 5);
      const retry = error.response?.headers?.['retry-after'];
      const seconds = Number(retry);
      const retryMS =
        retry && Number.isFinite(seconds)
          ? seconds * 1000
          : Date.parse(retry) - now();
      const delay =
        error.response?.status === 429
          ? Math.max(
              RATE_LIMIT_PAUSE_MS,
              Number.isFinite(retryMS) ? retryMS : 0,
            )
          : Math.min(300_000, CACHE_MS * 2 ** (state.failures - 1));
      state.blockedUntil = now() + delay;
      saveState(state);
      throw error;
    }
  };
  function get(path, { params = {}, signal } = {}) {
    if (signal?.aborted || !visible())
      return Promise.reject(monitorCancelled());
    const key = path + '?' + JSON.stringify(Object.entries(params).sort());
    let entry = pending.get(key);
    if (!entry || entry.controller.signal.aborted) {
      const controller = new AbortController();
      entry = { controller, users: 0 };
      const task = () => {
        const run = () => execute(path, params, key, controller);
        return lock ? lock(storageKey, controller.signal, run) : run();
      };
      entry.promise = tail.then(task);
      tail = entry.promise.catch(() => {});
      pending.set(key, entry);
      entry.promise
        .finally(() => {
          if (pending.get(key) === entry) pending.delete(key);
        })
        .catch(() => {});
    }
    entry.users++;
    return new Promise((resolve, reject) => {
      let done = false;
      const finish = (error, value) => {
        if (done) return;
        done = true;
        signal?.removeEventListener('abort', cancel);
        entry.users--;
        if (!entry.users) entry.controller.abort();
        if (error) reject(error);
        else resolve(value);
      };
      const cancel = () => finish(monitorCancelled());
      signal?.addEventListener('abort', cancel, { once: true });
      entry.promise.then(
        (value) => finish(null, value),
        (error) => finish(error),
      );
    });
  }
  return {
    get,
    pauseRemaining: () => Math.max(0, readState().blockedUntil - now()),
    dispose: () => {
      for (const entry of pending.values()) entry.controller.abort();
    },
  };
}
