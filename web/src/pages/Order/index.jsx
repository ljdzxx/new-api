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
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { Coins } from 'lucide-react';
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
};

const isSubscriptionTopup = (record) => {
  const tradeNo = (record?.trade_no || '').toLowerCase();
  return Number(record?.amount || 0) === 0 && tradeNo.startsWith('sub');
};

const Order = () => {
  const { t } = useTranslation();
  const [formApi, setFormApi] = useState(null);
  const [orders, setOrders] = useState([]);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [total, setTotal] = useState(0);

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
    setPage(1);
    setTimeout(() => loadOrders(1, pageSize), 0);
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
        >
          <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-7 gap-2'>
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
            <div className='flex gap-2'>
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
    </div>
  );
};

export default Order;
