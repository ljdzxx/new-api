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
  Banner,
  Button,
  Card,
  Divider,
  Modal,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconCreditCard } from '@douyinfe/semi-icons';
import { CalendarClock, CreditCard, Crown, Package } from 'lucide-react';
import { SiAlipay, SiStripe, SiWechat } from 'react-icons/si';
import { renderQuota } from '../../../helpers';
import { convertUSDToPaymentCurrency } from '../../../helpers/render';
import { formatSubscriptionDuration } from '../../../helpers/subscriptionFormat';

const { Text } = Typography;

function getDurationApproxSeconds(plan) {
  const unit = plan?.duration_unit || 'month';
  const value = Number(plan?.duration_value || 1);
  if (unit === 'custom') {
    return Number(plan?.custom_seconds || 0);
  }
  const unitSecondsMap = {
    year: 365 * 24 * 60 * 60,
    month: 30 * 24 * 60 * 60,
    week: 7 * 24 * 60 * 60,
    day: 24 * 60 * 60,
    hour: 60 * 60,
  };
  return (unitSecondsMap[unit] || 0) * value;
}

function getResetApproxSeconds(plan) {
  const period = plan?.quota_reset_period || 'never';
  if (period === 'daily') return 24 * 60 * 60;
  if (period === 'weekly') return 7 * 24 * 60 * 60;
  if (period === 'monthly') return 30 * 24 * 60 * 60;
  if (period === 'custom') {
    return Number(plan?.quota_reset_custom_seconds || 0);
  }
  return 0;
}

function getPeriodicQuotaLabels(plan, t) {
  const resetPeriod = plan?.quota_reset_period || 'never';
  const durationUnit = plan?.duration_unit || 'month';
  const totalAmount = Number(plan?.total_amount || 0);

  if (resetPeriod === 'never' || totalAmount <= 0) {
    return null;
  }

  const resetLabelMap = {
    daily: t('每日额度'),
    weekly: t('每周额度'),
    monthly: t('每月额度'),
    custom: t('周期额度'),
  };

  const durationMaxLabelMap = {
    year: t('年度拉满'),
    month: t('月度拉满'),
    week: t('周度拉满'),
    day: t('日度拉满'),
    hour: t('时段拉满'),
    custom: t('周期拉满'),
  };

  const durationSeconds = getDurationApproxSeconds(plan);
  const resetSeconds = getResetApproxSeconds(plan);
  const cycleCount =
    durationSeconds > 0 && resetSeconds > 0
      ? Math.max(1, Math.floor(durationSeconds / resetSeconds))
      : 1;

  return {
    limitLabel: resetLabelMap[resetPeriod] || t('周期额度'),
    limitValue: renderQuota(totalAmount),
    maxLabel: durationMaxLabelMap[durationUnit] || t('周期拉满'),
    maxValue: renderQuota(totalAmount * cycleCount),
  };
}

function getPaymentButtonIcon(paymentType, paymentName) {
  if (paymentType === 'alipay') {
    return <SiAlipay size={20} color='#1677FF' />;
  }
  if (paymentType === 'wxpay') {
    return <SiWechat size={20} color='#07C160' />;
  }
  if (paymentType === 'stripe') {
    return <SiStripe size={20} color='#635BFF' />;
  }
  if (paymentType === 'mall') {
    return (
      <img
        src='/taobao_75px.png'
        alt='Mall'
        style={{
          width: 20,
          height: 20,
          objectFit: 'contain',
        }}
      />
    );
  }
  if (paymentType === 'creem') {
    return <IconCreditCard style={{ color: '#2563EB' }} />;
  }
  return (
    <CreditCard
      size={20}
      color={paymentName?.color || 'var(--semi-color-text-2)'}
    />
  );
}

