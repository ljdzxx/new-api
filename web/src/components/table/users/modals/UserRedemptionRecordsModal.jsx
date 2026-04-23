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
  Modal,
  Select,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { IconRefresh } from '@douyinfe/semi-icons';
import CardTable from '../../../common/ui/CardTable';
import { API, showError, timestamp2string } from '../../../../helpers';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';

const { Text } = Typography;

const renderTime = (value) => {
  const ts = Number(value || 0);
  if (ts <= 0) {
    return '-';
  }
  return timestamp2string(ts);
};

const renderStatusTag = (status, t) => {
  switch (status) {
    case 'active':
      return (
        <Tag
          color='white'
          shape='circle'
          size='small'
          prefixIcon={<Badge dot type='success' />}
        >
          {t('生效')}
        </Tag>
      );
    case 'cancelled':
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('已作废')}
        </Tag>
      );
    case 'expired':
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('已过期')}
        </Tag>
      );
    case 'quota':
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('额度兑换')}
        </Tag>
      );
    case 'unknown':
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('状态未知')}
        </Tag>
      );
    default:
      return <span>-</span>;
  }
};

const renderSourceTag = (rewardType, t) => {
  if (Number(rewardType || 0) === 1) {
    return (
      <Tag color='white' shape='circle' size='small'>
        {t('额度兑换')}
      </Tag>
    );
  }
  return (
    <Tag color='white' shape='circle' size='small'>
      {t('兑换订阅')}
    </Tag>
  );
};

const emptyNode = (
  <Empty
    image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
    darkModeImage={
      <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
    }
    description='暂无兑换记录'
    style={{ padding: 30 }}
  />
);

const UserRedemptionRecordsModal = ({ visible, onCancel, user, t }) => {
  const isMobile = useIsMobile();
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [items, setItems] = useState([]);
  const [currentPage, setCurrentPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [statusFilter, setStatusFilter] = useState('all');
  const pageSize = 10;

  const loadData = async (page = 1, nextStatus = statusFilter) => {
    if (!user?.id) {
      return;
    }
    setLoading(true);
    try {
      const params = new URLSearchParams({
        p: String(page),
        page_size: String(pageSize),
      });
      if (nextStatus && nextStatus !== 'all') {
        params.set('status', nextStatus);
      }
      const res = await API.get(
        `/api/user/${user.id}/redemptions?${params.toString()}`,
      );
      const { success, message, data } = res.data;
      if (!success) {
        showError(message || t('加载失败'));
        return;
      }
      setItems(data?.items || []);
      setCurrentPage(data?.page || page);
      setTotal(data?.total || 0);
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
    setStatusFilter('all');
    loadData(1, 'all');
  }, [visible, user?.id]);

  const columns = useMemo(
    () => [
      {
        title: t('兑换码'),
        dataIndex: 'key',
        width: 200,
      },
      {
        title: t('套餐'),
        key: 'plan_title',
        width: 180,
        render: (_, record) =>
          Number(record?.reward_type || 0) === 1
            ? t('额度兑换')
            : record?.plan_title || '-',
      },
      {
        title: t('来源'),
        key: 'source',
        width: 120,
        render: (_, record) => renderSourceTag(record?.reward_type, t),
      },
      {
        title: t('套餐开始时间'),
        dataIndex: 'subscription_start_time',
        width: 170,
        render: (text) => renderTime(text),
      },
      {
        title: t('套餐结束时间'),
        dataIndex: 'subscription_end_time',
        width: 170,
        render: (text) => renderTime(text),
      },
      {
        title: t('兑换时间'),
        dataIndex: 'redeemed_time',
        width: 170,
        render: (text) => renderTime(text),
      },
      {
        title: t('套餐状态'),
        dataIndex: 'subscription_status',
        width: 120,
        render: (text) => renderStatusTag(text, t),
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
            {t('兑换记录')}
          </Tag>
          <Typography.Title heading={4} className='m-0'>
            {t('用户兑换记录')}
          </Typography.Title>
          <Text type='tertiary'>
            {user?.username || '-'} (ID: {user?.id || '-'})
          </Text>
        </Space>
      }
      footer={null}
      onCancel={onCancel}
      width={isMobile ? '100%' : 1080}
      centered
    >
      <div className='flex items-center justify-between gap-3 flex-wrap mb-3'>
        <Space wrap>
          <Text type='tertiary'>
            {t('该用户累计已使用兑换码')} {Number(user?.redemption_count || 0)}{' '}
            {t('个')}
          </Text>
          <Select
            value={statusFilter}
            style={{ width: 180 }}
            onChange={(value) => {
              const nextStatus = value || 'all';
              setStatusFilter(nextStatus);
              loadData(1, nextStatus);
            }}
            optionList={[
              { label: t('全部状态'), value: 'all' },
              { label: t('生效'), value: 'active' },
              { label: t('已过期'), value: 'expired' },
              { label: t('已作废'), value: 'cancelled' },
              { label: t('额度兑换'), value: 'quota' },
              { label: t('状态未知'), value: 'unknown' },
            ]}
          />
        </Space>
        <Button
          icon={<IconRefresh />}
          loading={refreshing}
          onClick={() => {
            setRefreshing(true);
            loadData(currentPage, statusFilter);
          }}
        >
          {t('刷新')}
        </Button>
      </div>
      <CardTable
        columns={columns}
        dataSource={items}
        rowKey={(row) => row?.id}
        loading={loading}
        scroll={{ x: 'max-content' }}
        hidePagination={false}
        pagination={{
          currentPage,
          pageSize,
          total,
          pageSizeOpts: [10, 20, 50],
          showSizeChanger: false,
          onPageChange: (page) => loadData(page, statusFilter),
        }}
        empty={emptyNode}
      />
    </Modal>
  );
};

export default UserRedemptionRecordsModal;
