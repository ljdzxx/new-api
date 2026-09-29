import React, { useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Select,
  Spin,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { monitorRequests } from './requests';
import Trend from './Trend';
import GroupTests from './GroupTests';
import { monitorFailureMessage } from './errors';
import UseGroup from './UseGroup';
import './monitor.css';

function Ring({ value, label, count }) {
  const color =
    value == null
      ? 'var(--semi-color-text-3)'
      : value >= 0.95
        ? '#34d399'
        : '#38bdf8';
  return (
    <div className='monitor-ring-wrap' title={`n=${count}`}>
      <div
        className='monitor-ring'
        style={{
          background: `conic-gradient(${color} ${(value || 0) * 360}deg, var(--semi-color-fill-1) 0)`,
        }}
      >
        <span>{value == null ? '—' : `${(value * 100).toFixed(1)}%`}</span>
      </div>
      <small>{label}</small>
    </div>
  );
}

function GroupRow({ group, model, onModelChange, probeMinutes, useGroup }) {
  const { t } = useTranslation();
  const state = (probe) =>
    !probe || Date.now() - probe.at > probeMinutes * 120000
      ? 'grey'
      : probe.ok
        ? 'green'
        : 'red';
  const metrics = group.metrics;
  return (
    <tr>
      <td>
        <strong>{group.name}</strong>
        <div className='monitor-subtitle'>{group.description || '—'}</div>
      </td>
      <td>
        <strong>{group.ratio == null ? '—' : `${group.ratio}x`}</strong>
      </td>
      <td>
        <div className='monitor-models'>
          {group.models.map((m) => (
            <Tag
              key={m.name}
              color={state(m.probe)}
              title={
                m.probe
                  ? `${new Date(m.probe.at).toLocaleString()} · ${m.probe.error ? monitorFailureMessage(m.probe.error, t) : t('可用')}`
                  : t('未检测')
              }
            >
              {m.name} ·{' '}
              {state(m.probe) === 'grey'
                ? t('未知')
                : m.probe.ok
                  ? t('可用')
                  : t('异常')}
            </Tag>
          ))}
        </div>
      </td>
      <td>
        <strong>
          {metrics.ttft == null
            ? '—'
            : `${Math.round(metrics.ttft).toLocaleString()} ms`}
        </strong>
        <div className='monitor-latency-bar'>
          <i
            style={{
              width: `${metrics.ttft == null ? 0 : Math.max(5, 100 - metrics.ttft / 600)}%`,
            }}
          />
        </div>
        <small>
          {t('样本数')} {metrics.ttft_count}
        </small>
      </td>
      <td>
        <Ring
          value={metrics.cache}
          label={t('1h 缓存命中')}
          count={metrics.cache_count}
        />
      </td>
      <td>
        <Ring
          value={metrics.success_rate}
          label={t('1h 成功率')}
          count={metrics.total}
        />
      </td>
      <td className='monitor-chart-cell'>
        <Select
          size='small'
          value={model}
          onChange={onModelChange}
          optionList={[
            { label: t('全部模型'), value: '' },
            ...group.models.map((m) => ({ label: m.name, value: m.name })),
          ]}
        />
        <Trend data={group.trend} />
      </td>
      <GroupTests group={group} />
      <td>
        <Button size='small' theme='solid' onClick={() => useGroup(group)}>
          {t('使用此分组')}
        </Button>
      </td>
    </tr>
  );
}

export default function Monitor() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [data, setData] = useState(null),
    [error, setError] = useState(''),
    [loading, setLoading] = useState(false);
  const [hours, setHours] = useState(1),
    [sort, setSort] = useState('default'),
    [using, setUsing] = useState(null);
  const refresh = useRef(() => {});
  const [models, setModels] = useState({});
  const selectedModels = JSON.stringify(
    Object.fromEntries(
      Object.entries(models)
        .filter(([, name]) => name)
        .sort(),
    ),
  );
  useEffect(() => {
    let disposed = false,
      busy = false,
      timer,
      controller;
    const load = async () => {
      if (disposed || busy || document.hidden) return;
      clearTimeout(timer);
      const pause = monitorRequests.pauseRemaining();
      if (pause > 0) {
        timer = setTimeout(load, pause);
        return;
      }
      busy = true;
      controller = new AbortController();
      setLoading(true);
      try {
        const res = await monitorRequests.get('/api/monitor', {
          params: { hours, models: selectedModels },
          signal: controller.signal,
        });
        if (disposed || controller.signal.aborted) return;
        setData(res.data.data);
        setError('');
      } catch (e) {
        if (!disposed && e.name !== 'AbortError') setError(e.message);
      } finally {
        busy = false;
        if (!disposed) {
          setLoading(false);
          if (!document.hidden)
            timer = setTimeout(
              load,
              Math.max(30000, monitorRequests.pauseRemaining()),
            );
        }
      }
    };
    const visibility = () => {
      if (document.hidden) {
        clearTimeout(timer);
        controller?.abort();
      } else load();
    };
    refresh.current = load;
    load();
    document.addEventListener('visibilitychange', visibility);
    return () => {
      disposed = true;
      clearTimeout(timer);
      controller?.abort();
      refresh.current = () => {};
      document.removeEventListener('visibilitychange', visibility);
    };
  }, [hours, selectedModels]);
  const groups = [...(data?.groups || [])];
  if (sort !== 'default')
    groups.sort((a, b) => {
      const av = sort === 'ratio' ? a.ratio : a.metrics[sort],
        bv = sort === 'ratio' ? b.ratio : b.metrics[sort];
      if (av == null) return 1;
      if (bv == null) return -1;
      return sort === 'cache' || sort === 'success_rate' ? bv - av : av - bv;
    });
  const healthy = groups.filter(
    (g) =>
      g.models.length &&
      g.models.every(
        (m) =>
          m.probe?.ok && Date.now() - m.probe.at <= data.probe_minutes * 120000,
      ),
  ).length;
  const unavailable =
    data?.state === 'redis_unavailable' || data?.state === 'disabled';
  return (
    <div className='monitor-page'>
      <div className='monitor-heading'>
        <div>
          <Typography.Title heading={3}>{t('可用性')}</Typography.Title>
          <p>{t('最近一小时真实请求统计与主动探测')}</p>
        </div>
        <Button loading={loading} onClick={() => refresh.current()}>
          {t('刷新')}
        </Button>
      </div>
      {error && (
        <Banner
          type='warning'
          description={t('监控读取已暂停，稍后自动重试')}
        />
      )}
      {data?.state === 'redis_unavailable' && (
        <Banner
          type='warning'
          description={t('Redis 未配置或不可用，无法展示监控数据。')}
        />
      )}
      {data?.state === 'disabled' && (
        <Banner type='info' description={t('分组监控尚未启用')} />
      )}
      {!data && !error && <Spin />}
      {data && !unavailable && (
        <>
          <div className='monitor-summary'>
            <div>
              <strong>{groups.length}</strong>
              {t('监控分组')}
            </div>
            <div>
              <strong>{healthy}</strong>
              {t('全部模型可用')}
            </div>
            <div>
              <strong>{groups.length - healthy}</strong>
              {t('异常或未知')}
            </div>
            <small>
              {t('更新时间')} {new Date(data.updated_at).toLocaleString()}
            </small>
          </div>
          <div className='monitor-toolbar'>
            <span>{t('趋势时间范围')}</span>
            {[1, 6, 24].map((n) => (
              <Button
                key={n}
                size='small'
                theme={hours === n ? 'solid' : 'light'}
                onClick={() => setHours(n)}
              >
                {n}h
              </Button>
            ))}
            <Select
              value={sort}
              onChange={setSort}
              optionList={[
                { value: 'default', label: t('默认排序') },
                { value: 'ratio', label: t('倍率') },
                { value: 'ttft', label: t('平均首字延迟') },
                { value: 'cache', label: t('缓存命中') },
                { value: 'success_rate', label: t('成功率') },
              ]}
            />
          </div>
          <div className='monitor-table-scroll'>
            <table className='monitor-table'>
              <thead>
                <tr>
                  {[
                    t('分组'),
                    t('倍率'),
                    t('状态 / 模型'),
                    t('平均首字延迟（1h）'),
                    t('缓存命中'),
                    t('成功率'),
                    t('TTFT 趋势'),
                    t('模型检测'),
                    t('预览'),
                    t('使用分组'),
                  ].map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {groups.map((g) => (
                  <GroupRow
                    key={g.name}
                    group={g}
                    model={models[g.name] || ''}
                    onModelChange={(model) =>
                      setModels((current) => ({ ...current, [g.name]: model }))
                    }
                    probeMinutes={data.probe_minutes}
                    useGroup={(group) => {
                      if (!localStorage.getItem('user')) {
                        navigate('/login');
                        return;
                      }
                      setUsing(group);
                    }}
                  />
                ))}
              </tbody>
            </table>
          </div>
          <p className='monitor-footnote'>
            {t(
              '缓存率仅统计读缓存 token 大于 0 的请求；首字包含空 SSE 事件。无样本显示 —，过期探测显示未知。',
            )}
          </p>
        </>
      )}
      {using && <UseGroup group={using} onClose={() => setUsing(null)} />}
    </div>
  );
}
