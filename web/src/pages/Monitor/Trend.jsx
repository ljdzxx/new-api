import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';

export default function Trend({ data }) {
  const { t } = useTranslation();
  const [hover, setHover] = useState(null);
  const actual = data?.actual || [],
    probe = data?.probe || [];
  const all = [...actual, ...probe];
  if (!all.length) return <div className='monitor-empty'>{t('暂无样本')}</div>;
  const from = data.from,
    span = Math.max(1, data.to - from);
  const max = Math.max(1, ...all.map((p) => p.ttft));
  const x = (p) => 8 + ((p.at - from) / span) * 384;
  const y = (p) => 66 - (p.ttft / max) * 56;
  const path = (points) =>
    points
      .map(
        (p, i) =>
          `${i === 0 || p.at - points[i - 1].at > 15 * 60000 ? 'M' : 'L'}${x(p)},${y(p)}`,
      )
      .join(' ');
  const move = (e) => {
    const box = e.currentTarget.getBoundingClientRect();
    const time = from + ((e.clientX - box.left) / box.width) * span;
    const nearest = (points) =>
      points.reduce(
        (best, p) =>
          !best || Math.abs(p.at - time) < Math.abs(best.at - time) ? p : best,
        null,
      );
    setHover({ actual: nearest(actual), probe: nearest(probe) });
  };
  return (
    <div className='monitor-trend'>
      <svg
        viewBox='0 0 400 80'
        role='img'
        aria-label={t('TTFT 趋势')}
        onMouseMove={move}
        onMouseLeave={() => setHover(null)}
      >
        {[10, 38, 66].map((v) => (
          <line
            key={v}
            x1='8'
            x2='392'
            y1={v}
            y2={v}
            stroke='currentColor'
            opacity='.1'
          />
        ))}
        <path
          d={path(probe)}
          fill='none'
          stroke='var(--monitor-accent)'
          strokeWidth='1.7'
          strokeDasharray='4 3'
        />
        <path
          d={path(actual)}
          fill='none'
          stroke='var(--monitor-success)'
          strokeWidth='2'
        />
        {probe.map((p, i) => (
          <circle
            key={`p${i}`}
            cx={x(p)}
            cy={y(p)}
            r='1.4'
            fill='var(--monitor-accent)'
          />
        ))}
        {actual.length === 1 && (
          <circle
            cx={x(actual[0])}
            cy={y(actual[0])}
            r='2'
            fill='var(--monitor-success)'
          />
        )}
      </svg>
      <div className='monitor-legend'>
        <span style={{ color: 'var(--monitor-accent)' }}>{t('探针')}</span>
        <span style={{ color: 'var(--monitor-success)' }}>{t('真实请求')}</span>
        <span>
          {Math.round(max).toLocaleString()} {t('毫秒')}
        </span>
      </div>
      {hover && (
        <div className='monitor-tooltip'>
          {[
            ['probe', t('探针')],
            ['actual', t('真实请求')],
          ].map(([key, label]) => (
            <div key={key}>
              {label}:{' '}
              {hover[key]
                ? `${new Date(hover[key].at).toLocaleTimeString()} · ${Math.round(hover[key].ttft).toLocaleString()} ms · n=${hover[key].count}`
                : '—'}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
