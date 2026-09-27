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

import React from 'react';
import { Tag, Tooltip, Button } from '@douyinfe/semi-ui';

export default function ModelHealthStatus({ health, model, t, onClick }) {
  if (!health) return null;
  if (health.error)
    return (
      <Tooltip content={health.error}>
        <Tag color='orange'>{t('模型状态读取失败')}</Tag>
      </Tooltip>
    );
  if (!health.supported) return <Tag color='grey'>{t('不参与监控')}</Tag>;
  if (model !== undefined) {
    const state = health.models?.[model];
    if (!state) return <Tag color='grey'>{t('暂无观测')}</Tag>;
    const label = !state.available
      ? t('不可用')
      : state.observed
        ? t('可用')
        : t('暂无观测');
    return (
      <div className='flex flex-col gap-1'>
        <Tag
          color={!state.available ? 'red' : state.observed ? 'green' : 'grey'}
        >
          {label}
        </Tag>
        {!health.enabled && <span>{t('监控已关闭')}</span>}
        {state.count > 0 && (
          <span>
            {state.code} · {state.count}/{health.threshold}
          </span>
        )}
        {state.blocked_at > 0 && (
          <span>
            {t('停用时间')}：
            {new Date(state.blocked_at * 1000).toLocaleString()}
          </span>
        )}
        {state.recovered_at > 0 && (
          <span>
            {t('测试恢复时间')}：
            {new Date(state.recovered_at * 1000).toLocaleString()}
          </span>
        )}
      </div>
    );
  }
  const states = Object.values(health.models || {});
  const blocked = states.filter((state) => !state.available).length;
  return (
    <Button
      size='small'
      theme='borderless'
      type={blocked ? 'danger' : 'tertiary'}
      onClick={onClick}
    >
      {t('模型可用性')} {states.length - blocked}/{states.length}
      {!health.enabled && ` · ${t('监控已关闭')}`}
    </Button>
  );
}
