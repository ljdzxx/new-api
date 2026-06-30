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
  Empty,
  Form,
  Modal,
  Progress,
  Space,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { CalendarClock } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import CardPro from '../../common/ui/CardPro';
import CardTable from '../../common/ui/CardTable';
import { API, showError, showSuccess } from '../../../helpers';
import { createCardProPagination } from '../../../helpers/utils';
import { renderQuota } from '../../../helpers/render';
import { useIsMobile } from '../../../hooks/common/useIsMobile';

const { Text } = Typography;

const formatTs = (ts) => {
  if (!ts) return '-';
  return new Date(ts * 1000).toLocaleString();
};

const toUnix = (value) => {
  if (!value) return 0;
  const date = value instanceof Date ? value : new Date(value);
  const ts = date.getTime();
  return Number.isFinite(ts) ? Math.floor(ts / 1000) : 0;
};

const getQuotaSummary = (subscriptions = []) => {
  return subscriptions.reduce(
    (summary, item) => {
      const sub = item?.subscription || {};
      const total = Number(sub.amount_total || 0);
      const used = Math.max(0, Number(sub.amount_used || 0));
      if (total <= 0) {
        summary.unlimited += 1;
      } else {
        summary.total += total;
        summary.used += Math.min(used, total);
      }
      return summary;
    },
    { total: 0, used: 0, unlimited: 0 },
  );
};

const renderQuotaCell = (sub, t) => {
  const total = Number(sub?.amount_total || 0);
  const used = Math.max(0, Number(sub?.amount_used || 0));
  if (total <= 0) {
    return (
      <Space spacing={4}>
        <Text>{renderQuota(used)}</Text>
        <Tag color='green' size='small'>
          {t('不限')}
        </Tag>
      </Space>
    );
  }
  const percent = Math.max(0, Math.min(100, (used / total) * 100));
  return (
    <div className='min-w-[150px]'>
      <div className='text-xs mb-1'>
        {renderQuota(used)} / {renderQuota(total)}
      </div>
      <Progress percent={percent} showInfo={false} size='small' />
    </div>
  );
};

