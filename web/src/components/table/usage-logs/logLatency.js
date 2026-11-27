function normalizeLatency(value, multiplier = 1) {
  if (
    (typeof value !== 'number' && typeof value !== 'string') ||
    (typeof value === 'string' && value.trim() === '')
  ) {
    return null;
  }
  const milliseconds = Number(value) * multiplier;
  return Number.isFinite(milliseconds) && milliseconds >= 0
    ? milliseconds
    : null;
}

export function getLogLatency(record, other) {
  const measuredDuration = normalizeLatency(other?.duration_ms);
  const outputTokens = record.completion_tokens;
  const hasRate =
    other?.output_tps_eligible === true &&
    measuredDuration > 0 &&
    Number.isFinite(outputTokens) &&
    outputTokens > 0;
  return {
    firstToken: record.is_stream ? normalizeLatency(other?.frt) : null,
    duration: measuredDuration ?? normalizeLatency(record.use_time, 1000),
    outputRate: hasRate
      ? `${((outputTokens * 1000) / measuredDuration).toFixed(1)} tok/s`
      : '-',
  };
}
