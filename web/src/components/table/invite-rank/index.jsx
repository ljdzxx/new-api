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

import React, { useEffect, useMemo, useState } from 'react';
import {
  Avatar,
  Button,
  DatePicker,
  Empty,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { Pencil, ReceiptText, ShieldCheck, UserPlus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, renderNumber, showError } from '../../../helpers';
import { createCardProPagination } from '../../../helpers/utils';
import { useIsMobile } from '../../../hooks/common/useIsMobile';
import { useTableCompactMode } from '../../../hooks/common/useTableCompactMode';
import CardPro from '../../common/ui/CardPro';
import CardTable from '../../common/ui/CardTable';
import CompactModeToggle from '../../common/ui/CompactModeToggle';
import TopupHistoryModal from '../../topup/modals/TopupHistoryModal';
import EditUserModal from '../users/modals/EditUserModal';
import InviteRewardAuditsModal from '../users/modals/InviteRewardAuditsModal';

const { Text } = Typography;
const MAX_CUSTOM_RANGE_SECONDS = 366 * 24 * 60 * 60;
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];
const RANGE_OPTIONS = [
  { key: 'today', label: '当天' },
  { key: 'day', label: '天' },
  { key: 'week', label: '周' },
  { key: 'month', label: '月' },
];

const toTimestamp = (value) => Math.floor(new Date(value).getTime() / 1000);

const formatDateTime = (timestamp) => {
  if (!timestamp) return '-';
  return new Date(timestamp * 1000).toLocaleString();
};

