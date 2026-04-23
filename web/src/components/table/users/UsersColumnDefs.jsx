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

import React from 'react';
import {
  Button,
  Space,
  Tag,
  Tooltip,
  Progress,
  Popover,
  Typography,
  Dropdown,
} from '@douyinfe/semi-ui';
import { IconMore } from '@douyinfe/semi-icons';
import {
  renderGroup,
  renderNumber,
  renderQuota,
  timestamp2string,
} from '../../../helpers';

/**
 * Render user role
 */
const renderRole = (role, t) => {
  switch (role) {
    case 1:
      return (
        <Tag color='blue' shape='circle'>
          {t('普通用户')}
        </Tag>
      );
    case 10:
      return (
        <Tag color='yellow' shape='circle'>
          {t('管理员')}
        </Tag>
      );
    case 100:
      return (
        <Tag color='orange' shape='circle'>
          {t('超级管理员')}
        </Tag>
      );
    default:
      return (
        <Tag color='red' shape='circle'>
          {t('未知身份')}
        </Tag>
      );
  }
};

/**
 * Render username with remark
 */
const renderUsername = (text, record) => {
  const remark = record.remark;
  if (!remark) {
    return <span>{text}</span>;
  }
  const maxLen = 10;
  const displayRemark =
    remark.length > maxLen ? remark.slice(0, maxLen) + '...' : remark;
  return (
    <Space spacing={2}>
      <span>{text}</span>
      <Tooltip content={remark} position='top' showArrow>
        <Tag color='white' shape='circle' className='!text-xs'>
          <div className='flex items-center gap-1'>
            <div
              className='w-2 h-2 flex-shrink-0 rounded-full'
              style={{ backgroundColor: '#10b981' }}
            />
            {displayRemark}
          </div>
        </Tag>
      </Tooltip>
    </Space>
  );
};

/**
 * Render user statistics
 */
const renderStatistics = (text, record, showEnableDisableModal, t) => {
  const isDeleted = record.DeletedAt !== null;

  // Determine tag text & color like original status column
  let tagColor = 'grey';
  let tagText = t('未知状态');
  if (isDeleted) {
    tagColor = 'red';
    tagText = t('已注销');
  } else if (record.status === 1) {
    tagColor = 'green';
    tagText = t('已启用');
  } else if (record.status === 2) {
    tagColor = 'red';
    tagText = t('已禁用');
  }

  const content = (
    <Tag color={tagColor} shape='circle' size='small'>
      {tagText}
    </Tag>
  );

  const tooltipContent = (
    <div className='text-xs'>
      <div>
        {t('调用次数')}: {renderNumber(record.request_count)}
      </div>
    </div>
  );

  return (
    <Tooltip content={tooltipContent} position='top'>
      {content}
    </Tooltip>
  );
};

// Render separate quota usage column
const renderQuotaUsage = (text, record, t) => {
  const { Paragraph } = Typography;
  const used = parseInt(record.used_quota) || 0;
  const remain = parseInt(record.quota) || 0;
  const total = used + remain;
  const percent = total > 0 ? (remain / total) * 100 : 0;
  const popoverContent = (
    <div className='text-xs p-2'>
      <Paragraph copyable={{ content: renderQuota(used) }}>
        {t('已用额度')}: {renderQuota(used)}
      </Paragraph>
      <Paragraph copyable={{ content: renderQuota(remain) }}>
        {t('剩余额度')}: {renderQuota(remain)} ({percent.toFixed(0)}%)
      </Paragraph>
      <Paragraph copyable={{ content: renderQuota(total) }}>
        {t('总额度')}: {renderQuota(total)}
      </Paragraph>
    </div>
  );
  return (
    <Popover content={popoverContent} position='top'>
      <Tag color='white' shape='circle'>
        <div className='flex flex-col items-end'>
          <span className='text-xs leading-none'>{`${renderQuota(remain)} / ${renderQuota(total)}`}</span>
          <Progress
            percent={percent}
            aria-label='quota usage'
            format={() => `${percent.toFixed(0)}%`}
            style={{ width: '100%', marginTop: '1px', marginBottom: 0 }}
          />
        </div>
      </Tag>
    </Popover>
  );
};

const getDailySubscriptionUsageValue = (record) => {
  const total = Number(record?.daily_subscription_total || 0);
  const remain = Number(record?.daily_subscription_remain || 0);
  const used = Math.max(0, total - remain);
  return { total, remain, used };
};

