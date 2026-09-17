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
import { Table, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { CHANNEL_OPTIONS } from '../../../constants/channel.constants';
import { channelTypeIconMap } from '../../../constants/channel-icons';
import { renderGroupLabel } from '../../../helpers/render';

export default function GroupIconGuide() {
  const { t } = useTranslation();
  return (
    <details className='mb-4'>
      <summary
        className='cursor-pointer text-sm'
        style={{ color: 'var(--semi-color-primary)' }}
      >
        {t('渠道图标与样式名称对照表')}
      </summary>
      <p className='my-3 text-sm' style={{ color: 'var(--semi-color-text-2)' }}>
        {t(
          '点击样式名称可复制，名称区分大小写；同一图标的分组使用固定颜色。未配置的分组不显示图标。',
        )}
      </p>
      <Table
        size='small'
        pagination={false}
        scroll={{ y: 320 }}
        rowKey='value'
        dataSource={CHANNEL_OPTIONS}
        columns={[
          { title: t('渠道类型'), dataIndex: 'label' },
          {
            title: t('预览'),
            render: (_, channel) =>
              channelTypeIconMap[channel.value]
                ? renderGroupLabel({
                    value: channel.label,
                    icon: channelTypeIconMap[channel.value],
                  })
                : '—',
          },
          {
            title: t('图标样式名称'),
            render: (_, channel) =>
              channelTypeIconMap[channel.value] ? (
                <Typography.Text copyable>
                  {channelTypeIconMap[channel.value]}
                </Typography.Text>
              ) : (
                <Typography.Text type='tertiary'>
                  {t('暂无渠道图标')}
                </Typography.Text>
              ),
          },
        ]}
      />
    </details>
  );
}
