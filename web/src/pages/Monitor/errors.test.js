import { describe, expect, test } from 'bun:test';
import { monitorFailureMessage } from './errors';

describe('monitor failure diagnostics', () => {
  const t = (key) => key;
  test('old and new Messages truncation records explain the token limit', () => {
    expect(monitorFailureMessage('response_incomplete', t)).toBe(
      '输出达到 token 上限，测试未完成',
    );
    expect(monitorFailureMessage('response_not_completed', t)).toBe(
      '模型响应未完整结束',
    );
    expect(monitorFailureMessage('response.incomplete', t)).toBe(
      '模型响应未完整结束',
    );
    expect(monitorFailureMessage('probe_timeout', t)).toBe('模型请求超时');
  });
});