const InviteRankTable = () => {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const [compactMode, setCompactMode] = useTableCompactMode('invite-rank');
  const [items, setItems] = useState([]);
  const [summary, setSummary] = useState({});
  const [windowInfo, setWindowInfo] = useState(null);
  const [rangeKey, setRangeKey] = useState('today');
  const [dateRange, setDateRange] = useState([]);
  const [loading, setLoading] = useState(true);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [editingUser, setEditingUser] = useState({ id: undefined });
  const [showEditUser, setShowEditUser] = useState(false);
  const [auditUserId, setAuditUserId] = useState(0);
  const [showAudits, setShowAudits] = useState(false);
  const [selectedUser, setSelectedUser] = useState(null);
  const [showOrders, setShowOrders] = useState(false);

  const loadRank = async (nextRange = rangeKey, nextDateRange = dateRange) => {
    const params = new URLSearchParams({ range: nextRange });
    if (nextRange === 'custom') {
      if (!Array.isArray(nextDateRange) || nextDateRange.length !== 2) {
        showError(t('请选择完整的日期时间范围'));
        return;
      }
      const start = toTimestamp(nextDateRange[0]);
      const end = toTimestamp(nextDateRange[1]);
      if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
        showError(t('结束时间必须晚于开始时间'));
        return;
      }
      if (end - start > MAX_CUSTOM_RANGE_SECONDS) {
        showError(t('自定义查询范围不能超过 366 天'));
        return;
      }
      params.set('start_timestamp', String(start));
      params.set('end_timestamp', String(end));
    }

    setLoading(true);
    try {
      const res = await API.get(`/api/user/invite_rank?${params}`);
      if (!res.data?.success) {
        showError(res.data?.message || t('加载失败'));
        return;
      }
      const payload = res.data?.data || {};
      const nextWindow = payload.window || null;
      setItems(payload.items || []);
      setSummary(payload.summary || {});
      setWindowInfo(nextWindow);
      setRangeKey(nextRange);
      setActivePage(1);
      if (nextWindow?.start && nextWindow?.end) {
        setDateRange([
          new Date(nextWindow.start * 1000),
          new Date(nextWindow.end * 1000),
        ]);
      }
    } catch (error) {
      showError(t('请求失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadRank('today', []);
  }, []);

  const columns = useMemo(
    () => [
      {
        title: t('排名'),
        dataIndex: 'rank',
        width: 80,
        render: (rank) =>
          rank <= 3 ? (
            <Tag color={rank === 1 ? 'orange' : 'grey'} shape='circle'>
              #{rank}
            </Tag>
          ) : (
            <Text strong>#{rank}</Text>
          ),
      },
      {
        title: t('邀请人'),
        key: 'user',
        width: 230,
        render: (_, record) => (
          <div className='flex items-center gap-2'>
            <Avatar size='small' color='light-blue'>
              {(record?.display_name || record?.username || '?')
                .slice(0, 1)
                .toUpperCase()}
            </Avatar>
            <div className='min-w-0'>
              <div className='flex items-center gap-2'>
                <Text
                  strong
                  ellipsis={{ showTooltip: true }}
                  style={{ maxWidth: 130 }}
                >
                  {record?.display_name ||
                    record?.username ||
                    `#${record?.user_id}`}
                </Text>
                {record?.deleted ? <Tag color='red'>{t('已注销')}</Tag> : null}
              </div>
              <Text type='tertiary' size='small'>
                @{record?.username || '-'} · {t('用户 ID')} {record?.user_id}
              </Text>
            </div>
          </div>
        ),
      },
      {
        title: t('分组'),
        dataIndex: 'group',
        width: 110,
        render: (value) => value || '-',
      },
      {
        title: t('区间有效邀请人数'),
        dataIndex: 'invite_count',
        width: 150,
        render: (value) => (
          <Text strong>{renderNumber(Number(value || 0))}</Text>
        ),
      },
      {
        title: t('累计邀请人数'),
        dataIndex: 'total_aff_count',
        width: 130,
        render: (value) => renderNumber(Number(value || 0)),
      },
      {
        title: t('区间占比'),
        key: 'range_ratio',
        width: 120,
        render: (_, record) => {
          const total = Number(summary?.total_invite_count || 0);
          if (total <= 0) return '0%';
          return `${((Number(record?.invite_count || 0) / total) * 100).toFixed(2)}%`;
        },
      },
      {
        title: t('最近邀请时间'),
        dataIndex: 'last_invite_at',
        width: 180,
        render: formatDateTime,
      },
      {
        title: t('操作'),
        key: 'actions',
        fixed: 'right',
        width: 300,
        render: (_, record) => (
          <Space>
            <Button
              size='small'
              type='tertiary'
              theme='light'
              icon={<ReceiptText size={14} />}
              onClick={() => {
                setSelectedUser(record);
                setShowOrders(true);
              }}
            >
              {t('订单')}
            </Button>
            <Button
              size='small'
              type='tertiary'
              theme='light'
              icon={<ShieldCheck size={14} />}
              onClick={() => {
                setAuditUserId(record.user_id);
                setShowAudits(true);
              }}
            >
              {t('审计')}
            </Button>
            {!record?.deleted ? (
              <Button
                size='small'
                type='tertiary'
                icon={<Pencil size={14} />}
                onClick={() => {
                  setEditingUser({ id: record.user_id });
                  setShowEditUser(true);
                }}
              >
                {t('编辑')}
              </Button>
            ) : null}
          </Space>
        ),
      },
    ],
    [summary, t],
  );

  const pageItems = useMemo(() => {
    const start = (activePage - 1) * pageSize;
    return items.slice(start, start + pageSize);
  }, [items, activePage, pageSize]);

  const metrics = [
    [
      t('区间有效邀请总数'),
      renderNumber(Number(summary?.total_invite_count || 0)),
    ],
    [
      t('产生有效邀请的用户'),
      renderNumber(Number(summary?.inviter_count || 0)),
    ],
    [
      t('Top 100 有效邀请数'),
      renderNumber(Number(summary?.top_100_invite_count || 0)),
    ],
    [
      t('第一名有效邀请数'),
      renderNumber(Number(summary?.top_invite_count || 0)),
    ],
  ];

  return (
    <>
      <CardPro
        type='type1'
        descriptionArea={
          <div className='flex flex-col md:flex-row justify-between gap-2 w-full'>
            <div>
              <div className='flex items-center gap-2 text-blue-500'>
                <UserPlus size={17} />
                <Text>{t('拉新排行榜')}</Text>
              </div>
              <Text type='tertiary' size='small'>
                {windowInfo
                  ? `${formatDateTime(windowInfo.start)} - ${formatDateTime(windowInfo.end)} · ${windowInfo.timezone || '-'}`
                  : t('正在加载')}
              </Text>
            </div>
            <CompactModeToggle
              compactMode={compactMode}
              setCompactMode={setCompactMode}
              t={t}
            />
          </div>
        }
        actionsArea={
          <div className='flex flex-col xl:flex-row gap-3 xl:items-center w-full'>
            <div className='flex flex-wrap gap-2'>
              {RANGE_OPTIONS.map((option) => (
                <Button
                  key={option.key}
                  size='small'
                  type={rangeKey === option.key ? 'primary' : 'tertiary'}
                  theme={rangeKey === option.key ? 'solid' : 'outline'}
                  onClick={() => loadRank(option.key, dateRange)}
                >
                  {t(option.label)}
                </Button>
              ))}
            </div>
            <div className='flex flex-col sm:flex-row gap-2 flex-1'>
              <DatePicker
                className='w-full'
                type='dateTimeRange'
                value={dateRange}
                onChange={(value) => {
                  setDateRange(value || []);
                  setRangeKey('custom');
                }}
                placeholder={[t('开始时间'), t('结束时间')]}
                showClear
                size='small'
              />
              <Button
                size='small'
                type='primary'
                onClick={() => loadRank('custom', dateRange)}
              >
                {t('查询')}
              </Button>
              <Button
                size='small'
                icon={<IconRefresh />}
                onClick={() => loadRank(rangeKey, dateRange)}
              >
                {t('刷新')}
              </Button>
            </div>
          </div>
        }
        paginationArea={createCardProPagination({
          currentPage: activePage,
          pageSize,
          total: items.length,
          onPageChange: setActivePage,
          onPageSizeChange: (size) => {
            setPageSize(size);
            setActivePage(1);
          },
          pageSizeOpts: PAGE_SIZE_OPTIONS,
          isMobile,
          t,
        })}
        t={t}
      >
        <div className='flex flex-col gap-4'>
          <div
            className='grid grid-cols-2 lg:grid-cols-4 border rounded-lg overflow-hidden'
            style={{ borderColor: 'var(--semi-color-border)' }}
          >
            {metrics.map(([label, value]) => (
              <div
                key={label}
                className='p-3 border-r last:border-r-0'
                style={{ borderColor: 'var(--semi-color-border)' }}
              >
                <Text type='tertiary' size='small'>
                  {label}
                </Text>
                <div className='mt-1'>
                  <Text strong>{value}</Text>
                </div>
              </div>
            ))}
          </div>
          <CardTable
            columns={
              compactMode
                ? columns.map(({ fixed, ...column }) => column)
                : columns
            }
            dataSource={pageItems}
            loading={loading}
            rowKey='user_id'
            pagination={false}
            scroll={compactMode ? undefined : { x: 'max-content' }}
            empty={<Empty description={t('暂无排行榜数据')} />}
          />
        </div>
      </CardPro>

      <InviteRewardAuditsModal
        visible={showAudits}
        handleClose={() => setShowAudits(false)}
        inviterId={auditUserId}
        t={t}
      />
      <TopupHistoryModal
        visible={showOrders}
        onCancel={() => setShowOrders(false)}
        t={t}
        userId={selectedUser?.user_id || 0}
      />
      <EditUserModal
        visible={showEditUser}
        handleClose={() => {
          setShowEditUser(false);
          setEditingUser({ id: undefined });
        }}
        editingUser={editingUser}
        refresh={() => loadRank(rangeKey, dateRange)}
      />
    </>
  );
};

export default InviteRankTable;
