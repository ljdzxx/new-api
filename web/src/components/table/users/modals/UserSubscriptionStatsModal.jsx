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
  Button,
  Card,
  Checkbox,
  Empty,
  Form,
  Modal,
  Progress,
  Space,
  TabPane,
  Tabs,
  Tag,
  Typography,
  Badge,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import CardTable from '../../../common/ui/CardTable';
import { API, renderQuota, showError } from '../../../../helpers';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';

const { Text } = Typography;

function toDayString(value) {
  if (!value) {
    return '';
  }
  if (value instanceof Date) {
    const year = value.getFullYear();
    const month = `${value.getMonth() + 1}`.padStart(2, '0');
    const day = `${value.getDate()}`.padStart(2, '0');
    return `${year}-${month}-${day}`;
  }
  if (typeof value === 'string') {
    return value.slice(0, 10);
  }
  if (typeof value === 'number') {
    return toDayString(new Date(value));
  }
  return '';
}

function formatSnapshotTime(ts) {
  if (!ts) {
    return '-';
  }
  return new Date(ts * 1000).toLocaleString();
}

function renderAmount(value, unlimited, t) {
  if (unlimited) {
    return t('不限');
  }
  return renderQuota(Number(value || 0));
}

function renderUsageProgress(total, used, unlimited, t) {
  if (unlimited || total <= 0) {
    return <span className='text-xs text-gray-500'>{t('不限')}</span>;
  }
  const percent = Math.max(0, Math.min(100, (Number(used || 0) / total) * 100));
  return (
    <div className='min-w-[120px]'>
      <div className='text-xs mb-1'>{percent.toFixed(0)}%</div>
      <Progress percent={percent} showInfo={false} />
    </div>
  );
}

function renderStatusTag(status, t) {
  switch (status) {
    case 'active':
      return (
        <Tag
          color='white'
          size='small'
          shape='circle'
          prefixIcon={<Badge dot type='success' />}
        >
          {t('生效')}
        </Tag>
      );
    case 'cancelled':
    case 'deleted':
      return (
        <Tag color='white' size='small' shape='circle'>
          {t('已作废')}
        </Tag>
      );
    default:
      return (
        <Tag color='white' size='small' shape='circle'>
          {t('已过期')}
        </Tag>
      );
  }
}

const emptyNode = (
  <Empty
    image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
    darkModeImage={
      <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
    }
    description='暂无统计记录'
    style={{ padding: 30 }}
  />
);

