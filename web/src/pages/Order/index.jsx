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
  Badge,
  Button,
  Empty,
  Form,
  Modal,
  Table,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { Coins, History, ListChecks, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import CardTable from '../../components/common/ui/CardTable';
import { API, timestamp2string } from '../../helpers';
import { DATE_RANGE_PRESETS } from '../../constants/console.constants';

const { Text } = Typography;

const STATUS_CONFIG = {
  success: { type: 'success', key: '成功' },
  pending: { type: 'warning', key: '待支付' },
  expired: { type: 'danger', key: '已过期' },
};

const PAYMENT_METHOD_MAP = {
  stripe: 'Stripe',
  creem: 'Creem',
  alipay: '支付宝',
  wxpay: '微信',
  mall: '商城',
  redemption: '兑换码',
  aff: '邀请奖励',
  aff_inviter: '拉新奖励',
  aff_invitee: '受邀奖励',
};

const PAYMENT_PROVIDER_MAP = {
  epay: 'Epay',
  stripe: 'Stripe',
  creem: 'Creem',
  mall: '商城',
  promotion: '活动',
};

const RECONCILE_STATUS_CONFIG = {
  unchecked: { color: 'grey', key: '未对账' },
  normal: { color: 'green', key: '正常' },
  abnormal: { color: 'red', key: '异常' },
};

const RECONCILE_JOB_STATUS_CONFIG = {
  pending: { color: 'grey', key: '待执行' },
  running: { color: 'blue', key: '执行中' },
  success: { color: 'green', key: '完成' },
  failed: { color: 'red', key: '失败' },
  partial: { color: 'orange', key: '部分异常' },
};

const isSubscriptionTopup = (record) => {
  const tradeNo = (record?.trade_no || '').toLowerCase();
  return Number(record?.amount || 0) === 0 && tradeNo.startsWith('sub');
};

const getDefaultDateRange = () => {
  const start = new Date();
  start.setHours(0, 0, 0, 0);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return [start, end];
};

const Order = () => {
  const { t } = useTranslation();
  const [formApi, setFormApi] = useState(null);
  const [orders, setOrders] = useState([]);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [total, setTotal] = useState(0);
  const [reconcileJobsVisible, setReconcileJobsVisible] = useState(false);
  const [reconcileJobsLoading, setReconcileJobsLoading] = useState(false);
  const [reconcileJobs, setReconcileJobs] = useState([]);
  const [reconcileItemsVisible, setReconcileItemsVisible] = useState(false);
  const [reconcileItemsLoading, setReconcileItemsLoading] = useState(false);
  const [reconcileItems, setReconcileItems] = useState([]);
  const [currentReconcileJob, setCurrentReconcileJob] = useState(null);

  const buildQuery = (currentPage, currentPageSize) => {
    const values = formApi?.getValues?.() || {};
    const params = new URLSearchParams({
      p: String(currentPage),
      page_size: String(currentPageSize),
    });

    const appendText = (key) => {
      const value = String(values[key] || '').trim();
      if (value) params.set(key, value);
    };

    appendText('keyword');
    appendText('user_id');
    appendText('username');
    appendText('status');
    appendText('payment_provider');
    appendText('payment_method');
    appendText('reconcile_status');

    if (Array.isArray(values.dateRange) && values.dateRange.length === 2) {
      const startTimestamp = Math.floor(Date.parse(values.dateRange[0]) / 1000);
      const endTimestamp = Math.floor(Date.parse(values.dateRange[1]) / 1000);
      if (!Number.isNaN(startTimestamp)) {
        params.set('start_timestamp', String(startTimestamp));
      }
      if (!Number.isNaN(endTimestamp)) {
        params.set('end_timestamp', String(endTimestamp));
      }
    }

    return params.toString();
  };

  const buildReconcilePayload = () => {
    const values = formApi?.getValues?.() || {};
    const payload = {};

    [
      'keyword',
      'username',
      'status',
      'payment_provider',
      'payment_method',
      'reconcile_status',
    ].forEach((key) => {
      const value = String(values[key] || '').trim();
      if (value) payload[key] = value;
    });

    const userId = Number(values.user_id || 0);
    if (userId > 0) {
      payload.user_id = userId;
    }

    if (Array.isArray(values.dateRange) && values.dateRange.length === 2) {
      const startTimestamp = Math.floor(Date.parse(values.dateRange[0]) / 1000);
      const endTimestamp = Math.floor(Date.parse(values.dateRange[1]) / 1000);
      if (!Number.isNaN(startTimestamp)) {
        payload.start_timestamp = startTimestamp;
      }
      if (!Number.isNaN(endTimestamp)) {
        payload.end_timestamp = endTimestamp;
      }
    }

    return payload;
  };

  const loadOrders = async (currentPage = page, currentPageSize = pageSize) => {
    setLoading(true);
    try {
      const res = await API.get(
        `/api/user/topup?${buildQuery(currentPage, currentPageSize)}`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setOrders(data.items || []);
        setTotal(data.total || 0);
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } catch (error) {
      Toast.error({ content: t('加载订单失败') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!formApi) {
      return;
    }
    loadOrders(1, pageSize);
  }, [formApi]);

  const handleSearch = () => {
    setPage(1);
    loadOrders(1, pageSize);
  };

  const handleReset = () => {
    formApi?.reset();
    formApi?.setValues?.({ dateRange: getDefaultDateRange() });
    setPage(1);
    setTimeout(() => loadOrders(1, pageSize), 0);
  };

  const loadReconcileJobs = async () => {
    setReconcileJobsLoading(true);
    try {
      const res = await API.get(
        '/api/user/topup/reconcile/jobs?p=1&page_size=50',
      );
      const { success, message, data } = res.data;
      if (success) {
        setReconcileJobs(data.items || []);
      } else {
        Toast.error({ content: message || t('加载对账记录失败') });
      }
    } catch (error) {
      Toast.error({ content: t('加载对账记录失败') });
    } finally {
      setReconcileJobsLoading(false);
    }
  };

  const showReconcileJobs = async () => {
    setReconcileJobsVisible(true);
    await loadReconcileJobs();
  };

  const loadReconcileItems = async (job) => {
    if (!job?.id) return;
    setCurrentReconcileJob(job);
    setReconcileItemsVisible(true);
    setReconcileItemsLoading(true);
    try {
      const res = await API.get(
        `/api/user/topup/reconcile/jobs/${job.id}/items?p=1&page_size=100`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setReconcileItems(data.items || []);
      } else {
        Toast.error({ content: message || t('加载对账明细失败') });
      }
    } catch (error) {
      Toast.error({ content: t('加载对账明细失败') });
    } finally {
      setReconcileItemsLoading(false);
    }
  };

  const handleAdminComplete = async (tradeNo) => {
    try {
      const res = await API.post('/api/user/topup/complete', {
        trade_no: tradeNo,
      });
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('补单成功') });
        await loadOrders(page, pageSize);
        await loadReconcileJobs();
      } else {
        Toast.error({ content: message || t('补单失败') });
      }
    } catch (error) {
      Toast.error({ content: t('补单失败') });
    }
  };

  const confirmAdminComplete = (tradeNo) => {
    Modal.confirm({
      title: t('确认补单'),
      content: t('是否将该订单标记为成功并为用户入账？'),
      onOk: () => handleAdminComplete(tradeNo),
    });
  };

  const handleStartEpayReconcile = async () => {
    try {
      const res = await API.post(
        '/api/user/topup/epay/reconcile',
        buildReconcilePayload(),
      );
      const { success, message, data } = res.data;
      if (success) {
        Toast.success({
          content: t('Epay对账任务已启动，待处理订单 {{count}} 笔', {
            count: data?.total_count ?? 0,
          }),
        });
        await loadOrders(page, pageSize);
      } else {
        Toast.error({ content: message || t('Epay对账启动失败') });
      }
    } catch (error) {
      Toast.error({ content: t('Epay对账启动失败') });
    }
  };

  const confirmStartEpayReconcile = () => {
    Modal.confirm({
      title: t('确认启动Epay对账'),
      content: t(
        '将在当前查询条件基础上，仅对Epay、支付成功、未对账订单发起平台查单核对。',
      ),
      onOk: handleStartEpayReconcile,
    });
  };

  const renderStatusBadge = (status) => {
    const config = STATUS_CONFIG[status] || { type: 'primary', key: status };
    return (
      <span className='flex items-center gap-2'>
        <Badge dot type={config.type} />
        <span>{t(config.key)}</span>
      </span>
    );
  };

  const renderPaymentMethod = (paymentMethod) => {
    const displayName = PAYMENT_METHOD_MAP[paymentMethod];
    return <Text>{displayName ? t(displayName) : paymentMethod || '-'}</Text>;
  };

  const renderPaymentProvider = (paymentProvider) => {
    const displayName = PAYMENT_PROVIDER_MAP[paymentProvider];
    return <Text>{displayName ? t(displayName) : paymentProvider || '-'}</Text>;
  };

  const renderReconcileStatus = (status) => {
    const config = RECONCILE_STATUS_CONFIG[status || 'unchecked'] || {
      color: 'blue',
      key: status,
    };
    return (
      <Tag color={config.color} shape='circle' size='small'>
        {t(config.key)}
      </Tag>
    );
  };

  const renderReconcileJobStatus = (status) => {
    const config = RECONCILE_JOB_STATUS_CONFIG[status] || {
      color: 'blue',
      key: status,
    };
    return (
      <Tag color={config.color} shape='circle' size='small'>
        {t(config.key)}
      </Tag>
    );
  };

  const reconcileJobColumns = useMemo(
    () => [
      {
        title: t('任务ID'),
        dataIndex: 'id',
        key: 'id',
        width: 80,
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        render: renderReconcileJobStatus,
      },
      {
        title: t('处理进度'),
        key: 'progress',
        render: (_, record) =>
          `${record.checked_count || 0}/${record.total_count || 0}`,
      },
      {
        title: t('正常'),
        dataIndex: 'normal_count',
        key: 'normal_count',
      },
      {
        title: t('异常'),
        dataIndex: 'abnormal_count',
        key: 'abnormal_count',
      },
      {
        title: t('失败'),
        dataIndex: 'failed_count',
        key: 'failed_count',
      },
      {
        title: t('开始时间'),
        dataIndex: 'started_at',
        key: 'started_at',
        render: (time) => (time ? timestamp2string(time) : '-'),
      },
      {
        title: t('完成时间'),
        dataIndex: 'finished_at',
        key: 'finished_at',
        render: (time) => (time ? timestamp2string(time) : '-'),
      },
      {
        title: t('结果'),
        dataIndex: 'message',
        key: 'message',
        render: (text) => text || '-',
      },
      {
        title: t('操作'),
        key: 'action',
        fixed: 'right',
        width: 90,
        render: (_, record) => (
          <Button
            size='small'
            theme='outline'
            icon={<ListChecks size={14} />}
            onClick={() => loadReconcileItems(record)}
          >
            {t('明细')}
          </Button>
        ),
      },
    ],
    [t],
  );

  const reconcileItemColumns = useMemo(
    () => [
      {
        title: t('订单号'),
        dataIndex: 'trade_no',
        key: 'trade_no',
        render: (text) => <Text copyable>{text}</Text>,
      },
      {
        title: t('对账状态'),
        dataIndex: 'status',
        key: 'status',
        render: renderReconcileStatus,
      },
      {
        title: t('平台订单号'),
        dataIndex: 'remote_trade_no',
        key: 'remote_trade_no',
        render: (text) => text || '-',
      },
      {
        title: t('平台金额'),
        dataIndex: 'remote_money',
        key: 'remote_money',
        render: (text) => text || '-',
      },
      {
        title: t('平台状态'),
        dataIndex: 'remote_status',
        key: 'remote_status',
        render: (text) => text || '-',
      },
      {
        title: t('支付方式'),
        dataIndex: 'remote_type',
        key: 'remote_type',
        render: renderPaymentMethod,
      },
      {
        title: t('对账时间'),
        dataIndex: 'checked_at',
        key: 'checked_at',
        render: (time) => (time ? timestamp2string(time) : '-'),
      },
      {
        title: t('结果'),
        dataIndex: 'message',
        key: 'message',
        render: (text) => text || '-',
      },
    ],
    [t],
  );

  const columns = useMemo(
    () => [
      {
        title: t('订单号'),
        dataIndex: 'trade_no',
        key: 'trade_no',
        render: (text) => <Text copyable>{text}</Text>,
      },
      {
        title: t('用户ID'),
        dataIndex: 'user_id',
        key: 'user_id',
        width: 90,
      },
      {
        title: t('用户名'),
        dataIndex: 'username',
        key: 'username',
        render: (text) => text || '-',
      },
      {
        title: t('支付通道'),
        dataIndex: 'payment_provider',
        key: 'payment_provider',
        render: renderPaymentProvider,
      },
      {
        title: t('支付方式'),
        dataIndex: 'payment_method',
        key: 'payment_method',
        render: renderPaymentMethod,
      },
      {
        title: t('充值额度'),
        dataIndex: 'amount',
        key: 'amount',
        render: (amount, record) => {
          if (isSubscriptionTopup(record)) {
            return (
              <Tag color='purple' shape='circle' size='small'>
                {t('订阅套餐')}
              </Tag>
            );
          }
          return (
            <span className='flex items-center gap-1'>
              <Coins size={16} />
              <Text>{amount}</Text>
            </span>
          );
        },
      },
      {
        title: t('支付金额'),
        dataIndex: 'money',
        key: 'money',
        render: (money) => (
          <Text type='danger'>¥{Number(money || 0).toFixed(2)}</Text>
        ),
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        render: renderStatusBadge,
      },
      {
        title: t('对账状态'),
        dataIndex: 'reconcile_status',
        key: 'reconcile_status',
        render: renderReconcileStatus,
      },
      {
        title: t('创建时间'),
        dataIndex: 'create_time',
        key: 'create_time',
        render: (time) => timestamp2string(time),
      },
      {
        title: t('完成时间'),
        dataIndex: 'complete_time',
        key: 'complete_time',
        render: (time) => (time ? timestamp2string(time) : '-'),
      },
      {
        title: t('操作'),
        key: 'action',
        fixed: 'right',
        width: 90,
        render: (_, record) => {
          if (record.status !== 'pending') return null;
          return (
            <Button
              size='small'
              type='primary'
              theme='outline'
              onClick={() => confirmAdminComplete(record.trade_no)}
            >
              {t('补单')}
            </Button>
          );
        },
      },
    ],
    [t],
  );

  return (
    <div className='mt-[60px] px-2'>
      <div className='mb-4'>
        <Form
          getFormApi={setFormApi}
          onSubmit={handleSearch}
          allowEmpty
          autoComplete='off'
          layout='vertical'
          initValues={{ dateRange: getDefaultDateRange() }}
        >
          <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-8 gap-2'>
            <Form.Input
              field='keyword'
              prefix={<IconSearch />}
              placeholder={t('订单号')}
              showClear
              pure
              size='small'
            />
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
            <Form.Select
              field='status'
              placeholder={t('状态')}
              showClear
              pure
              size='small'
              optionList={[
                { label: t('成功'), value: 'success' },
                { label: t('待支付'), value: 'pending' },
                { label: t('已过期'), value: 'expired' },
              ]}
            />
            <Form.Select
              field='payment_provider'
              placeholder={t('支付通道')}
              showClear
              pure
              size='small'
              optionList={[
                { label: 'Epay', value: 'epay' },
                { label: 'Stripe', value: 'stripe' },
                { label: 'Creem', value: 'creem' },
                { label: t('商城'), value: 'mall' },
                { label: t('活动'), value: 'promotion' },
              ]}
            />
            <Form.Select
              field='payment_method'
              placeholder={t('支付方式')}
              showClear
              pure
              size='small'
              optionList={[
                { label: t('微信'), value: 'wxpay' },
                { label: t('支付宝'), value: 'alipay' },
                { label: t('兑换码'), value: 'redemption' },
                { label: 'Stripe', value: 'stripe' },
                { label: 'Creem', value: 'creem' },
                { label: t('商城'), value: 'mall' },
                { label: t('邀请奖励'), value: 'aff' },
                { label: t('拉新奖励'), value: 'aff_inviter' },
                { label: t('受邀奖励'), value: 'aff_invitee' },
              ]}
            />
            <Form.Select
              field='reconcile_status'
              placeholder={t('对账状态')}
              showClear
              pure
              size='small'
              optionList={[
                { label: t('未对账'), value: 'unchecked' },
                { label: t('正常'), value: 'normal' },
                { label: t('异常'), value: 'abnormal' },
              ]}
            />
            <div className='md:col-span-2 xl:col-span-2'>
              <Form.DatePicker
                field='dateRange'
                className='w-full'
                type='dateTimeRange'
                placeholder={[t('起始时间'), t('截止时间')]}
                showClear
                pure
                size='small'
                presets={DATE_RANGE_PRESETS.map((preset) => ({
                  text: t(preset.text),
                  start: preset.start(),
                  end: preset.end(),
                }))}
              />
            </div>
            <div className='flex flex-wrap gap-2 xl:col-span-2'>
              <Button
                htmlType='submit'
                type='primary'
                icon={<IconSearch />}
                loading={loading}
                size='small'
              >
                {t('查询')}
              </Button>
              <Button onClick={handleReset} size='small'>
                {t('重置')}
              </Button>
              <Button
                type='warning'
                theme='outline'
                icon={<RefreshCw size={14} />}
                onClick={confirmStartEpayReconcile}
                loading={loading}
                size='small'
              >
                {t('Epay对账')}
              </Button>
              <Button
                theme='outline'
                icon={<History size={14} />}
                onClick={showReconcileJobs}
                loading={reconcileJobsLoading}
                size='small'
              >
                {t('查看对账记录')}
              </Button>
            </div>
          </div>
        </Form>
      </div>

      <CardTable
        columns={columns}
        dataSource={orders}
        loading={loading}
        rowKey='id'
        scroll={{ x: 'max-content' }}
        pagination={{
          currentPage: page,
          pageSize,
          total,
          showSizeChanger: true,
          pageSizeOpts: [10, 20, 50, 100],
          onPageChange: (currentPage) => {
            setPage(currentPage);
            loadOrders(currentPage, pageSize);
          },
          onPageSizeChange: (currentPageSize) => {
            setPageSize(currentPageSize);
            setPage(1);
            loadOrders(1, currentPageSize);
          },
        }}
        empty={
          <Empty
            image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
            darkModeImage={
              <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
            }
            description={t('暂无订单记录')}
            style={{ padding: 30 }}
          />
        }
      />

      <Modal
        title={t('对账记录')}
        visible={reconcileJobsVisible}
        onCancel={() => setReconcileJobsVisible(false)}
        footer={null}
        width={1000}
      >
        <Table
          columns={reconcileJobColumns}
          dataSource={reconcileJobs}
          loading={reconcileJobsLoading}
          rowKey='id'
          size='small'
          pagination={false}
          scroll={{ x: 'max-content', y: 460 }}
          empty={t('暂无对账记录')}
        />
      </Modal>

      <Modal
        title={
          currentReconcileJob?.id
            ? t('对账明细 #{{id}}', { id: currentReconcileJob.id })
            : t('对账明细')
        }
        visible={reconcileItemsVisible}
        onCancel={() => setReconcileItemsVisible(false)}
        footer={null}
        width={1100}
      >
        <Table
          columns={reconcileItemColumns}
          dataSource={reconcileItems}
          loading={reconcileItemsLoading}
          rowKey='id'
          size='small'
          pagination={false}
          scroll={{ x: 'max-content', y: 460 }}
          empty={t('暂无对账明细')}
        />
      </Modal>
    </div>
  );
};

export default Order;
