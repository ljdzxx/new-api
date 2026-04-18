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

import React, { useMemo } from 'react';
import {
  Avatar,
  Button,
  Card,
  Empty,
  Input,
  Progress,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { BarChart3, Crown } from 'lucide-react';
import CardPro from '../../common/ui/CardPro';
import CardTable from '../../common/ui/CardTable';
import CompactModeToggle from '../../common/ui/CompactModeToggle';
import { createCardProPagination } from '../../../helpers/utils';
import { renderQuota } from '../../../helpers/render';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
import { useSubscriptionUsageRankData } from '../../../hooks/subscription-usage-rank/useSubscriptionUsageRankData';

const { Text } = Typography;

const RANGE_OPTIONS = [
  { key: '1d', label: '1 日' },
  { key: '3d', label: '3 日' },
  { key: '7d', label: '7 日' },
];

const formatDateTime = (value) => {
  if (!value) {
    return '-';
  }
  return new Date(value * 1000).toLocaleString();
};

const formatARPM = (value) => {
  if (!Number.isFinite(Number(value))) {
    return '-';
  }
  return Number(value).toFixed(2);
};

const renderSnapshotQuota = (used, total, unlimited, t) => {
  if (unlimited) {
    return (
      <div className='flex items-center justify-end gap-2'>
        <span>{renderQuota(Number(used || 0))}</span>
        <Tag color='green' size='small'>
          {t('无限')}
        </Tag>
      </div>
    );
  }
  return (
    <span>
      {renderQuota(Number(used || 0))} / {renderQuota(Number(total || 0))}
    </span>
  );
};

const renderUsageRatio = (ratio, unlimited, t) => {
  if (unlimited) {
    return (
      <div className='flex justify-end'>
        <Tag color='green' size='small'>
          {t('无限')}
        </Tag>
      </div>
    );
  }

  const percent = Math.max(0, Math.min(100, Number(ratio || 0) * 100));
  return (
    <div className='min-w-[130px]'>
      <div className='mb-1 text-xs text-right'>{percent.toFixed(1)}%</div>
      <Progress percent={percent} showInfo={false} />
    </div>
  );
};

const renderRankTag = (rank, t) => {
  if (rank === 1) {
    return (
      <Tag color='orange' shape='circle'>
        {t('第 1 名')}
      </Tag>
    );
  }
  if (rank === 2) {
    return (
      <Tag color='grey' shape='circle'>
        {t('第 2 名')}
      </Tag>
    );
  }
  if (rank === 3) {
    return (
      <Tag color='orange' shape='circle'>
        {t('第 3 名')}
      </Tag>
    );
  }
  return <span className='font-semibold'>#{rank}</span>;
};

const SummaryCard = ({ title, value, hint }) => (
  <Card className='!rounded-2xl shadow-sm' bodyStyle={{ padding: 16 }}>
    <div className='flex flex-col gap-2'>
      <Text type='tertiary' size='small'>
        {title}
      </Text>
      <Text strong style={{ fontSize: 22, lineHeight: '28px' }}>
        {value}
      </Text>
      <Text type='tertiary' size='small'>
        {hint}
      </Text>
    </div>
  </Card>
);

const SubscriptionUsageRankPage = () => {
  const isMobile = useIsMobile();
  const rankData = useSubscriptionUsageRankData();
  const {
    items,
    summary,
    loading,
    keywordInput,
    rangeKey,
    compactMode,
    setCompactMode,
    setKeywordInput,
    handleSearch,
    handleReset,
    handleRangeChange,
    handlePageChange,
    handlePageSizeChange,
    refresh,
    activePage,
    pageSize,
    total,
    t,
  } = rankData;

  const columns = useMemo(
    () => [
      {
        title: t('排名'),
        key: 'rank',
        width: 110,
        render: (_, record) => renderRankTag(record?.rank, t),
      },
      {
        title: t('用户'),
        key: 'user',
        width: 220,
        render: (_, record) => (
          <div className='flex items-center gap-2 justify-end md:justify-start'>
            <Avatar size='small' color='light-blue'>
              {(record?.display_name || record?.username || '?')
                .slice(0, 1)
                .toUpperCase()}
            </Avatar>
            <div className='min-w-0 text-right md:text-left'>
              <div className='font-medium truncate'>
                {record?.display_name || record?.username || '-'}
              </div>
              <div className='text-xs text-gray-500 truncate'>
                @{record?.username || '-'} · {record?.group || '-'}
              </div>
            </div>
          </div>
        ),
      },
      {
        title: t('窗口使用量'),
        dataIndex: 'usage_amount',
        width: 150,
        render: (value) => <span>{renderQuota(Number(value || 0))}</span>,
      },
      {
        title: t('今日使用率'),
        key: 'today_usage_ratio',
        width: 160,
        render: (_, record) =>
          renderUsageRatio(
            record?.today_usage_ratio,
            record?.today_subscription_unlimited,
            t,
          ),
      },
      {
        title: t('今日已用 / 总额度'),
        key: 'today_subscription',
        width: 180,
        render: (_, record) =>
          renderSnapshotQuota(
            record?.today_subscription_used,
            record?.today_subscription_total,
            record?.today_subscription_unlimited,
            t,
          ),
      },
      {
        title: t('请求次数'),
        dataIndex: 'request_count',
        width: 120,
      },
      {
        title: t('平均 ARPM'),
        key: 'arpm',
        width: 120,
        render: (_, record) => (
          <span>
            {formatARPM(record?.arpm)}
            <span className='text-xs text-gray-500'> / min</span>
          </span>
        ),
      },
      {
        title: t('活跃分钟数'),
        dataIndex: 'active_minutes',
        width: 120,
      },
      {
        title: t('最近请求时间'),
        key: 'last_request_at',
        width: 190,
        render: (_, record) => formatDateTime(record?.last_request_at),
      },
    ],
    [t],
  );

  const summaryCards = useMemo(
    () => [
      {
        key: 'ranked_user_count',
        title: t('上榜订阅用户'),
        value: summary?.ranked_user_count || 0,
        hint: t('仅统计当前仍有有效订阅且窗口内有消费记录的用户'),
      },
      {
        key: 'total_usage_amount',
        title: t('窗口总使用量'),
        value: renderQuota(Number(summary?.total_usage_amount || 0)),
        hint: t('按消费日志中的额度金额汇总'),
      },
      {
        key: 'total_request_count',
        title: t('总请求次数'),
        value: summary?.total_request_count || 0,
        hint: t('统计窗口内的消费请求总数'),
      },
      {
        key: 'average_arpm',
        title: t('平均 ARPM'),
        value: formatARPM(summary?.average_arpm || 0),
        hint: t('按用户活跃期平均请求频率计算'),
      },
    ],
    [summary, t],
  );

  const emptyNode = (
    <div className='py-12'>
      <Empty description={t('暂无排行榜数据')} />
    </div>
  );

  return (
    <CardPro
      type='type1'
      descriptionArea={
        <div className='flex flex-col md:flex-row justify-between items-start md:items-center gap-2 w-full'>
          <div className='flex items-center text-blue-500'>
            <BarChart3 size={16} className='mr-2' />
            <Text>{t('订阅套餐使用量排行榜')}</Text>
          </div>

          <CompactModeToggle
            compactMode={compactMode}
            setCompactMode={setCompactMode}
            t={t}
          />
        </div>
      }
      actionsArea={
        <div className='flex flex-col gap-3 w-full'>
          <div className='flex flex-wrap gap-2'>
            {RANGE_OPTIONS.map((option) => (
              <Button
                key={option.key}
                size='small'
                theme={rangeKey === option.key ? 'solid' : 'borderless'}
                type={rangeKey === option.key ? 'primary' : 'tertiary'}
                icon={option.key === rangeKey ? <Crown size={14} /> : null}
                onClick={() => handleRangeChange(option.key)}
              >
                {t(option.label)}
              </Button>
            ))}
            <Button
              size='small'
              icon={<IconRefresh />}
              onClick={() => refresh()}
            >
              {t('刷新')}
            </Button>
          </div>

          <div className='flex flex-col md:flex-row gap-2 w-full'>
            <Input
              value={keywordInput}
              onChange={setKeywordInput}
              onEnterPress={handleSearch}
              prefix={<IconSearch />}
              placeholder={t('搜索用户 ID / 用户名 / 显示名')}
              showClear
            />
            <div className='flex gap-2 w-full md:w-auto'>
              <Button className='flex-1 md:flex-none' onClick={handleSearch}>
                {t('搜索')}
              </Button>
              <Button
                className='flex-1 md:flex-none'
                type='tertiary'
                onClick={handleReset}
              >
                {t('重置')}
              </Button>
            </div>
          </div>
        </div>
      }
      paginationArea={createCardProPagination({
        currentPage: activePage,
        pageSize: pageSize,
        total,
        onPageChange: handlePageChange,
        onPageSizeChange: handlePageSizeChange,
        isMobile,
        t,
      })}
      t={t}
    >
      <div className='flex flex-col gap-4'>
        <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-3'>
          {summaryCards.map((card) => (
            <SummaryCard
              key={card.key}
              title={card.title}
              value={card.value}
              hint={card.hint}
            />
          ))}
        </div>

        <CardTable
          columns={columns}
          dataSource={items}
          loading={loading}
          rowKey='user_id'
          empty={emptyNode}
          pagination={false}
          scroll={compactMode ? undefined : { x: 'max-content' }}
        />
      </div>
    </CardPro>
  );
};

export default SubscriptionUsageRankPage;