const renderDailySubscriptionUsage = (text, record, t) => {
  const { Paragraph } = Typography;
  const { total, remain, used } = getDailySubscriptionUsageValue(record);
  const unlimited = !!record?.daily_subscription_unlimited;
  const percent = total > 0 ? (remain / total) * 100 : 0;

  const displayTotal = unlimited && total <= 0 ? t('不限') : renderQuota(total);
  const displayRemain =
    unlimited && total <= 0 ? t('不限') : renderQuota(remain);
  const displayUsed = renderQuota(used);

  const popoverContent = (
    <div className='text-xs p-2'>
      <Paragraph copyable={{ content: displayUsed }}>
        {t('已用额度')}: {displayUsed}
      </Paragraph>
      <Paragraph copyable={{ content: displayRemain }}>
        {t('剩余额度')}: {displayRemain}
        {total > 0 ? ` (${percent.toFixed(0)}%)` : ''}
      </Paragraph>
      <Paragraph copyable={{ content: displayTotal }}>
        {t('总额度')}: {displayTotal}
      </Paragraph>
    </div>
  );

  return (
    <Popover content={popoverContent} position='top'>
      <Tag color='white' shape='circle'>
        <div className='flex flex-col items-end min-w-[140px]'>
          <span className='text-xs leading-none'>{`${displayRemain} / ${displayTotal}`}</span>
          {total > 0 ? (
            <Progress
              percent={percent}
              aria-label='daily subscription usage'
              format={() => `${percent.toFixed(0)}%`}
              style={{ width: '100%', marginTop: '1px', marginBottom: 0 }}
            />
          ) : (
            <div style={{ width: '100%', marginTop: '3px', height: '4px' }} />
          )}
        </div>
      </Tag>
    </Popover>
  );
};

const formatGlobalModelRatio = (ratio) => {
  const value = Number(ratio);
  if (!Number.isFinite(value)) {
    return '1';
  }
  return value.toFixed(4).replace(/\.?0+$/, '');
};

const renderGlobalModelRatio = (text, record, t) => {
  const ratio = Number(record?.global_model_ratio);
  const value = Number.isFinite(ratio) ? ratio : 1;
  const formattedValue = formatGlobalModelRatio(value);

  let color = 'grey';
  let label = `${t('默认')} ${formattedValue}`;
  if (value === 0) {
    color = 'red';
    label = `${t('免费')} ${formattedValue}`;
  } else if (value !== 1) {
    color = value > 1 ? 'orange' : 'blue';
    label = `${t('自定义')} ${formattedValue}`;
  }

  return (
    <Tooltip
      content={`${t('该倍率仅对当前用户生效，并与系统全局模型倍率叠乘')}: ${formattedValue}`}
      position='top'
    >
      <Tag color={color} shape='circle'>
        {label}
      </Tag>
    </Tooltip>
  );
};

const renderRegisterTime = (text) => {
  const ts = Number(text || 0);
  if (ts <= 0) {
    return '-';
  }
  return <span>{timestamp2string(ts)}</span>;
};

/**
 * Render invite information
 */
const renderInviteInfo = (text, record, t) => {
  return (
    <div>
      <Space spacing={1} wrap>
        <Tag color='white' shape='circle' className='!text-xs'>
          {t('邀请')}: {renderNumber(record.aff_count)}
        </Tag>
        <Tag color='white' shape='circle' className='!text-xs'>
          {t('收益')}: {renderQuota(record.aff_history_quota)}
        </Tag>
        <Tag color='white' shape='circle' className='!text-xs'>
          {record.inviter_id === 0
            ? t('无邀请人')
            : `${t('邀请人')}: ${record.inviter_id}`}
        </Tag>
      </Space>
    </div>
  );
};

/**
 * Render operations column
 */
const renderOperations = (
  text,
  record,
  {
    showUserSubscriptionStatsModal,
    setEditingUser,
    setShowEditUser,
    showPromoteModal,
    showDemoteModal,
    showEnableDisableModal,
    showDeleteModal,
    showResetPasskeyModal,
    showResetTwoFAModal,
    showUserSubscriptionsModal,
    t,
  },
) => {
  if (record.DeletedAt !== null) {
    return <></>;
  }

  const moreMenu = [
    {
      node: 'item',
      name: t('订阅管理'),
      onClick: () => showUserSubscriptionsModal(record),
    },
    {
      node: 'divider',
    },
    {
      node: 'item',
      name: t('重置 Passkey'),
      onClick: () => showResetPasskeyModal(record),
    },
    {
      node: 'item',
      name: t('重置 2FA'),
      onClick: () => showResetTwoFAModal(record),
    },
    {
      node: 'divider',
    },
    {
      node: 'item',
      name: t('注销'),
      type: 'danger',
      onClick: () => showDeleteModal(record),
    },
  ];

  return (
    <Space>
      <Button
        type='primary'
        theme='light'
        size='small'
        onClick={() => showUserSubscriptionStatsModal(record)}
      >
        {t('统计')}
      </Button>
      {record.status === 1 ? (
        <Button
          type='danger'
          size='small'
          onClick={() => showEnableDisableModal(record, 'disable')}
        >
          {t('禁用')}
        </Button>
      ) : (
        <Button
          size='small'
          onClick={() => showEnableDisableModal(record, 'enable')}
        >
          {t('启用')}
        </Button>
      )}
      <Button
        type='tertiary'
        size='small'
        onClick={() => {
          setEditingUser(record);
          setShowEditUser(true);
        }}
      >
        {t('编辑')}
      </Button>
      <Button
        type='warning'
        size='small'
        onClick={() => showPromoteModal(record)}
      >
        {t('提升')}
      </Button>
      <Button
        type='secondary'
        size='small'
        onClick={() => showDemoteModal(record)}
      >
        {t('降级')}
      </Button>
      <Dropdown menu={moreMenu} trigger='click' position='bottomRight'>
        <Button type='tertiary' size='small' icon={<IconMore />} />
      </Dropdown>
    </Space>
  );
};

