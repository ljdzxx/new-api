import { describe, expect, test } from 'bun:test';
import {
  defaultAdvancedCustomConfig,
  writeAdvancedCustomSettings,
} from './advancedCustomSettings';

describe('advanced custom channel settings', () => {
  test('new channels receive both Gemini routes and bearer auth', () => {
    const saved = writeAdvancedCustomSettings({ existing: true }, { type: 59 });
    expect(saved.existing).toBe(true);
    expect(saved.advanced_custom.advanced_routes).toHaveLength(2);
    expect(saved.advanced_custom.advanced_routes[0].auth.value).toBe(
      'Bearer {key}',
    );
  });
  test('editing keeps the custom target and auth, switching type clears only routes', () => {
    const config = defaultAdvancedCustomConfig();
    config.advanced_routes[0].upstream_path = '/gateway/responses';
    config.advanced_routes[0].auth = {
      type: 'query',
      name: 'key',
      value: '{key}',
    };
    const saved = writeAdvancedCustomSettings(
      { existing: true },
      { type: 59, advanced_custom: config, tool_loss_policy: 'safe' },
    );
    expect(saved.advanced_custom).toEqual(config);
    expect(saved.tool_loss_policy).toBe('safe');
    const switched = writeAdvancedCustomSettings(saved, { type: 58 });
    expect(switched.advanced_custom).toBeUndefined();
    expect(switched.existing).toBe(true);
    expect(saved.advanced_custom).toEqual(config);
  });
});
