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

import React, { useMemo, useState } from 'react';
import {
  Badge,
  Button,
  Card,
  Divider,
  Select,
  Skeleton,
  Space,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import {
  API,
  executePaymentCheckout,
  showError,
  showSuccess,
  renderQuota,
} from '../../helpers';
import { getPaymentCurrencyConfig } from '../../helpers/render';
import { RefreshCw } from 'lucide-react';
import SubscriptionPurchaseModal from './modals/SubscriptionPurchaseModal';
import {
  formatSubscriptionDuration,
  formatSubscriptionResetPeriod,
} from '../../helpers/subscriptionFormat';

const { Text } = Typography;

const PAYMENT_PROVIDER_EXCLUDED_TYPES = new Set(['stripe', 'creem', 'mall']);

function getEpayMethods(rawMethods) {
  if (!Array.isArray(rawMethods)) {
    return [];
  }
  return rawMethods.filter(
    (method) =>
      method?.type &&
      !PAYMENT_PROVIDER_EXCLUDED_TYPES.has(method.type),
  );
}

function formatSubscriptionEndTime(endTime) {
  if (!endTime) return '-';
  const date = new Date(endTime * 1000);
  const year = date.getFullYear();
  const month = date.getMonth() + 1;
  const day = date.getDate();
  const hour = date.getHours().toString().padStart(2, '0');
  const minute = date.getMinutes().toString().padStart(2, '0');
  const second = date.getSeconds().toString().padStart(2, '0');
  return `${year}/${month}/${day} ${hour}:${minute}:${second}`;
}

const SubscriptionPlansCard = ({
  t,
  loading = false,
  plans = [],
  payMethods = [],
  enableOnlineTopUp = false,
  enableStripeTopUp = false,
  enableCreemTopUp = false,
  billingPreference,
  onChangeBillingPreference,
  activeSubscriptions = [],
  allSubscriptions = [],
  reloadSubscriptionSelf,
  withCard = true,
}) => {
  const [open, setOpen] = useState(false);
  const [selectedPlan, setSelectedPlan] = useState(null);
  const [paying, setPaying] = useState(false);
  const [selectedEpayMethod, setSelectedEpayMethod] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [selectedProviderMeta, setSelectedProviderMeta] = useState(null);

  const epayMethods = useMemo(() => getEpayMethods(payMethods), [payMethods]);

  const openBuy = async (p) => {
    setPaying(true);
    try {
      const res = await API.get('/api/payment/subscription/meta', {
        params: { plan_id: p?.plan?.id },
      });
      if (!res.data?.success) {
        showError(res.data?.message || t('加载支付信息失败'));
        return;
      }
      const providerMeta = res.data?.data?.provider_meta || null;
      const availableChannels =
        providerMeta?.available_channels?.length > 0
          ? getEpayMethods(providerMeta.available_channels)
          : epayMethods;
      setSelectedPlan(p);
      setSelectedProviderMeta(providerMeta);
      setSelectedEpayMethod(availableChannels?.[0]?.type || '');
      setOpen(true);
    } catch (e) {
      showError(t('加载支付信息失败'));
    } finally {
      setPaying(false);
    }
  };

  const closeBuy = () => {
    setOpen(false);
  };

  const handleBuyAfterClose = () => {
    setSelectedPlan(null);
    setSelectedProviderMeta(null);
    setPaying(false);
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await reloadSubscriptionSelf?.();
    } finally {
      setRefreshing(false);
    }
  };

  const paySubscriptionWithUnifiedCheckout = async (paymentMethod = '') => {
    const planId = selectedPlan?.plan?.id;
    if (!planId) {
      throw new Error(t('请选择订阅套餐'));
    }
    const res = await API.post('/api/payment/subscription/checkout', {
      plan_id: planId,
      payment_method: paymentMethod,
    });
    if (!res.data?.success) {
      throw new Error(res.data?.message || t('发起支付失败'));
    }
    executePaymentCheckout(res.data.data);
    showSuccess(t('已发起支付'));
    closeBuy();
  };

  const payStripe = async () => {
    if (!selectedPlan?.plan?.stripe_price_id) {
      showError(t('当前套餐未配置 Stripe 价格'));
      return;
    }
    setPaying(true);
    try {
      await paySubscriptionWithUnifiedCheckout('stripe');
    } catch (e) {
      showError(e.message || t('发起支付失败'));
    } finally {
      setPaying(false);
    }
  };
  const payCreem = async () => {
    if (!selectedPlan?.plan?.creem_product_id) {
      showError(t('当前套餐未配置 Creem 商品'));
      return;
    }
    setPaying(true);
    try {
      await paySubscriptionWithUnifiedCheckout('creem');
    } catch (e) {
      showError(e.message || t('发起支付失败'));
    } finally {
      setPaying(false);
    }
  };
  const payEpay = async (paymentMethod = selectedEpayMethod) => {
    if (!paymentMethod) {
      showError(t('请选择支付方式'));
      return;
    }
    setPaying(true);
    try {
      setSelectedEpayMethod(paymentMethod);
      await paySubscriptionWithUnifiedCheckout(paymentMethod);
    } catch (e) {
      showError(e.message || t('发起支付失败'));
    } finally {
      setPaying(false);
    }
  };
  const payMall = async () => {
    setPaying(true);
    try {
      await paySubscriptionWithUnifiedCheckout('mall');
    } catch (e) {
      showError(e.message || t('发起支付失败'));
    } finally {
      setPaying(false);
    }
  };

  // Billing preference falls back to wallet modes when no active subscription exists.
  const hasActiveSubscription = activeSubscriptions.length > 0;
  const hasAnySubscription = allSubscriptions.length > 0;
  const disableSubscriptionPreference = !hasActiveSubscription;
  const isSubscriptionPreference =
    billingPreference === 'subscription_first' ||
    billingPreference === 'subscription_only';
  const displayBillingPreference =
    disableSubscriptionPreference && isSubscriptionPreference
      ? 'wallet_first'
      : billingPreference;
  const subscriptionPreferenceLabel =
    billingPreference === 'subscription_only'
      ? t('仅订阅扣费')
      : t('优先订阅扣费');

  const planPurchaseCountMap = useMemo(() => {
    const map = new Map();
    (allSubscriptions || []).forEach((sub) => {
      const planId = sub?.subscription?.plan_id;
      if (!planId) return;
      map.set(planId, (map.get(planId) || 0) + 1);
    });
    return map;
  }, [allSubscriptions]);

  const planTitleMap = useMemo(() => {
    const map = new Map();
    (plans || []).forEach((p) => {
      const plan = p?.plan;
      if (!plan?.id) return;
      map.set(plan.id, plan.title || '');
    });
    return map;
  }, [plans]);

  const getPlanPurchaseCount = (planId) =>
    planPurchaseCountMap.get(planId) || 0;

  const getDurationApproxSeconds = (plan) => {
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
  };

  const getResetApproxSeconds = (plan) => {
    const period = plan?.quota_reset_period || 'never';
    if (period === 'daily') return 24 * 60 * 60;
    if (period === 'weekly') return 7 * 24 * 60 * 60;
    if (period === 'monthly') return 30 * 24 * 60 * 60;
    if (period === 'custom') {
      return Number(plan?.quota_reset_custom_seconds || 0);
    }
    return 0;
  };

  const getPeriodicQuotaLabels = (plan) => {
    const resetPeriod = plan?.quota_reset_period || 'never';
    const durationUnit = plan?.duration_unit || 'month';
    const totalAmount = Number(plan?.total_amount || 0);

    if (resetPeriod === 'never' || totalAmount <= 0) {
      return null;
    }

    const resetLabelMap = {
      daily: t('日限额'),
      weekly: t('周限额'),
      monthly: t('月限额'),
      custom: t('周期限额'),
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
    const maxQuota = totalAmount * cycleCount;

    return {
      limitLabel: resetLabelMap[resetPeriod] || t('周期限额'),
      limitValue: renderQuota(totalAmount),
      maxLabel: durationMaxLabelMap[durationUnit] || t('周期拉满'),
      maxValue: renderQuota(maxQuota),
      maxTooltip: `${t('按当前重置周期估算')} × ${cycleCount}`,
    };
  };

  const renderSubscriptionQuotaBar = (usedAmount, totalAmount) => {
    if (totalAmount <= 0) {
      return (
        <div className='text-xs text-gray-500'>
          {t('额度')}: {t('不限')}
        </div>
      );
    }

    const remainAmount = Math.max(0, totalAmount - usedAmount);
    const usagePercent = Math.max(
      0,
      Math.min(100, Math.round((usedAmount / totalAmount) * 100)),
    );
    const remainPercent = Math.max(0, 100 - usagePercent);
    const dividerLeft = `${usagePercent}%`;

    return (
      <div className='mt-1'>
        <div className='mb-1.5 flex items-center justify-between gap-3'>
          <span className='text-xs font-medium text-gray-600'>{t('额度')}</span>
          <span className='text-[11px] font-medium text-gray-500'>
            {usagePercent}% / {remainPercent}%
          </span>
        </div>
        <Tooltip
          content={`${t('已用额度')}: ${renderQuota(usedAmount)}/${renderQuota(totalAmount)} · ${t('剩余')} ${renderQuota(remainAmount)} · ${t('使用率')} ${usagePercent}%`}
        >
          <div className='relative h-2.5 cursor-help overflow-hidden rounded-full bg-gray-200 ring-1 ring-inset ring-gray-300'>
            <div
              className='absolute inset-0 rounded-full'
              style={{
                background:
                  'linear-gradient(90deg, #16a34a 0%, #22c55e 55%, #4ade80 100%)',
                boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.35)',
              }}
            />
            <div
              className='absolute inset-y-0 left-0 rounded-full transition-all duration-300'
              style={{
                width: `${usagePercent}%`,
                background:
                  'linear-gradient(90deg, #dc2626 0%, #ef4444 55%, #f87171 100%)',
                boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.35)',
              }}
            />
            {usagePercent > 0 && remainPercent > 0 && (
              <div
                className='absolute inset-y-0 w-px bg-white/80'
                style={{ left: dividerLeft }}
              />
            )}
          </div>
        </Tooltip>
      </div>
    );
  };

  const cardContent = (
    <>
      {/* Subscription overview skeleton */}
      {loading ? (
        <div className='space-y-4'>
          {/* Subscription overview */}
          <Card className='!rounded-xl w-full' bodyStyle={{ padding: '12px' }}>
            <div className='flex items-center justify-between mb-3'>
              <Skeleton.Title active style={{ width: 100, height: 20 }} />
              <Skeleton.Button active style={{ width: 24, height: 24 }} />
            </div>
            <div className='space-y-2'>
              <Skeleton.Paragraph active rows={2} />
            </div>
          </Card>
          {/* Subscription plans skeleton */}
          <div className='grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-2 xl:grid-cols-3 gap-5 w-full px-1'>
            {[1, 2, 3].map((i) => (
              <Card
                key={i}
                className='!rounded-xl w-full h-full'
                bodyStyle={{ padding: 16 }}
              >
                <Skeleton.Title
                  active
                  style={{ width: '60%', height: 24, marginBottom: 8 }}
                />
                <Skeleton.Paragraph
                  active
                  rows={1}
                  style={{ marginBottom: 12 }}
                />
                <div className='text-center py-4'>
                  <Skeleton.Title
                    active
                    style={{ width: '40%', height: 32, margin: '0 auto' }}
                  />
                </div>
                <Skeleton.Paragraph active rows={3} style={{ marginTop: 12 }} />
                <Skeleton.Button
                  active
                  block
                  style={{ marginTop: 16, height: 32 }}
                />
              </Card>
            ))}
          </div>
        </div>
      ) : (
        <Space vertical style={{ width: '100%' }} spacing={8}>
          {/* Subscription list */}
          <Card className='!rounded-xl w-full' bodyStyle={{ padding: '12px' }}>
            <div className='flex items-center justify-between mb-2 gap-3'>
              <div className='flex items-center gap-2 flex-1 min-w-0'>
                <Text strong>{t('我的订阅')}</Text>
                {hasActiveSubscription ? (
                  <Tag
                    color='white'
                    size='small'
                    shape='circle'
                    prefixIcon={<Badge dot type='success' />}
                  >
                    {activeSubscriptions.length} {t('个生效中')}
                  </Tag>
                ) : (
                  <Tag color='white' size='small' shape='circle'>
                    {t('暂无激活订阅')}
                  </Tag>
                )}
                {allSubscriptions.length > activeSubscriptions.length && (
                  <Tag color='white' size='small' shape='circle'>
                    {allSubscriptions.length - activeSubscriptions.length}{' '}
                    {t('个已结束')}
                  </Tag>
                )}
              </div>
              <div className='flex items-center gap-2'>
                <Select
                  value={displayBillingPreference}
                  onChange={onChangeBillingPreference}
                  size='small'
                  optionList={[
                    {
                      value: 'subscription_first',
                      label: disableSubscriptionPreference
                        ? `${t('优先订阅扣费')} (${t('暂无激活订阅')})`
                        : t('优先订阅扣费'),
                      disabled: disableSubscriptionPreference,
                    },
                    { value: 'wallet_first', label: t('优先钱包扣费') },
                    {
                      value: 'subscription_only',
                      label: disableSubscriptionPreference
                        ? `${t('仅订阅扣费')} (${t('暂无激活订阅')})`
                        : t('仅订阅扣费'),
                      disabled: disableSubscriptionPreference,
                    },
                    { value: 'wallet_only', label: t('仅钱包扣费') },
                  ]}
                />
                <Button
                  size='small'
                  theme='light'
                  type='tertiary'
                  icon={
                    <RefreshCw
                      size={12}
                      className={refreshing ? 'animate-spin' : ''}
                    />
                  }
                  onClick={handleRefresh}
                  loading={refreshing}
                />
              </div>
            </div>
            {disableSubscriptionPreference && isSubscriptionPreference && (
              <Text type='tertiary' size='small'>
                {t('当前没有可用订阅，已自动切换为')}
                {subscriptionPreferenceLabel}
                {t('，你也可以手动切换到钱包扣费。')}
              </Text>
            )}

            {hasAnySubscription ? (
              <>
                <Divider margin={8} />
                <div className='max-h-64 overflow-y-auto pr-1 semi-table-body'>
                  {allSubscriptions.map((sub, subIndex) => {
                    const isLast = subIndex === allSubscriptions.length - 1;
                    const subscription = sub.subscription;
                    const totalAmount = Number(subscription?.amount_total || 0);
                    const usedAmount = Number(subscription?.amount_used || 0);
                    const planTitle =
                      planTitleMap.get(subscription?.plan_id) || '';
                    const now = Date.now() / 1000;
                    const isExpired = (subscription?.end_time || 0) < now;
                    const isCancelled = subscription?.status === 'cancelled';
                    const isActive =
                      subscription?.status === 'active' && !isExpired;
                    return (
                      <div key={subscription?.id || subIndex}>
                        <div className='flex items-center gap-2 text-xs mb-1 flex-wrap'>
                          <span className='font-medium'>
                            {planTitle
                              ? `${planTitle} · ${t('订阅')} #${subscription?.id}`
                              : `${t('订阅')} #${subscription?.id}`}
                          </span>
                          {isActive ? (
                            <Tag
                              color='white'
                              size='small'
                              shape='circle'
                              prefixIcon={<Badge dot type='success' />}
                            >
                              {t('生效中')}
                            </Tag>
                          ) : isCancelled ? (
                            <Tag color='white' size='small' shape='circle'>
                              {t('已取消')}
                            </Tag>
                          ) : (
                            <Tag color='white' size='small' shape='circle'>
                              {t('已过期')}
                            </Tag>
                          )}
                        </div>
                        <div className='text-xs text-gray-500 mb-1'>
                          {t('到期时间')} {formatSubscriptionEndTime(subscription?.end_time || 0)}
                        </div>
                        {renderSubscriptionQuotaBar(usedAmount, totalAmount)}
                        {!isLast && <Divider margin={12} />}
                      </div>
                    );
                  })}
                </div>
              </>
            ) : (
              <div className='text-xs text-gray-500'>
                {t('暂无订阅记录，购买套餐后会显示在这里')}
              </div>
            )}
          </Card>

          {/* Subscription plans */}
          {plans.length > 0 ? (
            <div className='grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-2 xl:grid-cols-3 gap-5 w-full px-1'>
              {plans.map((p) => {
                const plan = p?.plan;
                const totalAmount = Number(plan?.total_amount || 0);
                const { symbol, rate } = getPaymentCurrencyConfig();
                const price = Number(plan?.price_amount || 0);
                const convertedPrice = price * rate;
                const displayPrice = convertedPrice.toFixed(
                  Number.isInteger(convertedPrice) ? 0 : 2,
                );
                const limit = Number(plan?.max_purchase_per_user || 0);
                const limitLabel = limit > 0 ? `${t('限购')} ${limit}` : null;
                const periodicQuotaLabels = getPeriodicQuotaLabels(plan);
                const totalLabel =
                  totalAmount > 0
                    ? `${t('总额度')}: ${renderQuota(totalAmount)}`
                    : `${t('总额度')}: ${t('不限')}`;
                const upgradeLabel = plan?.upgrade_group
                  ? `${t('升级分组')}: ${plan.upgrade_group}`
                  : null;
                const planBenefits = [
                  {
                    label: `${t('有效期')}: ${formatSubscriptionDuration(plan, t)}`,
                  },
                  periodicQuotaLabels
                    ? {
                        label: `${periodicQuotaLabels.limitLabel}: ${periodicQuotaLabels.limitValue}`,
                        tooltip: `${t('重置周期')}: ${formatSubscriptionResetPeriod(plan, t)}`,
                      }
                    : totalAmount > 0
                      ? {
                          label: totalLabel,
                          tooltip: `${t('原始额度')}: ${renderQuota(totalAmount)}`,
                        }
                      : { label: totalLabel },
                  periodicQuotaLabels
                    ? {
                        label: `${periodicQuotaLabels.maxLabel}: ${periodicQuotaLabels.maxValue}`,
                        tooltip: periodicQuotaLabels.maxTooltip,
                      }
                    : null,
                  limitLabel ? { label: limitLabel } : null,
                  upgradeLabel ? { label: upgradeLabel } : null,
                ].filter(Boolean);

                const planVariantStyle =
                  plan?.duration_unit === 'month'
                    ? {
                        border: '2px solid #d4af37',
                        boxShadow:
                          '0 0 0 1px rgba(212,175,55,0.34), 0 14px 28px rgba(217,119,6,0.18)',
                        background:
                          'linear-gradient(135deg, var(--semi-color-bg-0) 0%, var(--semi-color-bg-1) 48%, rgba(212,175,55,0.18) 100%)',
                      }
                    : plan?.duration_unit === 'week'
                      ? {
                          border: '2px solid #bfc6d1',
                          boxShadow:
                            '0 0 0 1px rgba(148,163,184,0.34), 0 14px 28px rgba(100,116,139,0.18)',
                          background:
                            'linear-gradient(135deg, var(--semi-color-bg-0) 0%, var(--semi-color-bg-1) 50%, rgba(148,163,184,0.20) 100%)',
                        }
                      : {
                          border: '1px solid var(--semi-color-border)',
                          background: 'var(--semi-color-bg-2)',
                        };

                const planAccentStyle =
                  plan?.duration_unit === 'month'
                    ? {
                        background:
                          'linear-gradient(90deg, #b8860b 0%, #facc15 45%, #fde68a 100%)',
                      }
                    : plan?.duration_unit === 'week'
                      ? {
                          background:
                            'linear-gradient(90deg, #94a3b8 0%, #e2e8f0 48%, #f8fafc 100%)',
                        }
                      : {
                          background:
                            'linear-gradient(90deg, rgba(148,163,184,0.35) 0%, rgba(226,232,240,0.35) 100%)',
                        };

                return (
                  <Card
                    key={plan?.id}
                    className='!rounded-xl transition-all hover:shadow-lg w-full h-full'
                    style={planVariantStyle}
                    bodyStyle={{ padding: 0 }}
                  >
                    <div className='h-1.5 w-full rounded-t-xl' style={planAccentStyle} />
                    <div className='p-4 h-full flex flex-col'>
                      {/* Plan title */}
                      <div className='mb-3'>
                        <Typography.Title
                          heading={5}
                          ellipsis={{ rows: 1, showTooltip: true }}
                          style={{ margin: 0 }}
                        >
                          {plan?.title || t('未命名套餐')}
                        </Typography.Title>
                        {plan?.subtitle && (
                          <Text
                            type='tertiary'
                            size='small'
                            ellipsis={{ rows: 1, showTooltip: true }}
                            style={{ display: 'block' }}
                          >
                            {plan.subtitle}
                          </Text>
                        )}
                      </div>

                      {/* Price section */}
                      <div className='py-2'>
                        <div className='flex items-baseline justify-start'>
                          <span className='text-xl font-bold text-purple-600'>
                            {symbol}
                          </span>
                          <span className='text-3xl font-bold text-purple-600'>
                            {displayPrice}
                          </span>
                        </div>
                      </div>

                      {/* Plan benefits */}
                      <div className='flex flex-col items-start gap-1 pb-2'>
                        {planBenefits.map((item) => {
                          const content = (
                            <div className='flex items-center gap-2 text-xs text-gray-500'>
                              <Badge dot type='tertiary' />
                              <span>{item.label}</span>
                            </div>
                          );
                          if (!item.tooltip) {
                            return (
                              <div
                                key={item.label}
                                className='w-full flex justify-start'
                              >
                                {content}
                              </div>
                            );
                          }
                          return (
                            <Tooltip key={item.label} content={item.tooltip}>
                              <div className='w-full flex justify-start'>
                                {content}
                              </div>
                            </Tooltip>
                          );
                        })}
                      </div>

                      <div className='mt-auto'>
                        <Divider margin={12} />

                        {/* Purchase action */}
                        {(() => {
                          const count = getPlanPurchaseCount(p?.plan?.id);
                          const reached = limit > 0 && count >= limit;
                          const tip = reached
                            ? t('已达购买上限') + ` (${count}/${limit})`
                            : '';
                          const buttonEl = (
                            <Button
                              theme='outline'
                              type='primary'
                              block
                              disabled={reached}
                              onClick={() => {
                                if (reached) return;
                                openBuy(p);
                              }}
                            >
                              {reached ? t('已达上限') : t('立即订阅')}
                            </Button>
                          );
                          return reached ? (
                            <Tooltip content={tip} position='top'>
                              {buttonEl}
                            </Tooltip>
                          ) : (
                            buttonEl
                          );
                        })()}
                      </div>
                    </div>
                  </Card>
                );
              })}
            </div>
          ) : (
            <div className='text-center text-gray-400 text-sm py-4'>
              {t('暂无可购买套餐')}
            </div>
          )}
        </Space>
      )}
    </>
  );

  return (
    <>
      {withCard ? (
        <Card className='!rounded-2xl shadow-sm border-0'>{cardContent}</Card>
      ) : (
        <div className='space-y-3'>{cardContent}</div>
      )}

      {/* Purchase modal */}
      <SubscriptionPurchaseModal
        t={t}
        visible={open}
        onCancel={closeBuy}
        onAfterClose={handleBuyAfterClose}
        selectedPlan={selectedPlan}
        providerMeta={selectedProviderMeta}
        paying={paying}
        selectedEpayMethod={selectedEpayMethod}
        setSelectedEpayMethod={setSelectedEpayMethod}
        epayMethods={epayMethods}
        enableOnlineTopUp={enableOnlineTopUp}
        enableStripeTopUp={enableStripeTopUp}
        enableCreemTopUp={enableCreemTopUp}
        purchaseLimitInfo={
          selectedPlan?.plan?.id
            ? {
                limit: Number(selectedPlan?.plan?.max_purchase_per_user || 0),
                count: getPlanPurchaseCount(selectedPlan?.plan?.id),
              }
            : null
        }
        onPayStripe={payStripe}
        onPayCreem={payCreem}
        onPayEpay={payEpay}
        onPayMall={payMall}
      />
    </>
  );
};

export default SubscriptionPlansCard;




