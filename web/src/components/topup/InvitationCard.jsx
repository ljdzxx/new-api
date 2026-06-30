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
import { Button, Input } from '@douyinfe/semi-ui';
import { Copy, Users, TrendingUp } from 'lucide-react';

const InvitationCard = ({
  t,
  userState,
  renderQuota,
  affLink,
  handleAffLinkClick,
  invitationRewardInfo,
}) => {
  const formatRewardMoney = (value) => {
    const amount = Number(value || 0);
    if (!Number.isFinite(amount) || amount <= 0) {
      return '';
    }
    return Number.isInteger(amount) ? String(amount) : amount.toFixed(2);
  };

  const inviteeMoneyText = formatRewardMoney(
    invitationRewardInfo?.inviteeMoney,
  );
  const inviterMoneyText = formatRewardMoney(
    invitationRewardInfo?.inviterMoney,
  );
  const inviteePlanTitle = invitationRewardInfo?.inviteePlanTitle || '';
  const hasInviteeReward = Boolean(inviteeMoneyText || inviteePlanTitle);
  const hasInviterReward = Boolean(inviterMoneyText);
  const highlightStyle = {
    color: 'var(--semi-color-danger)',
    fontSize: '24px',
    fontWeight: 600,
  };

  return (
    <div className='w-full flex-shrink-0 lg:w-[320px]'>
      <div className='mb-3'>
        <span
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--semi-color-text-0)',
            letterSpacing: '-0.01em',
          }}
        >
          {t('邀请奖励')}
        </span>
      </div>
      <div className='space-y-3'>
        <div className='flex flex-wrap items-center gap-4'>
          <div className='flex items-center gap-1.5'>
            <TrendingUp
              size={12}
              style={{ color: 'var(--semi-color-text-2)', opacity: 0.6 }}
            />
            <span style={{ fontSize: 11, color: 'var(--semi-color-text-2)' }}>
              {t('总收益')}
            </span>
            <span style={{ fontSize: 12, fontWeight: 600 }}>
              {renderQuota(userState?.user?.aff_history_quota || 0)}
            </span>
          </div>
          <div
            className='h-4 w-px'
            style={{ background: 'var(--semi-color-border)' }}
          />
          <div className='flex items-center gap-1.5'>
            <Users
              size={12}
              style={{ color: 'var(--semi-color-text-2)', opacity: 0.6 }}
            />
            <span style={{ fontSize: 11, color: 'var(--semi-color-text-2)' }}>
              {t('邀请人数')}
            </span>
            <span style={{ fontSize: 12, fontWeight: 600 }}>
              {userState?.user?.aff_count || 0}
            </span>
          </div>
        </div>

        <div className='flex gap-2'>
          <Input
            value={affLink}
            readonly
            size='small'
            className='flex-1'
            style={{ fontSize: 12 }}
          />
          <Button
            size='small'
            theme='outline'
            type='primary'
            icon={<Copy size={11} />}
            onClick={handleAffLinkClick}
          >
            {t('复制')}
          </Button>
        </div>

        {(hasInviteeReward || hasInviterReward) && (
          <div
            className='rounded-lg px-3 py-2 text-xs leading-5'
            style={{
              color: 'var(--semi-color-text-0)',
              background: 'var(--semi-color-success-light-default)',
              border: '1px solid var(--semi-color-success-light-active)',
            }}
          >
            {hasInviteeReward && (
              <div>
                {t('每邀请一个新人注册，新人自动获得')}
                {inviteeMoneyText && (
                  <>
                    {' '}
                    {t('额度')}{' '}
                    <span style={highlightStyle}>${inviteeMoneyText}</span>
                  </>
                )}
                {inviteePlanTitle && (
                  <>
                    {' '}
                    {t('套餐')}{' '}
                    <span style={highlightStyle}>{inviteePlanTitle}</span>
                  </>
                )}
              </div>
            )}
            {hasInviterReward && (
              <div>
                {t('同时您也将获得')}{' '}
                {t('额度')}{' '}
                <span style={highlightStyle}>${inviterMoneyText}</span>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default InvitationCard;