const UserSubscriptionStatsModal = ({ visible, onCancel, user, t }) => {
  const isMobile = useIsMobile();
  const [formApi, setFormApi] = useState(null);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [activeTab, setActiveTab] = useState('daily');
  const [summary, setSummary] = useState(null);
  const [dailyItems, setDailyItems] = useState([]);
  const [dailyPage, setDailyPage] = useState(1);
  const [dailyTotal, setDailyTotal] = useState(0);
  const [detailItems, setDetailItems] = useState([]);
  const [detailPage, setDetailPage] = useState(1);
  const [detailTotal, setDetailTotal] = useState(0);
  const pageSize = 10;

  const buildParams = (override = {}) => {
    const values = formApi?.getValues?.() || {};
    const dateRange = override.dateRange ?? values.dateRange;
    const startDay = Array.isArray(dateRange) ? toDayString(dateRange[0]) : '';
    const endDay = Array.isArray(dateRange) ? toDayString(dateRange[1]) : '';

    return {
      start_day: startDay,
      end_day: endDay,
      keyword: (override.keyword ?? values.keyword ?? '').trim(),
      status: override.status ?? values.status ?? '',
      only_used: String(!!(override.onlyUsed ?? values.onlyUsed)),
    };
  };

  const loadData = async (view, page = 1, override = {}) => {
    if (!user?.id) {
      return;
    }
    const params = new URLSearchParams({
      view,
      p: String(page),
      page_size: String(pageSize),
      ...buildParams(override),
    });
    setLoading(true);
    try {
      const res = await API.get(
        `/api/user/${user.id}/subscription_daily_stats?${params.toString()}`,
      );
      const { success, message, data } = res.data;
      if (!success) {
        showError(message || t('加载失败'));
        return;
      }
      setSummary(data?.summary || null);
      const pageData = data?.page || {};
      if (view === 'daily') {
        setDailyItems(pageData.items || []);
        setDailyPage(pageData.page || page);
        setDailyTotal(pageData.total || 0);
      } else {
        setDetailItems(pageData.items || []);
        setDetailPage(pageData.page || page);
        setDetailTotal(pageData.total || 0);
      }
    } catch (error) {
      showError(t('请求失败'));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  };

  useEffect(() => {
    if (!visible || !user?.id) {
      return;
    }
    setActiveTab('daily');
    setDailyPage(1);
    setDetailPage(1);
    formApi?.reset?.();
    loadData('daily', 1);
  }, [visible, user?.id]);

  const handleSearch = async () => {
    if (activeTab === 'daily') {
      setDailyPage(1);
      await loadData('daily', 1);
      return;
    }
    setDetailPage(1);
    await loadData('detail', 1);
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    await loadData(activeTab, activeTab === 'daily' ? dailyPage : detailPage);
  };

  const handleTabChange = async (tabKey) => {
    setActiveTab(tabKey);
    if (tabKey === 'daily') {
      await loadData('daily', 1);
      return;
    }
    await loadData('detail', 1);
  };

  const handleDailyPageChange = async (page) => {
    setDailyPage(page);
    await loadData('daily', page);
  };

  const handleDetailPageChange = async (page) => {
    setDetailPage(page);
    await loadData('detail', page);
  };

  const jumpToDayDetails = async (statDay) => {
    const start = new Date(`${statDay}T00:00:00`);
    const end = new Date(`${statDay}T23:59:59`);
    formApi?.setValue?.('dateRange', [start, end]);
    setActiveTab('detail');
    setDetailPage(1);
    await loadData('detail', 1, {
      dateRange: [start, end],
    });
  };

  const dailyColumns = useMemo(
    () => [
      {
        title: t('日期'),
        dataIndex: 'stat_day',
        width: 120,
      },
      {
        title: t('当日总额度'),
        key: 'total',
        width: 160,
        render: (_, record) =>
          renderAmount(
            record?.total,
            Number(record?.unlimited_count || 0) > 0,
            t,
          ),
      },
      {
        title: t('当日已用额度'),
        key: 'used',
        width: 160,
        render: (_, record) => renderQuota(Number(record?.used || 0)),
      },
      {
        title: t('当日剩余额度'),
        key: 'remain',
        width: 160,
        render: (_, record) =>
          renderAmount(
            record?.remain,
            Number(record?.unlimited_count || 0) > 0,
            t,
          ),
      },
      {
        title: t('使用率'),
        key: 'usage_rate',
        width: 150,
        render: (_, record) =>
          renderUsageProgress(
            Number(record?.total || 0),
            Number(record?.used || 0),
            Number(record?.unlimited_count || 0) > 0,
            t,
          ),
      },
      {
        title: t('订阅数'),
        dataIndex: 'subscription_count',
        width: 100,
      },
      {
        title: '',
        key: 'view',
        width: 110,
        render: (_, record) => (
          <Button
            size='small'
            theme='light'
            type='primary'
            onClick={() => jumpToDayDetails(record?.stat_day)}
          >
            {t('查看明细')}
          </Button>
        ),
      },
    ],
    [t],
  );

  const detailColumns = useMemo(
    () => [
      {
        title: t('日期'),
        dataIndex: 'stat_day',
        width: 120,
      },
      {
        title: t('套餐名称'),
        dataIndex: 'plan_title',
        width: 180,
        render: (text, record) => text || `#${record?.plan_id || '-'}`,
      },
      {
        title: t('订阅ID'),
        dataIndex: 'user_subscription_id',
        width: 100,
      },
      {
        title: t('状态'),
        dataIndex: 'snapshot_status',
        width: 100,
        render: (text) => renderStatusTag(text, t),
      },
      {
        title: t('当日总额度'),
        key: 'amount_total',
        width: 160,
        render: (_, record) =>
          renderAmount(record?.amount_total, !!record?.unlimited, t),
      },
      {
        title: t('当日已用额度'),
        key: 'amount_used',
        width: 160,
        render: (_, record) => renderQuota(Number(record?.amount_used || 0)),
      },
      {
        title: t('当日剩余额度'),
        key: 'amount_remain',
        width: 160,
        render: (_, record) =>
          renderAmount(record?.amount_remain, !!record?.unlimited, t),
      },
      {
        title: t('使用率'),
        key: 'usage_rate',
        width: 150,
        render: (_, record) =>
          renderUsageProgress(
            Number(record?.amount_total || 0),
            Number(record?.amount_used || 0),
            !!record?.unlimited,
            t,
          ),
      },
      {
        title: t('统计时间'),
        dataIndex: 'updated_at',
        width: 180,
        render: (text) => formatSnapshotTime(text),
      },
    ],
    [t],
  );

  return (
    <Modal
      visible={visible}
      title={
        <Space align='center'>
          <Tag color='blue' shape='circle'>
            {t('统计')}
          </Tag>
          <Typography.Title heading={4} className='m-0'>
            {t('订阅额度统计')}
          </Typography.Title>
          <Text type='tertiary'>
            {user?.username || '-'} (ID: {user?.id || '-'})
          </Text>
        </Space>
      }
      footer={null}
      onCancel={onCancel}
      width={isMobile ? '100%' : 1180}
      bodyStyle={{ padding: 0 }}
      centered
    >
      <div className='p-4 space-y-4'>
        <div className='flex flex-col gap-3'>
          <div className='flex items-center justify-between gap-3 flex-wrap'>
            <Text type='tertiary'>
              {t('展示该用户按日沉淀的订阅额度记录，支持按日汇总与按套餐明细查看。')}
            </Text>
            <Button
              icon={<IconRefresh spin={refreshing} />}
              onClick={handleRefresh}
              loading={refreshing}
            >
              {t('刷新')}
            </Button>
          </div>

          <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-3'>
            <Card bodyStyle={{ padding: 14 }}>
              <Text type='tertiary'>{t('今日总额度')}</Text>
              <div className='text-lg font-semibold mt-1'>
                {renderAmount(
                  summary?.total,
                  Number(summary?.unlimited_count || 0) > 0,
                  t,
                )}
              </div>
            </Card>
            <Card bodyStyle={{ padding: 14 }}>
              <Text type='tertiary'>{t('今日已用额度')}</Text>
              <div className='text-lg font-semibold mt-1'>
                {renderQuota(Number(summary?.used || 0))}
              </div>
            </Card>
            <Card bodyStyle={{ padding: 14 }}>
              <Text type='tertiary'>{t('今日剩余额度')}</Text>
              <div className='text-lg font-semibold mt-1'>
                {renderAmount(
                  summary?.remain,
                  Number(summary?.unlimited_count || 0) > 0,
                  t,
                )}
              </div>
            </Card>
            <Card bodyStyle={{ padding: 14 }}>
              <Text type='tertiary'>{t('当前生效订阅数')}</Text>
              <div className='text-lg font-semibold mt-1'>
                {Number(summary?.active_subscription_count || 0)}
              </div>
              {Number(summary?.unlimited_count || 0) > 0 && (
                <Text type='tertiary' size='small'>
                  {t('含不限额订阅')} {Number(summary?.unlimited_count || 0)}
                </Text>
              )}
            </Card>
          </div>

          <Form
            initValues={{
              dateRange: [],
              keyword: '',
              status: '',
              onlyUsed: false,
            }}
            getFormApi={(api) => setFormApi(api)}
            onSubmit={handleSearch}
            allowEmpty
            autoComplete='off'
            layout='vertical'
          >
            <div className='grid grid-cols-1 lg:grid-cols-4 gap-3'>
              <Form.DatePicker
                field='dateRange'
                type='dateRange'
                inputReadOnly
                showClear
                label={t('日期范围')}
              />
              <Form.Input
                field='keyword'
                label={t('套餐名称 / 订阅ID')}
                prefix={<IconSearch />}
                showClear
              />
              <Form.Select
                field='status'
                label={t('状态')}
                showClear
                optionList={[
                  { label: t('生效'), value: 'active' },
                  { label: t('已过期'), value: 'expired' },
                  { label: t('已作废'), value: 'cancelled' },
                ]}
              />
              <div className='flex items-end gap-3'>
                <Form.Checkbox field='onlyUsed' noLabel>
                  {t('仅看有消耗')}
                </Form.Checkbox>
                <Button type='primary' htmlType='submit'>
                  {t('查询')}
                </Button>
              </div>
            </div>
          </Form>
        </div>

        <Tabs type='card' activeKey={activeTab} onChange={handleTabChange}>
          <TabPane tab={t('按日汇总')} itemKey='daily'>
            <CardTable
              columns={dailyColumns}
              dataSource={dailyItems}
              rowKey={(row) => row?.stat_day}
              loading={loading && activeTab === 'daily'}
              scroll={{ x: 'max-content' }}
              hidePagination={false}
              pagination={{
                currentPage: dailyPage,
                pageSize,
                total: dailyTotal,
                pageSizeOpts: [10, 20, 50],
                showSizeChanger: false,
                onPageChange: handleDailyPageChange,
              }}
              empty={emptyNode}
            />
          </TabPane>
          <TabPane tab={t('按套餐明细')} itemKey='detail'>
            <CardTable
              columns={detailColumns}
              dataSource={detailItems}
              rowKey={(row) => row?.id}
              loading={loading && activeTab === 'detail'}
              scroll={{ x: 'max-content' }}
              hidePagination={false}
              pagination={{
                currentPage: detailPage,
                pageSize,
                total: detailTotal,
                pageSizeOpts: [10, 20, 50],
                showSizeChanger: false,
                onPageChange: handleDetailPageChange,
              }}
              empty={emptyNode}
            />
          </TabPane>
        </Tabs>
      </div>
    </Modal>
  );
};

export default UserSubscriptionStatsModal;
