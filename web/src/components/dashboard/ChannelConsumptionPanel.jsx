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

import React, { useMemo, useState } from 'react';
import { Card, Empty, Radio, RadioGroup } from '@douyinfe/semi-ui';
import { ChartPie } from 'lucide-react';
import { VChart } from '@visactor/react-vchart';
import { renderNumber, renderQuota } from '../../helpers';

const CHANNEL_COLORS = [
  '#3b82f6',
  '#10b981',
  '#f59e0b',
  '#ef4444',
  '#8b5cf6',
  '#06b6d4',
  '#ec4899',
  '#84cc16',
  '#f97316',
  '#64748b',
];

const ChannelConsumptionPanel = ({
  data,
  CARD_PROPS,
  CHART_CONFIG,
  FLEX_CENTER_GAP2,
  t,
}) => {
  const [sortBy, setSortBy] = useState('token');

  const rows = useMemo(() => {
    return (data || [])
      .map((item, index) => {
        const channelId = Number(item.channel_id || 0);
        const name = item.channel_name
          ? item.channel_name
          : channelId > 0
            ? `${t('已删除渠道')} #${channelId}`
            : t('未知渠道');
        return {
          ...item,
          channel_id: channelId,
          channel_name: name,
          request_count: Number(item.request_count || 0),
          token_used: Number(item.token_used || 0),
          quota: Number(item.quota || 0),
          color: CHANNEL_COLORS[index % CHANNEL_COLORS.length],
        };
      })
      .sort((a, b) => {
        const field = sortBy === 'token' ? 'token_used' : 'quota';
        return b[field] - a[field] || a.channel_id - b.channel_id;
      });
  }, [data, sortBy, t]);

  const valueField = sortBy === 'token' ? 'token_used' : 'quota';
  const chartRows = rows.filter((item) => item[valueField] > 0);
  const chartSpec = useMemo(
    () => ({
      type: 'pie',
      data: [{ id: 'channelConsumption', values: chartRows }],
      valueField,
      categoryField: 'channel_name',
      outerRadius: 0.82,
      innerRadius: 0.52,
      padAngle: 0.8,
      legends: { visible: false },
      label: { visible: false },
      pie: {
        style: { cornerRadius: 4 },
        state: {
          hover: { outerRadius: 0.87 },
          selected: { outerRadius: 0.87 },
        },
      },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum.channel_name,
              value: (datum) =>
                sortBy === 'token'
                  ? renderNumber(datum.token_used)
                  : renderQuota(datum.quota, 4),
            },
          ],
        },
      },
      color: rows.map((item) => item.color),
    }),
    [chartRows, rows, sortBy, valueField],
  );

  return (
    <Card
      {...CARD_PROPS}
      className='!rounded-2xl'
      title={
        <div className='flex flex-col sm:flex-row sm:items-center sm:justify-between w-full gap-3'>
          <div className={FLEX_CENTER_GAP2}>
            <ChartPie size={16} />
            {t('渠道消耗')}
          </div>
          <RadioGroup
            type='button'
            value={sortBy}
            onChange={(event) => setSortBy(event.target.value)}
            aria-label={t('渠道消耗排序方式')}
          >
            <Radio value='token'>{t('按 Token')}</Radio>
            <Radio value='quota'>{t('按消耗')}</Radio>
          </RadioGroup>
        </div>
      }
      bodyStyle={{ padding: 0 }}
    >
      <div className='grid min-h-[24rem] lg:h-96 lg:grid-cols-[minmax(15rem,0.9fr)_minmax(0,2.1fr)]'>
        <div className='h-64 p-3 lg:h-full'>
          {chartRows.length > 0 ? (
            <VChart spec={chartSpec} option={CHART_CONFIG} />
          ) : (
            <div className='flex h-full items-center justify-center'>
              <Empty title={t('暂无数据')} />
            </div>
          )}
        </div>

        <div className='h-96 overflow-auto px-3 pb-3 lg:h-full lg:pt-3'>
          <div className='min-w-[36rem]'>
            <div className='sticky top-0 z-10 grid grid-cols-[minmax(10rem,1.4fr)_minmax(6rem,0.7fr)_minmax(7rem,0.9fr)_minmax(7rem,0.9fr)] border-b border-semi-color-border bg-semi-color-bg-2 px-2 py-2 text-xs font-medium text-semi-color-text-2'>
              <span>{t('渠道')}</span>
              <span className='text-right'>{t('请求')}</span>
              <span className='text-right'>{t('Token')}</span>
              <span className='text-right'>{t('消耗')}</span>
            </div>
            {rows.map((row) => (
              <div
                key={row.channel_id}
                className='grid min-h-10 grid-cols-[minmax(10rem,1.4fr)_minmax(6rem,0.7fr)_minmax(7rem,0.9fr)_minmax(7rem,0.9fr)] items-center border-b border-semi-color-border px-2 py-2 text-sm last:border-b-0'
              >
                <span className='flex min-w-0 items-center gap-2 font-medium text-semi-color-text-0'>
                  <span
                    className='h-2.5 w-2.5 flex-none rounded-sm'
                    style={{ backgroundColor: row.color }}
                  />
                  <span className='truncate' title={row.channel_name}>
                    {row.channel_name}
                  </span>
                </span>
                <span className='text-right text-semi-color-text-1'>
                  {renderNumber(row.request_count)}
                </span>
                <span className='text-right text-semi-color-text-1'>
                  {renderNumber(row.token_used)}
                </span>
                <span className='text-right font-medium text-emerald-500'>
                  {renderQuota(row.quota, 4)}
                </span>
              </div>
            ))}
            {rows.length === 0 && (
              <div className='flex h-64 items-center justify-center text-sm text-semi-color-text-2'>
                {t('暂无数据')}
              </div>
            )}
          </div>
        </div>
      </div>
    </Card>
  );
};

export default ChannelConsumptionPanel;