const renderStatsAction = (
  text,
  record,
  { showUserSubscriptionStatsModal, t },
) => {
  if (record.DeletedAt !== null) {
    return <></>;
  }

  return (
    <Button
      type='primary'
      theme='light'
      size='small'
      onClick={() => showUserSubscriptionStatsModal(record)}
    >
      {t('统计')}
    </Button>
  );
};

const renderRedemptionRecordsAction = (
  text,
  record,
  { showUserRedemptionRecordsModal, t },
) => {
  if (record.DeletedAt !== null) {
    return <></>;
  }

  const count = Number(record?.redemption_count || 0);
  return (
    <Button
      theme='light'
      size='small'
      onClick={() => showUserRedemptionRecordsModal(record)}
    >
      {t('{{count}} 个', { count })}
    </Button>
  );
};


/**
 * Get users table column definitions
 */
export const getUsersColumns = ({
  t,
  setEditingUser,
  setShowEditUser,
  showPromoteModal,
  showDemoteModal,
  showEnableDisableModal,
  showDeleteModal,
  showResetPasskeyModal,
  showResetTwoFAModal,
  showUserSubscriptionsModal,
  showUserRedemptionRecordsModal,
  showUserSubscriptionStatsModal,
}) => {
  return [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 50,
    },
    {
      title: t('用户名'),
      dataIndex: 'username',
      width: 100,
      render: (text, record) => renderUsername(text, record),
    },
    {
      title: t('状态'),
      dataIndex: 'info',
      width: 80,
      render: (text, record, index) =>
        renderStatistics(text, record, showEnableDisableModal, t),
    },
    {
      title: t('剩余额度/总额度'),
      key: 'quota_usage',
      width: 150,
      render: (text, record) => renderQuotaUsage(text, record, t),
    },
    {
      title: '日内剩余订阅额度/日内总订阅额度',
      dataIndex: 'daily_subscription_total',
      key: 'daily_subscription_usage',
      render: (text, record) => renderDailySubscriptionUsage(text, record, t),
      sorter: true,
    },
    {
      title: t('分组'),
      dataIndex: 'group',
      width: 60,
      render: (text, record, index) => {
        return <div>{renderGroup(text)}</div>;
      },
    },
    {
      title: t('用户倍率'),
      dataIndex: 'global_model_ratio',
      width: 120,
      render: (text, record) => renderGlobalModelRatio(text, record, t),
    },
    {
      title: t('角色'),
      dataIndex: 'role',
      width: 80,
      render: (text, record, index) => {
        return <div>{renderRole(text, t)}</div>;
      },
    },
    {
      title: t('邀请信息'),
      dataIndex: 'invite',
      width: 280,
      render: (text, record, index) => renderInviteInfo(text, record, t),
    },
    {
      title: t('兑换记录'),
      dataIndex: 'redemption_records',
      width: 80,
      render: (text, record) =>
        renderRedemptionRecordsAction(text, record, {
          showUserRedemptionRecordsModal,
          t,
        }),
    },
    {
      title: '注册时间',
      dataIndex: 'created_at',
      width: 160,
      sorter: true,
      render: (text) => renderRegisterTime(text),
    },
    {
      title: '',
      dataIndex: 'operate',
      fixed: 'right',
      width: 380,
      render: (text, record, index) =>
        renderOperations(text, record, {
          showUserSubscriptionStatsModal,
          setEditingUser,
          setShowEditUser,
          showPromoteModal,
          showDemoteModal,
          showEnableDisableModal,
          showDeleteModal,
          showResetPasskeyModal,
          showResetTwoFAModal,
          showUserSubscriptionsModal,
          t,
        }),
    },
  ];
};





