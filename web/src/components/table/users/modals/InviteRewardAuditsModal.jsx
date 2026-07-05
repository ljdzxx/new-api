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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Form, Modal, Space, Table, Tag } from '@douyinfe/semi-ui';
import { API, showError, timestamp2string } from '../../../../helpers';

const statusColor = {
  granted: 'green',
  denied_risk: 'red',
  denied_daily_limit: 'orange',
  denied_policy: 'amber',
};

const InviteRewardAuditsModal = ({ visible, handleClose, t }) => {
  const formApiRef = useRef(null);
  const [loading, setLoading] = useState(false);
  const [audits, setAudits] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);

  const buildParams = (targetPage = page, targetPageSize = pageSize) => {
    const values = formApiRef.current?.getValues?.() || {};
    const params = new URLSearchParams({
      p: String(targetPage),
      page_size: String(targetPageSize),
    });
    [
      'inviter_id',
      'invitee_id',
      'reward_status',
      'min_risk_score',
      'max_risk_score',
    ].forEach((key) => {
      const value = values[key];
      if (value !== undefined && value !== null && value !== '') {
        params.set(key, String(value));
      }
    });
    return params;
  };

  const loadAudits = async (targetPage = page, targetPageSize = pageSize) => {
    setLoading(true);
    try {
      const res = await API.get(
        `/api/user/invite_reward_audits?${buildParams(
          targetPage,
          targetPageSize,
        ).toString()}`,
      );
      if (res.data?.success) {
        setAudits(res.data?.data?.items || []);
        setTotal(res.data?.data?.total || 0);
        setPage(targetPage);
        setPageSize(targetPageSize);
      } else {
        showError(res.data?.message || t('查询失败'));
      }
    } catch (err) {
      showError(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (visible) {
      loadAudits(1, pageSize);
    }
  }, [visible]);

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    { title: t('邀请人'), dataIndex: 'inviter_id', width: 90 },
    { title: t('受邀用户'), dataIndex: 'invitee_id', width: 90 },
    {
      title: t('风控分'),
      dataIndex: 'risk_score',
      width: 90,
      render: (value) => (
        <Tag color={value >= 60 ? 'red' : 'green'}>{value}</Tag>
      ),
    },
    {
      title: t('状态'),
      dataIndex: 'reward_status',
      width: 130,
      render: (value) => (
        <Tag color={statusColor[value] || 'grey'}>{t(value || '-')}</Tag>
      ),
    },
    {
      title: t('原因'),
      dataIndex: 'risk_reasons',
      width: 220,
      render: (value) => value || '-',
    },
    {
      title: t('当日前置有效邀请数'),
      dataIndex: 'daily_count_before',
      width: 150,
    },
    {
      title: t('创建时间'),
      dataIndex: 'created_at',
      width: 170,
      render: (value) => (value ? timestamp2string(value) : '-'),
    },
  ];

  return (
    <Modal
      title={t('邀请奖励风控审计')}
      visible={visible}
      onCancel={handleClose}
      footer={null}
      width={1100}
    >
      <Form
        layout='horizontal'
        getFormApi={(api) => {
          formApiRef.current = api;
        }}
      >
        <Space wrap>
          <Form.InputNumber
            field='inviter_id'
            label={t('邀请人')}
            placeholder='ID'
            style={{ width: 120 }}
          />
          <Form.InputNumber
            field='invitee_id'
            label={t('受邀用户')}
            placeholder='ID'
            style={{ width: 120 }}
          />
          <Form.Select
            field='reward_status'
            label={t('状态')}
            placeholder={t('全部')}
            showClear
            style={{ width: 170 }}
            optionList={[
              { label: t('granted'), value: 'granted' },
              { label: t('denied_risk'), value: 'denied_risk' },
              { label: t('denied_daily_limit'), value: 'denied_daily_limit' },
              { label: t('denied_policy'), value: 'denied_policy' },
            ]}
          />
          <Form.InputNumber
            field='min_risk_score'
            label={t('最低分')}
            min={0}
            max={100}
            style={{ width: 120 }}
          />
          <Form.InputNumber
            field='max_risk_score'
            label={t('最高分')}
            min={0}
            max={100}
            style={{ width: 120 }}
          />
          <Button type='primary' onClick={() => loadAudits(1, pageSize)}>
            {t('查询')}
          </Button>
        </Space>
      </Form>
      <Table
        className='mt-3'
        columns={columns}
        dataSource={audits}
        loading={loading}
        rowKey='id'
        pagination={{
          currentPage: page,
          pageSize,
          total,
          showSizeChanger: true,
          pageSizeOpts: [10, 20, 50, 100],
          onPageChange: (nextPage) => loadAudits(nextPage, pageSize),
          onPageSizeChange: (nextPageSize) => loadAudits(1, nextPageSize),
        }}
      />
    </Modal>
  );
};

export default InviteRewardAuditsModal;
