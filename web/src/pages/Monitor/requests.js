import { API } from '../../helpers';
import { createMonitorClient } from './request-client';

let storage;
try {
  storage = window.localStorage;
} catch {
  /* memory fallback */
}

export const monitorRequests = createMonitorClient({
  request: (path, config) => API.get(path, config),
  storage,
  scope: API.defaults.baseURL || window.location.origin,
  cacheScope: (path) => {
    if (path !== '/api/monitor') return '';
    try {
      const user = JSON.parse(storage?.getItem('user') || 'null');
      return JSON.stringify([user?.id ?? null, user?.group ?? null]);
    } catch {
      return '';
    }
  },
  visible: () => document.visibilityState !== 'hidden',
  lock: navigator.locks
    ? (name, signal, run) => navigator.locks.request(name, { signal }, run)
    : undefined,
});

if (import.meta.hot) import.meta.hot.dispose(() => monitorRequests.dispose());