const UserSubscriptionsPage = () => {
  const { t } = useTranslation();
  const isMobile = useIsMobile();
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [loading, setLoading] = useState(false);
  const [resettingId, setResettingId] = useState(null);
  const [resettingAll, setResettingAll] = useState(false);
  const [formApi, setFormApi] = useState(null);
  const [filters, setFilters] = useState({});

  const normalizeFilters = (values = {}) => {
    const userId = String(values.user_id || '').trim();
    const username = String(values.username || '').trim();
    const email = String(values.email || '').trim();
    const registerRange = values.register_range || [];
    return {
      user_id: userId,
      username,
      email,
      created_start_at: toUnix(registerRange?.[0]),
      created_end_at: toUnix(registerRange?.[1]),
    };
  };

  const buildFilterParams = (nextFilters = filters) => {
    const params = {};
    if (nextFilters.user_id) {
      params.user_id = nextFilters.user_id;
    }
    if (nextFilters.username) {
      params.username = nextFilters.username;
    }
    if (nextFilters.email) {
      params.email = nextFilters.email;
    }
    if (nextFilters.created_start_at > 0) {
      params.created_start_at = nextFilters.created_start_at;
    }
    if (nextFilters.created_end_at > 0) {
      params.created_end_at = nextFilters.created_end_at;
    }
    return params;
  };

  const buildParams = (page, size, nextFilters = filters) => ({
    p: page,
    page_size: size,
    ...buildFilterParams(nextFilters),
  });

  const loadData = async ({
    page = activePage,
    size = pageSize,
    nextFilters = filters,
  } = {}) => {
    setLoading(true);
    try {
      const res = await API.get('/api/subscription/admin/active_users', {
        params: buildParams(page, size, nextFilters),
      });
      if (res.data?.success) {
        const data = res.data.data || {};
        setItems(data.items || []);
        setTotal(Number(data.total || 0));
        setActivePage(Number(data.page || page));
        setPageSize(Number(data.page_size || size));
      } else {
        showError(res.data?.message || t('加载失败'));
      }
    } catch (e) {
      showError(t('请求失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData({ page: 1, size: pageSize });
  }, []);

  const handlePageChange = (page) => {
    setActivePage(page);
    loadData({ page, size: pageSize });
  };

  const handlePageSizeChange = (size) => {
    setPageSize(size);
    setActivePage(1);
    loadData({ page: 1, size });
  };

  const searchSubscriptions = () => {
    const nextFilters = normalizeFilters(formApi?.getValues?.() || {});
    setFilters(nextFilters);
    setActivePage(1);
    loadData({ page: 1, size: pageSize, nextFilters });
  };

  const resetFilters = () => {
    formApi?.reset();
    const nextFilters = {};
    setFilters(nextFilters);
    setActivePage(1);
    loadData({ page: 1, size: pageSize, nextFilters });
  };

  const resetSubscriptionUsed = (item) => {
    const sub = item?.subscription || {};
    Modal.confirm({
      title: t('确认重置'),
      content: t('将把该订阅的已用额度清零，总额度快照保持不变。是否继续？'),
      centered: true,
      onOk: async () => {
        setResettingId(sub.id);
        try {
          const res = await API.post(
            `/api/subscription/admin/user_subscriptions/${sub.id}/reset_used`,
          );
          if (res.data?.success) {
            showSuccess(t('重置成功'));
            await loadData({ page: activePage, size: pageSize });
          } else {
            showError(res.data?.message || t('重置失败'));
          }
        } catch (e) {
          showError(t('请求失败'));
        } finally {
          setResettingId(null);
        }
      },
    });
  };

  const resetAllSubscriptionUsed = () => {
    const nextFilters = normalizeFilters(formApi?.getValues?.() || {});
    Modal.confirm({
      title: t('确认重置所有'),
      content: t(
        '将把当前查询条件下所有有效套餐的已用额度清零，总额度快照保持不变。该操作可能耗时较长，是否继续？',
      ),
      centered: true,
      onOk: async () => {
        setResettingAll(true);
        try {
          const res = await API.post(
            '/api/subscription/admin/user_subscriptions/reset_used/bulk',
            null,
            {
              params: buildFilterParams(nextFilters),
            },
          );
          if (res.data?.success) {
            const count = Number(res.data?.data?.count || 0);
            showSuccess(t('重置完成，共处理 {{count}} 个套餐', { count }));
            setFilters(nextFilters);
            setActivePage(1);
            await loadData({ page: 1, size: pageSize, nextFilters });
          } else {
            showError(res.data?.message || t('重置失败'));
          }
        } catch (e) {
          showError(t('请求失败'));
        } finally {
          setResettingAll(false);
        }
      },
    });
  };

  const invalidateSubscription = (subId) => {
    Modal.confirm({
      title: t('确认作废'),
      content: t('作废后该订阅将立即失效，历史记录不受影响。是否继续？'),
      centered: true,
      onOk: async () => {
        try {
          const res = await API.post(
            `/api/subscription/admin/user_subscriptions/${subId}/invalidate`,
          );
          if (res.data?.success) {
            const msg = res.data?.data?.message;
            showSuccess(msg ? msg : t('已作废'));
            await loadData({ page: activePage, size: pageSize });
          } else {
            showError(res.data?.message || t('操作失败'));
          }
        } catch (e) {
          showError(t('请求失败'));
        }
      },
    });
  };

  const deleteSubscription = (subId) => {
    Modal.confirm({
      title: t('确认删除'),
      content: t('删除会彻底移除该订阅记录（含权益明细）。是否继续？'),
      centered: true,
      okType: 'danger',
      onOk: async () => {
        try {
          const res = await API.delete(
            `/api/subscription/admin/user_subscriptions/${subId}`,
          );
          if (res.data?.success) {
            const msg = res.data?.data?.message;
            showSuccess(msg ? msg : t('已删除'));
            await loadData({ page: activePage, size: pageSize });
          } else {
            showError(res.data?.message || t('删除失败'));
          }
        } catch (e) {
          showError(t('请求失败'));
        }
      },
    });
  };

  const expandedRowRender = (record) => {
    const subscriptions = record?.subscriptions || [];
    if (subscriptions.length === 0) {
      return <Empty description={t('暂无订阅记录')} />;
    }

    return (
      <div className='overflow-x-auto py-2'>
        <div
          className='grid gap-2 px-3 py-2 text-xs font-medium'
          style={{
            minWidth: 1190,
            color: 'var(--semi-color-text-2)',
            gridTemplateColumns:
              'minmax(180px, 1.4fr) 120px 170px 190px 190px 90px 240px',
          }}
        >
          <div>{t('套餐')}</div>
          <div>{t('额度')}</div>
          <div>{t('总额度')}</div>
          <div>{t('开通时间')}</div>
          <div>{t('到期时间')}</div>
          <div>{t('重置次数')}</div>
          <div className='text-right'>{t('操作')}</div>
        </div>
        <div className='flex flex-col gap-2'>
          {subscriptions.map((item) => {
            const sub = item?.subscription || {};
            const plan = item?.plan || {};
            return (
              <div
                key={sub.id}
                className='grid gap-2 items-center px-3 py-3 rounded-lg'
                style={{
                  minWidth: 1190,
                  background: 'var(--semi-color-fill-0)',
                  gridTemplateColumns:
                    'minmax(180px, 1.4fr) 120px 170px 190px 190px 90px 240px',
                }}
              >
                <div className='min-w-0'>
                  <div className='font-medium truncate'>
                    {plan.title || `#${sub.plan_id || '-'}`}
                  </div>
                  <div className='text-xs text-gray-500'>
                    ID: {sub.id} · {t('来源')}: {sub.source || '-'}
                  </div>
                </div>
                <div>
                  <Tag color='green' shape='circle' size='small'>
                    {t('生效')}
                  </Tag>
                </div>
                <div>{renderQuotaCell(sub, t)}</div>
                <div className='text-xs'>{formatTs(sub.start_time)}</div>
                <div className='text-xs'>{formatTs(sub.end_time)}</div>
                <div className='text-xs'>{sub.reset_count || 0}</div>
                <div className='text-right'>
                  <Space>
                    <Button
                      size='small'
                      type='warning'
                      theme='light'
                      onClick={() => invalidateSubscription(sub.id)}
                    >
                      {t('作废')}
                    </Button>
                    <Button
                      size='small'
                      type='danger'
                      theme='light'
                      onClick={() => deleteSubscription(sub.id)}
                    >
                      {t('删除')}
                    </Button>
                    <Tooltip content={t('清零已用额度')}>
                      <Button
                        size='small'
                        type='primary'
                        theme='light'
                        icon={<IconRefresh />}
                        loading={resettingId === sub.id}
                        onClick={() => resetSubscriptionUsed(item)}
                      >
                        {t('重置')}
                      </Button>
                    </Tooltip>
                  </Space>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    );
  };

  const columns = useMemo(
    () => [
      {
        title: 'ID',
        key: 'id',
        width: 90,
        render: (_, record) => record?.user?.id || '-',
      },
      {
        title: t('用户'),
        key: 'user',
        width: 280,
        render: (_, record) => {
          const user = record?.user || {};
          const displayName = user.display_name || user.username || '-';
          return (
            <div className='flex items-center gap-2'>
              <Avatar size='small' color='light-blue'>
                {displayName.slice(0, 1).toUpperCase()}
              </Avatar>
              <div className='min-w-0'>
                <div className='font-medium truncate'>{displayName}</div>
                <div className='text-xs text-gray-500 truncate'>
                  @{user.username || '-'}
                </div>
                <div className='text-xs text-gray-500 truncate'>
                  {user.email || '-'}
                </div>
              </div>
            </div>
          );
        },
      },
      {
        title: t('分组'),
        dataIndex: ['user', 'group'],
        width: 120,
        render: (value) => (
          <Tag color='white' shape='circle' size='small'>
            {value || '-'}
          </Tag>
        ),
      },
      {
        title: t('注册时间'),
        key: 'created_at',
        width: 180,
        render: (_, record) => (
          <Text size='small'>{formatTs(record?.user?.created_at)}</Text>
        ),
      },
      {
        title: t('有效套餐'),
        key: 'subscription_count',
        width: 120,
        render: (_, record) => (
          <Tag color='blue' shape='circle'>
            {t('{{count}} 个', {
              count: (record?.subscriptions || []).length,
            })}
          </Tag>
        ),
      },
      {
        title: t('额度汇总'),
        key: 'quota_summary',
        render: (_, record) => {
          const summary = getQuotaSummary(record?.subscriptions || []);
          if (summary.unlimited > 0) {
            return (
              <Space spacing={4}>
                <Text>
                  {renderQuota(summary.used)} / {renderQuota(summary.total)}
                </Text>
                <Tag color='green' size='small'>
                  {t('含不限')}
                </Tag>
              </Space>
            );
          }
          return (
            <Text>
              {renderQuota(summary.used)} / {renderQuota(summary.total)}
            </Text>
          );
        },
      },
    ],
    [t],
  );

  return (
    <CardPro
      type='type1'
      descriptionArea={
        <div className='flex flex-col md:flex-row justify-between items-start md:items-center gap-2 w-full'>
          <div className='flex items-center text-blue-500'>
            <CalendarClock size={16} className='mr-2' />
            <Text>{t('用户订阅')}</Text>
          </div>
        </div>
      }
      actionsArea={
        <Space>
          <Button
            type='primary'
            theme='light'
            icon={<IconRefresh />}
            loading={resettingAll}
            disabled={loading}
            onClick={resetAllSubscriptionUsed}
          >
            {t('重置所有')}
          </Button>
          <Button icon={<IconRefresh />} onClick={() => loadData()}>
            {t('刷新')}
          </Button>
        </Space>
      }
      searchArea={
        <Form
          getFormApi={(api) => setFormApi(api)}
          onSubmit={searchSubscriptions}
          allowEmpty={true}
          autoComplete='off'
          layout='vertical'
          trigger='change'
          stopValidateWithError={false}
        >
          <div className='flex flex-col gap-2'>
            <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-2'>
              <Form.Input
                field='user_id'
                prefix={<IconSearch />}
                placeholder={t('用户ID')}
                showClear
                pure
                size='small'
              />
              <Form.Input
                field='username'
                prefix={<IconSearch />}
                placeholder={t('用户名')}
                showClear
                pure
                size='small'
              />
              <Form.Input
                field='email'
                prefix={<IconSearch />}
                placeholder={t('邮箱')}
                showClear
                pure
                size='small'
              />
              <Form.DatePicker
                field='register_range'
                className='w-full'
                type='dateTimeRange'
                placeholder={[t('注册起始时间'), t('注册截止时间')]}
                showClear
                pure
                size='small'
              />
            </div>
            <div className='flex justify-end gap-2'>
              <Button
                type='tertiary'
                htmlType='submit'
                loading={loading}
                size='small'
              >
                {t('查询')}
              </Button>
              <Button type='tertiary' onClick={resetFilters} size='small'>
                {t('重置')}
              </Button>
            </div>
          </div>
        </Form>
      }
      paginationArea={createCardProPagination({
        currentPage: activePage,
        pageSize,
        total,
        onPageChange: handlePageChange,
        onPageSizeChange: handlePageSizeChange,
        isMobile,
        t,
      })}
      t={t}
    >
      <CardTable
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey={(record) => record?.user?.id}
        pagination={false}
        expandedRowRender={expandedRowRender}
        rowExpandable={(record) => (record?.subscriptions || []).length > 0}
        empty={<Empty description={t('暂无有效订阅用户')} />}
      />
    </CardPro>
  );
};

export default UserSubscriptionsPage;