const SubscriptionPurchaseModal = ({
  t,
  visible,
  onCancel,
  onAfterClose,
  selectedPlan,
  providerMeta,
  paying,
  selectedEpayMethod,
  setSelectedEpayMethod,
  epayMethods = [],
  enableOnlineTopUp = false,
  enableStripeTopUp = false,
  enableCreemTopUp = false,
  purchaseLimitInfo = null,
  onPayStripe,
  onPayCreem,
  onPayEpay,
  onPayMall,
}) => {
  const plan = selectedPlan?.plan;
  const totalAmount = Number(plan?.total_amount || 0);
  const price = plan ? Number(plan.price_amount || 0) : 0;
  const displayPrice = convertUSDToPaymentCurrency(price);
  const periodicQuotaLabels = getPeriodicQuotaLabels(plan, t);

  const explicitRouting = !!providerMeta && !providerMeta?.legacy_auto;
  const providerName = providerMeta?.provider || '';
  const providerReady = providerMeta?.enabled && providerMeta?.config_ready;
  const modalEpayMethods =
    providerMeta?.available_channels?.length > 0
      ? providerMeta.available_channels.filter(
          (method) =>
            method?.type &&
            method.type !== 'stripe' &&
            method.type !== 'creem' &&
            method.type !== 'mall',
        )
      : epayMethods;

  const hasStripe = explicitRouting
    ? providerName === 'stripe' && providerReady
    : enableStripeTopUp && !!plan?.stripe_price_id;
  const hasCreem = explicitRouting
    ? providerName === 'creem' && providerReady
    : enableCreemTopUp && !!plan?.creem_product_id;
  const hasEpay = explicitRouting
    ? providerName === 'epay' && providerReady && modalEpayMethods.length > 0
    : enableOnlineTopUp && epayMethods.length > 0;
  const hasMall = explicitRouting
    ? providerName === 'mall' && providerReady
    : !!plan?.mall_link;
  const hasAnyPayment = hasStripe || hasCreem || hasEpay || hasMall;

  const purchaseLimit = Number(purchaseLimitInfo?.limit || 0);
  const purchaseCount = Number(purchaseLimitInfo?.count || 0);
  const purchaseLimitReached =
    purchaseLimit > 0 && purchaseCount >= purchaseLimit;

  const paymentOptions = [];
  if (hasEpay) {
    modalEpayMethods.forEach((method) => {
      paymentOptions.push({
        key: method.type,
        label: method.name || method.type,
        active: selectedEpayMethod === method.type,
        icon: getPaymentButtonIcon(method.type, method),
        onClick: () => {
          setSelectedEpayMethod(method.type);
          onPayEpay(method.type);
        },
      });
    });
  }
  if (hasMall) {
    paymentOptions.push({
      key: 'mall',
      label: t('商城'),
      active: false,
      icon: getPaymentButtonIcon('mall'),
      onClick: onPayMall,
    });
  }
  if (hasStripe) {
    paymentOptions.push({
      key: 'stripe',
      label: 'Stripe',
      active: false,
      icon: getPaymentButtonIcon('stripe'),
      onClick: onPayStripe,
    });
  }
  if (hasCreem) {
    paymentOptions.push({
      key: 'creem',
      label: 'Creem',
      active: false,
      icon: getPaymentButtonIcon('creem'),
      onClick: onPayCreem,
    });
  }

  return (
    <Modal
      title={
        <div className='flex items-center'>
          <Crown className='mr-2' size={18} />
          {t('购买订阅套餐')}
        </div>
      }
      visible={visible}
      onCancel={onCancel}
      afterClose={onAfterClose}
      footer={null}
      size='small'
      centered
    >
      {plan ? (
        <div className='space-y-4 pb-10'>
          <Card className='!rounded-xl !border-0 bg-slate-50 dark:bg-slate-800'>
            <div className='space-y-3'>
              <div className='flex justify-between items-center'>
                <Text strong className='text-slate-700 dark:text-slate-200'>
                  {t('套餐名称')}:
                </Text>
                <Typography.Text
                  ellipsis={{ rows: 1, showTooltip: true }}
                  className='text-slate-900 dark:text-slate-100'
                  style={{ maxWidth: 200 }}
                >
                  {plan.title}
                </Typography.Text>
              </div>

              <div className='flex justify-between items-center'>
                <Text strong className='text-slate-700 dark:text-slate-200'>
                  {t('有效期')}:
                </Text>
                <div className='flex items-center'>
                  <CalendarClock size={14} className='mr-1 text-slate-500' />
                  <Text className='text-slate-900 dark:text-slate-100'>
                    {formatSubscriptionDuration(plan, t)}
                  </Text>
                </div>
              </div>

              {periodicQuotaLabels ? (
                <>
                  <div className='flex justify-between items-center'>
                    <Text strong className='text-slate-700 dark:text-slate-200'>
                      {periodicQuotaLabels.limitLabel}:
                    </Text>
                    <div className='flex items-center'>
                      <Package size={14} className='mr-1 text-slate-500' />
                      <Text className='text-slate-900 dark:text-slate-100'>
                        {periodicQuotaLabels.limitValue}
                      </Text>
                    </div>
                  </div>

                  <div className='flex justify-between items-center'>
                    <Text strong className='text-slate-700 dark:text-slate-200'>
                      {periodicQuotaLabels.maxLabel}:
                    </Text>
                    <div className='flex items-center'>
                      <Package size={14} className='mr-1 text-slate-500' />
                      <Text className='text-slate-900 dark:text-slate-100'>
                        {periodicQuotaLabels.maxValue}
                      </Text>
                    </div>
                  </div>
                </>
              ) : (
                <div className='flex justify-between items-center'>
                  <Text strong className='text-slate-700 dark:text-slate-200'>
                    {t('总额度')}:
                  </Text>
                  <div className='flex items-center'>
                    <Package size={14} className='mr-1 text-slate-500' />
                    {totalAmount > 0 ? (
                      <Tooltip content={`${t('原始额度')}: ${totalAmount}`}>
                        <Text className='text-slate-900 dark:text-slate-100'>
                          {renderQuota(totalAmount)}
                        </Text>
                      </Tooltip>
                    ) : (
                      <Text className='text-slate-900 dark:text-slate-100'>
                        {t('不限')}
                      </Text>
                    )}
                  </div>
                </div>
              )}

              {plan?.upgrade_group ? (
                <div className='flex justify-between items-center'>
                  <Text strong className='text-slate-700 dark:text-slate-200'>
                    {t('升级分组')}:
                  </Text>
                  <Text className='text-slate-900 dark:text-slate-100'>
                    {plan.upgrade_group}
                  </Text>
                </div>
              ) : null}

              <Divider margin={8} />

              <div className='flex justify-between items-center'>
                <Text strong className='text-slate-700 dark:text-slate-200'>
                  {t('应付金额')}:
                </Text>
                <Text strong className='text-xl text-purple-600'>
                  {displayPrice}
                </Text>
              </div>
            </div>
          </Card>

          {purchaseLimitReached && (
            <Banner
              type='warning'
              description={`${t('已达购买上限')} (${purchaseCount}/${purchaseLimit})`}
              className='!rounded-xl'
              closeIcon={null}
            />
          )}

          {hasAnyPayment ? (
            <div className='space-y-3'>
              <Text size='small' type='tertiary'>
                {t('选择支付方式')}:
              </Text>

              <div className='grid grid-cols-2 gap-3 sm:flex sm:flex-wrap'>
                {paymentOptions.map((option) => (
                  <Button
                    key={option.key}
                    theme='borderless'
                    className='!h-[64px] !px-3 !text-left sm:!flex-1'
                    icon={option.icon}
                    onClick={option.onClick}
                    loading={
                      paying &&
                      (!selectedEpayMethod || selectedEpayMethod === option.key)
                    }
                    disabled={purchaseLimitReached || paying}
                    style={{
                      border: '1px solid #000',
                      borderRadius: 10,
                      backgroundColor: option.active
                        ? 'var(--semi-color-primary-light-hover)'
                        : 'var(--semi-color-bg-1)',
                      color: option.active
                        ? 'var(--semi-color-text-0)'
                        : 'var(--semi-color-text-1)',
                      justifyContent: 'flex-start',
                      boxShadow: 'none',
                    }}
                  >
                    <span className='text-sm font-semibold'>{option.label}</span>
                  </Button>
                ))}
              </div>
            </div>
          ) : (
            <Banner
              type='info'
              description={t(
                '管理员未开启在线充值功能，请联系管理员配置。',
              )}
              className='!rounded-xl'
              closeIcon={null}
            />
          )}
        </div>
      ) : null}
    </Modal>
  );
};

export default SubscriptionPurchaseModal;
