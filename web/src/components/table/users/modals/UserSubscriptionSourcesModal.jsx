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
import { Badge, Button, Empty, Modal, Space, Tag, Typography } from '@douyinfe/semi-ui';
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

const renderSourceTag = (source, t) => {
  switch (source) {
    case 'admin':
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('管理员赠送')}
        </Tag>
      );
    case 'redemption':
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('兑换码开通')}
        </Tag>
      );
    case 'order':
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('支付购买')}
        </Tag>
      );
    default:
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('未知来源')}
        </Tag>
      );
  }
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
    default:
      return (
        <Tag color='white' shape='circle' size='small'>
          {t('状态未知')}
        </Tag>
      );
  }
};

const emptyNode = (
  <Empty
    image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
    darkModeImage={
      <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
    }
    description='暂无订阅来源记录'
    style={{ padding: 30 }}
  />
);

const UserSubscriptionSourcesModal = ({ visible, onCancel, user, t }) => {
  const isMobile = useIsMobile();
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [items, setItems] = useState([]);
  const [currentPage, setCurrentPage] = useState(1);
  const [total, setTotal] = useState(0);
  const pageSize = 10;

  const loadData = async (page = 1) => {
    if (!user?.id) {
      return;
    }
    setLoading(true);
    try {
      const params = new URLSearchParams({
        p: String(page),
        page_size: String(pageSize),
      });
      const res = await API.get(
        `/api/user/${user.id}/subscription_sources?${params.toString()}`,
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
    loadData(1);
  }, [visible, user?.id]);

  const columns = useMemo(
    () => [
      {
        title: t('来源'),
        key: 'source',
        width: 130,
        render: (_, record) => renderSourceTag(record?.source, t),
      },
      {
        title: t('来源详情'),
        key: 'source_detail',
        width: 170,
        render: (_, record) => record?.source_detail || '-',
      },
      {
        title: t('套餐'),
        key: 'plan_title',
        width: 180,
        render: (_, record) => record?.plan_title || `#${record?.plan_id || '-'}`,
      },
      {
        title: t('套餐开始时间'),
        dataIndex: 'start_time',
        width: 170,
        render: (text) => renderTime(text),
      },
      {
        title: t('套餐结束时间'),
        dataIndex: 'end_time',
        width: 170,
        render: (text) => renderTime(text),
      },
      {
        title: t('开通时间'),
        dataIndex: 'created_at',
        width: 170,
        render: (text) => renderTime(text),
      },
      {
        title: t('套餐状态'),
        dataIndex: 'status',
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
            {t('订阅来源')}
          </Tag>
          <Typography.Title heading={4} className='m-0'>
            {t('用户订阅来源记录')}
          </Typography.Title>
          <Text type='tertiary'>
            {user?.username || '-'} (ID: {user?.id || '-'})
          </Text>
        </Space>
      }
      footer={null}
      onCancel={onCancel}
      width={isMobile ? '100%' : 1120}
      centered
    >
      <div className='flex items-center justify-between gap-3 flex-wrap mb-3'>
        <Text type='tertiary'>
          {t('展示该用户全部订阅的开通来源，包括管理员赠送、兑换码开通和支付购买')}
        </Text>
        <Button
          icon={<IconRefresh />}
          loading={refreshing}
          onClick={() => {
            setRefreshing(true);
            loadData(currentPage);
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
          onPageChange: (page) => loadData(page),
        }}
        empty={emptyNode}
      />
    </Modal>
  );
};

export default UserSubscriptionSourcesModal;
