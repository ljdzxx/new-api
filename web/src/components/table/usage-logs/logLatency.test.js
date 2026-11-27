import { expect, test } from 'bun:test';
import { getLogLatency } from './logLatency';

test('uses the output count displayed in the log, including billing scaling', () => {
  const record = { completion_tokens: 1000, is_stream: true, use_time: 20 };
  const other = { duration_ms: 20000, frt: 10000, output_tps_eligible: true };
  expect(getLogLatency(record, other).outputRate).toBe('50.0 tok/s');
  const scaled = { ...record, completion_tokens: 1500 };
  expect(getLogLatency(scaled, other).outputRate).toBe('75.0 tok/s');
});

test('uses the measured milliseconds even for sub-second requests', () => {
  expect(
    getLogLatency(
      { completion_tokens: 10, use_time: 0 },
      {
        duration_ms: 250,
        output_tps_eligible: true,
      },
    ),
  ).toEqual({ firstToken: null, duration: 250, outputRate: '40.0 tok/s' });
});

test('legacy seconds still display but never supply a rate', () => {
  const record = { completion_tokens: 1000, use_time: 3 };
  for (const other of [null, {}, { output_tps_eligible: true }]) {
    expect(getLogLatency(record, other)).toEqual({
      firstToken: null,
      duration: 3000,
      outputRate: '-',
    });
  }
});

test('zero duration, zero output and non-text requests have no rate', () => {
  const record = { completion_tokens: 10 };
  const other = { duration_ms: 250, output_tps_eligible: true };
  expect(getLogLatency(record, { ...other, duration_ms: 0 }).outputRate).toBe(
    '-',
  );
  expect(getLogLatency({ completion_tokens: 0 }, other).outputRate).toBe('-');
  expect(getLogLatency(record, { duration_ms: 250 }).outputRate).toBe('-');
});
