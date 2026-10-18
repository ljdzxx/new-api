/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import { expect, test } from 'bun:test';
import { buildCCSwitchURL, getCCSwitchName } from './ccSwitch';

function importParams(app, serverAddress = 'https://api.example.test') {
  return new URL(
    buildCCSwitchURL({
      app,
      name: getCCSwitchName('我的站点', '开发 & 测试'),
      models: { model: 'custom-model', haikuModel: '', sonnetModel: undefined },
      apiKey: 'sk-test-key',
      serverAddress,
    }),
  ).searchParams;
}

function usageConfig(params) {
  const code = Buffer.from(params.get('usageScript'), 'base64')
    .toString('utf8')
    .replaceAll('{{baseUrl}}', params.get('endpoint'))
    .replaceAll('{{apiKey}}', params.get('apiKey'));
  // Execute the generated, static script exactly as the importing client does.
  return new Function(`return ${code};`)();
}

test('all three applications preserve Unicode and import enabled 30-minute usage queries', () => {
  for (const app of ['claude', 'codex', 'gemini']) {
    for (const suffix of ['', '/', '/v1', '/v1/']) {
      const params = importParams(
        app,
        `https://api.example.test/gateway${suffix}`,
      );
      expect(params.get('name')).toBe('我的站点-开发 & 测试');
      expect(params.get('app')).toBe(app);
      expect(params.get('endpoint')).toBe(
        `https://api.example.test/gateway${app === 'codex' ? '/v1' : ''}`,
      );
      expect(params.get('usageEnabled')).toBe('true');
      expect(params.get('usageAutoInterval')).toBe('30');
      expect(params.has('haikuModel')).toBe(false);
      expect(params.has('sonnetModel')).toBe(false);
      expect(params.has('usageAccessToken')).toBe(false);
      expect(params.has('usageUserId')).toBe(false);
      const config = usageConfig(params);
      expect(config.request).toEqual({
        url: 'https://api.example.test/gateway/v1/usage',
        method: 'GET',
        headers: { Authorization: 'Bearer sk-test-key' },
      });
    }
  }
});

test('Claude imports custom names for each model slot without family filtering', () => {
  const models = {
    model: 'main-alias',
    haikuModel: 'fast-alias',
    sonnetModel: 'balanced-alias',
    opusModel: 'powerful-alias',
  };
  const params = new URL(
    buildCCSwitchURL({
      app: 'claude',
      name: '站点-令牌',
      models,
      apiKey: 'sk-test',
      serverAddress: 'https://api.example.test',
    }),
  ).searchParams;
  for (const [field, name] of Object.entries(models)) {
    expect(params.get(field)).toBe(name);
  }
});

test('usage extractor handles finite, unlimited, missing and invalid balances', () => {
  const { extractor } = usageConfig(importParams('codex'));
  expect(
    extractor({ isValid: true, remaining: 7, quota: { limit: 10, used: 3 } }),
  ).toEqual({
    isValid: true,
    planName: '令牌额度',
    unit: 'USD',
    remaining: 7,
    total: 10,
    used: 3,
  });
  expect(extractor({ isValid: true, remaining: -1 })).toEqual({
    isValid: true,
    planName: '令牌额度',
    unit: 'USD',
    extra: '无限额度',
  });
  expect(extractor({ isValid: true })).toEqual({
    isValid: true,
    planName: '令牌额度',
    unit: 'USD',
    extra: '无可用额度来源',
  });
  expect(
    extractor({
      isValid: true,
      remaining: 0,
      planName: '钱包余额',
      unit: 'CNY',
    }),
  ).toEqual({
    isValid: true,
    planName: '钱包余额',
    unit: 'CNY',
    remaining: 0,
  });
  expect(extractor({ isValid: false, message: '令牌无效' })).toEqual({
    isValid: false,
    invalidMessage: '令牌无效',
  });
  expect(extractor({})).toEqual({ isValid: false, invalidMessage: '查询失败' });
});

test('missing system names fall back to NewApi without using the global branding default', () => {
  for (const name of [undefined, null, '', '  ', 'undefined', 'null']) {
    expect(getCCSwitchName(name, '测试令牌')).toBe('NewApi-测试令牌');
  }
  expect(getCCSwitchName(' 我的系统 ', '令牌')).toBe('我的系统-令牌');
});

test('browser Base64 encoding preserves the Chinese extractor messages', () => {
  const previousWindow = globalThis.window;
  try {
    globalThis.window = { btoa: globalThis.btoa };
    const { extractor } = usageConfig(importParams('claude'));
    expect(extractor({ isValid: true, remaining: -1 }).extra).toBe('无限额度');
  } finally {
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
  }
});
