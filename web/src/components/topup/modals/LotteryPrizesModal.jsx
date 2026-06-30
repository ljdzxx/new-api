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
import { Empty, Modal, Table, Tag, Toast, Typography } from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { API, timestamp2string } from '../../../helpers';
import {
  REDEMPTION_CODE_TYPES,
  REDEMPTION_STATUS,
} from '../../../constants/redemption.constants';
import { useIsMobile } from '../../../hooks/common/useIsMobile';

const { Text } = Typography;

const codeTypeMap = {
  [REDEMPTION_CODE_TYPES.NORMAL]: { text: '普通兑换码', color: 'blue' },
  [REDEMPTION_CODE_TYPES.RESET]: { text: '重置兑换码', color: 'orange' },
};

const LotteryPrizesModal = ({ visible, onCancel, t }) => {
  const [loading, setLoading] = useState(false);
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const isMobile = useIsMobile();

  const loadPrizes = async (currentPage, currentPageSize) => {
    setLoading(true);
    try {
      const res = await API.get(
        `/api/lottery/prizes?p=${currentPage}&page_size=${currentPageSize}`,
      );
      const { success, message, data } = res.data;
      if (!success) {
        Toast.error({ content: message || t('加载失败') });
        return;
      }
      setItems(data?.items || []);
      setTotal(data?.total || 0);
    } catch (e) {
      Toast.error({ content: t('加载奖品失败') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (visible) {
      loadPrizes(page, pageSize);
    }
  }, [visible, page, pageSize]);

  const columns = useMemo(
    () => [
      {
        title: t('抽奖期数'),
        dataIndex: 'issue',
        key: 'issue',
        width: 100,
        render: (text) => `#${text || '-'}`,
      },
      {
        title: t('抽奖标题'),
        dataIndex: 'title',
        key: 'title',
        width: 180,
        render: (text) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 170 }}>
            {text || '-'}
          </Text>
        ),
      },
      {
        title: t('奖品名称'),
        dataIndex: 'prize_name',
        key: 'prize_name',
        width: 130,
        render: (text) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 120 }}>
            {text || '-'}
          </Text>
        ),
      },
      {
        title: t('兑换码类型'),
        dataIndex: 'code_type',
        key: 'code_type',
        width: 120,
        render: (value) => {
          const config = codeTypeMap[Number(value)] || codeTypeMap[1];
          return (
            <Tag color={config.color} shape='circle'>
              {t(config.text)}
            </Tag>
          );
        },
      },
      {
        title: t('兑换码'),
        dataIndex: 'code',
        key: 'code',
        width: 260,
        render: (text) => (
          <Text
            copyable={text ? { content: text } : false}
            ellipsis={{ showTooltip: true }}
            style={{ maxWidth: 250 }}
          >
            {text || '-'}
          </Text>
        ),
      },
      {
        title: t('状态'),
        dataIndex: 'redemption_status',
        key: 'redemption_status',
        width: 95,
        render: (value) => {
          const used = Number(value) === REDEMPTION_STATUS.USED;
          return (
            <Tag color={used ? 'grey' : 'green'} shape='circle'>
              {t(used ? '已使用' : '未使用')}
            </Tag>
          );
        },
      },
      {
        title: t('中奖时间'),
        dataIndex: 'won_time',
        key: 'won_time',
        width: 155,
        render: (time) => timestamp2string(time),
      },
      {
        title: t('过期时间'),
        dataIndex: 'expired_time',
        key: 'expired_time',
        width: 155,
        render: (time) => (time ? timestamp2string(time) : t('永不过期')),
      },
    ],
    [t],
  );

  return (
    <Modal
      title={t('我的奖品')}
      visible={visible}
      onCancel={onCancel}
      footer={null}
      size={isMobile ? 'full-width' : 'large'}
      width={isMobile ? '100vw' : 1160}
    >
      <Table
        columns={columns}
        dataSource={items}
        loading={loading}
        rowKey='id'
        pagination={{
          currentPage: page,
          pageSize,
          total,
          showSizeChanger: true,
          pageSizeOpts: [10, 20, 50, 100],
          onPageChange: (currentPage) => setPage(currentPage),
          onPageSizeChange: (currentPageSize) => {
            setPageSize(currentPageSize);
            setPage(1);
          },
        }}
        size='small'
        scroll={{ x: 1100 }}
        empty={
          <Empty
            image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
            darkModeImage={
              <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
            }
            description={t('暂无中奖奖品')}
            style={{ padding: 30 }}
          />
        }
      />
    </Modal>
  );
};

export default LotteryPrizesModal;
