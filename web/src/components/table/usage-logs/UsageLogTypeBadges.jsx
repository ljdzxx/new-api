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
import { Badge, Tooltip } from '@douyinfe/semi-ui';

export default function UsageLogTypeBadges({
  children,
  codes,
  canceledByDownstream,
  t,
}) {
  const badges = [
    {
      code: 'R1',
      type: 'success',
      hint: t('reasoning密文失败后进行的有损重试'),
    },
    {
      code: 'E1',
      type: 'danger',
      hint: t('reasoning密文失败，可能需要客户端新开会话窗口重试'),
    },
    {
      code: 'E2',
      type: 'danger',
      hint: t('compaction密文失败，可能需要客户端新开会话窗口重试'),
    },
  ].filter(({ code }) => Array.isArray(codes) && codes.includes(code));

  if (canceledByDownstream === true) {
    badges.push({
      code: 'C',
      type: 'success',
      hint: t('流式响应已被下游取消'),
    });
  }

  if (badges.length === 0) return children;

  return (
    <span
      className='usage-log-type-with-badges'
      style={{ paddingRight: badges.length * 25 - 8 }}
    >
      <span className='usage-log-type-anchor'>
        {children}
        <span className='usage-log-type-badges'>
          {badges.map(({ code, type, hint }) => (
            <Tooltip key={code} content={hint}>
              <span
                className='usage-log-type-badge-trigger'
                tabIndex={0}
                aria-label={`${code}: ${hint}`}
              >
                <Badge count={code} type={type} theme='solid' />
              </span>
            </Tooltip>
          ))}
        </span>
      </span>
    </span>
  );
}
