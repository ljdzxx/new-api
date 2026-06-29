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
  Typography,
  Card,
  Button,
  Banner,
  Spin,
  Tag,
  Tooltip,
  Modal,
  Input,
  InputNumber,
  Select,
} from '@douyinfe/semi-ui';
import { SiAlipay, SiWechat, SiStripe } from 'react-icons/si';
import {
  CreditCard,
  Wallet,
  TrendingUp,
  Receipt,
  Sparkles,
  Gift,
  FileText,
  RefreshCw,
  Activity,
  ChevronRight,
} from 'lucide-react';
import { IconGift } from '@douyinfe/semi-icons';
import { convertTopupBaseToPaymentCurrency } from '../../helpers/render';
import SubscriptionPlansCard from './SubscriptionPlansCard';
import InvitationCard from './InvitationCard';

const { Text } = Typography;

const ELEGANT_DISPLAY_FONT =
  '"Baskerville", "Palatino Linotype", "Book Antiqua", "Songti SC", "STSong", serif';

const formatUsdAmount = (amount) => {
  const numericAmount = Number(amount || 0);
  const digits = Number.isInteger(numericAmount) ? 0 : 2;
  return `$${numericAmount.toFixed(digits)}`;
};

const formatSubscriptionEndTime = (endTime) => {
  if (!endTime) return '-';
  const date = new Date(endTime * 1000);
  const year = date.getFullYear();
  const month = date.getMonth() + 1;
  const day = date.getDate();
  const hour = date.getHours().toString().padStart(2, '0');
  const minute = date.getMinutes().toString().padStart(2, '0');
  return `${year}/${month}/${day} ${hour}:${minute}`;
};

const RechargeCard = ({
  t,
  enableOnlineTopUp,
  enableStripeTopUp,
  enableCreemTopUp,
  creemProducts,
  creemPreTopUp,
  presetAmounts,
  selectedPreset,
  selectPresetAmount,
  priceRatio,
  topUpCount,
  minTopUp,
  getAmount,
  setTopUpCount,
  setSelectedPreset,
  payMethods,
  hasMallPayMethod = false,
  preTopUp,
  paymentLoading,
  payWay,
  redemptionCode,
  setRedemptionCode,
  topUp,
  isSubmitting,
  topUpLink,
  openTopUpLink,
  userState,
  renderQuota,
  statusLoading,
  topupInfo,
  onOpenHistory,
  subscriptionLoading = false,
  subscriptionPlans = [],
  billingPreference,
  onChangeBillingPreference,
  activeSubscriptions = [],
  allSubscriptions = [],
  reloadSubscriptionSelf,
  affLink,
  handleAffLinkClick,
  invitationRewardInfo,
}) => {
  const [redeemOpen, setRedeemOpen] = useState(false);
  const [refreshingSubscriptions, setRefreshingSubscriptions] = useState(false);
  const shouldShowSubscription =
    !subscriptionLoading && subscriptionPlans.length > 0;

  const hasPresetAmounts =
    Array.isArray(presetAmounts) && presetAmounts.length > 0;
  const hasTopupPaymentMethods =
    enableOnlineTopUp || enableStripeTopUp || hasMallPayMethod;
  const hasCreemProducts =
    enableCreemTopUp &&
    Array.isArray(creemProducts) &&
    creemProducts.length > 0;
  const shouldShowTopupPanel =
    hasPresetAmounts || hasCreemProducts || (payMethods?.length ?? 0) > 0;

  const planTitleMap = useMemo(() => {
    const map = new Map();
    (subscriptionPlans || []).forEach((p) => {
      const plan = p?.plan;
      if (plan?.id) {
        map.set(plan.id, plan.title || '');
      }
    });
    return map;
  }, [subscriptionPlans]);

  const getPresetDisplayDetail = (preset) => {
    if (!preset) {
      return null;
    }

    const discount =
      preset.discount || topupInfo?.discount?.[preset.value] || 1.0;
    const actualPay = preset.value * priceRatio * discount;
    const topupRate =
      Number.isFinite(Number(priceRatio)) && Number(priceRatio) > 0
        ? Number(priceRatio)
        : 1;

    return {
      discount,
      quotaAmountCompact: formatUsdAmount(preset.value),
      paymentAmountText: convertTopupBaseToPaymentCurrency(actualPay, topupRate),
      quotaAmountText: formatUsdAmount(preset.value),
    };
  };

  const selectedPresetConfig = presetAmounts.find(
    (preset) => preset.value === selectedPreset,
  );
  const selectedPresetDetail = getPresetDisplayDetail(selectedPresetConfig);

  const handleRefreshSubscriptions = async () => {
    setRefreshingSubscriptions(true);
    try {
      await reloadSubscriptionSelf?.();
    } finally {
      setRefreshingSubscriptions(false);
    }
  };

  const handleManualAmountChange = (value) => {
    const normalized = Number(value || 0);
    setTopUpCount(normalized);
    setSelectedPreset(null);
    if (normalized > 0) {
      getAmount(normalized);
    }
  };

  const handleRedeem = async () => {
    const success = await topUp();
    if (success) {
      setRedeemOpen(false);
    }
  };

  const renderPresetButtons = () => {
    return (
      <div className='flex flex-wrap gap-2.5'>
        {presetAmounts.map((preset, index) => {
          const presetDetail = getPresetDisplayDetail(preset);
          const active = selectedPreset === preset.value;

          return (
            <button
              key={index}
              type='button'
              className='relative rounded-lg px-5 py-1.5 text-sm font-medium transition-all'
              style={{
                background: 'transparent',
                color: active
                  ? 'var(--semi-color-primary)'
                  : 'var(--semi-color-text-0)',
                border: 'none',
                boxShadow: active
                  ? 'inset 0 0 0 1.5px var(--semi-color-primary)'
                  : 'inset 0 0 0 1px var(--semi-color-border)',
                cursor: 'pointer',
                lineHeight: '22px',
              }}
              onClick={() => selectPresetAmount(preset)}
            >
              {presetDetail?.quotaAmountCompact}
            </button>
          );
        })}
      </div>
    );
  };

  const renderPaymentIcon = (payMethod) => {
    if (payMethod.type === 'alipay') {
      return <SiAlipay size={14} color='#1677FF' />;
    }
    if (payMethod.type === 'wxpay') {
      return <SiWechat size={14} color='#07C160' />;
    }
    if (payMethod.type === 'stripe') {
      return <SiStripe size={14} color='#635BFF' />;
    }
    if (payMethod.type === 'mall') {
      return (
        <img
          src='/taobao_75px.png'
          alt='Taobao'
          style={{ width: 14, height: 14, objectFit: 'contain' }}
        />
      );
    }
    return (
      <CreditCard
        size={14}
        color={payMethod.color || 'var(--semi-color-text-2)'}
      />
    );
  };

  const renderPaymentButtons = () => {
    if (!payMethods || payMethods.length === 0) {
      return (
        <div className='rounded-lg border border-dashed border-gray-300 bg-gray-50 p-3 text-sm text-gray-500'>
          {t('暂无可用的支付方式，请联系管理员配置')}
        </div>
      );
    }

    return (
      <div className='flex flex-wrap gap-2'>
        {payMethods.map((payMethod) => {
          const minTopupVal = Number(payMethod.min_topup) || 0;
          const isStripe = payMethod.type === 'stripe';
          const isMall = payMethod.type === 'mall';
          const disabled =
            (!enableOnlineTopUp && !isStripe && !isMall) ||
            (!enableStripeTopUp && isStripe) ||
            minTopupVal > Number(topUpCount || 0);
          const isActive = payWay === payMethod.type;

          const buttonEl = (
            <button
              key={payMethod.type}
              type='button'
              onClick={() => preTopUp(payMethod.type)}
              disabled={disabled}
              className='flex items-center gap-2 rounded-md px-4 py-2 text-xs font-medium transition-all'
              style={{
                background: isActive
                  ? 'var(--semi-color-primary)'
                  : 'transparent',
                color: isActive ? '#fff' : 'var(--semi-color-text-0)',
                border: isActive
                  ? '1px solid var(--semi-color-primary)'
                  : '1px solid var(--semi-color-border)',
                cursor: disabled ? 'not-allowed' : 'pointer',
                opacity: disabled ? 0.55 : 1,
              }}
            >
              {renderPaymentIcon(payMethod)}
              {payMethod.type === 'mall' ? t('商城') : payMethod.name}
              {paymentLoading && payWay === payMethod.type ? (
                <span>{t('加载中')}</span>
              ) : (
                <ChevronRight size={12} style={{ opacity: 0.6 }} />
              )}
            </button>
          );

          return disabled && minTopupVal > Number(topUpCount || 0) ? (
            <Tooltip
              content={t('此支付方式最低充值金额为') + ' ' + minTopupVal}
              key={payMethod.type}
            >
              {buttonEl}
            </Tooltip>
          ) : (
            <React.Fragment key={payMethod.type}>{buttonEl}</React.Fragment>
          );
        })}
      </div>
    );
  };

  const renderAccountMetric = (icon, label, value, withDivider = false) => (
    <div className='flex items-center gap-0'>
      {withDivider && (
        <div
          className='mr-6 h-8 w-px flex-shrink-0 sm:mr-8'
          style={{ background: 'var(--semi-color-border)' }}
        />
      )}
      <div>
        <div className='mb-1 flex items-center gap-1.5'>
          {icon}
          <span
            style={{
              fontSize: 11,
              color: 'var(--semi-color-text-1)',
              letterSpacing: '0.02em',
            }}
          >
            {label}
          </span>
        </div>
        <div
          style={{
            fontSize: 20,
            fontWeight: 600,
            color: 'var(--semi-color-text-0)',
            letterSpacing: '-0.02em',
          }}
        >
          {value}
        </div>
      </div>
    </div>
  );

  const renderSubscriptionUsage = () => {
    if (!shouldShowSubscription) {
      return null;
    }

    if (!activeSubscriptions.length) {
      return (
        <div style={{ fontSize: 11, color: 'var(--semi-color-text-2)' }}>
          {t('已保存偏好为优先订阅，当前无生效订阅，将自动使用钱包')}
        </div>
      );
    }

    return (
      <div className='grid gap-2 [grid-template-columns:repeat(auto-fit,minmax(320px,1fr))] max-[420px]:[grid-template-columns:1fr]'>
        {activeSubscriptions.map((item, index) => {
          const subscription = item?.subscription || {};
          const total = Number(subscription.amount_total || 0);
          const used = Number(subscription.amount_used || 0);
          const remain = Math.max(0, total - used);
          const percent =
            total > 0 ? Math.max(0, Math.min(100, (used / total) * 100)) : 0;
          const planTitle =
            planTitleMap.get(subscription.plan_id) ||
            `${t('订阅')} #${subscription.id || index + 1}`;

          return (
            <div key={subscription.id || index} className='min-w-0'>
              <div className='mb-1 flex flex-wrap items-center justify-between gap-2'>
                <span className='text-xs font-medium'>{planTitle}</span>
                <span
                  className='text-[11px]'
                  style={{ color: 'var(--semi-color-text-2)' }}
                >
                  {t('到期时间')} {formatSubscriptionEndTime(subscription.end_time)}
                </span>
              </div>
              {total > 0 ? (
                <>
                  <div className='relative h-[5px] overflow-hidden rounded-full bg-gray-200 ring-1 ring-inset ring-gray-300'>
                    <div
                      className='absolute inset-0 rounded-full'
                      style={{
                        background:
                          'linear-gradient(90deg, #16a34a 0%, #22c55e 55%, #4ade80 100%)',
                      }}
                    />
                    {percent > 0 && (
                      <div
                        className='absolute inset-y-0 left-0 rounded-full transition-all'
                        style={{
                          width: `${percent}%`,
                          background:
                            'linear-gradient(90deg, #dc2626 0%, #ef4444 55%, #f87171 100%)',
                        }}
                      />
                    )}
                  </div>
                  <div
                    className='mt-1 flex justify-between text-[11px]'
                    style={{ color: 'var(--semi-color-text-2)' }}
                  >
                    <span>
                      {t('已用额度')}: {renderQuota(used)}
                    </span>
                    <span>
                      {t('剩余')}: {renderQuota(remain)}
                    </span>
                  </div>
                </>
              ) : (
                <Tag color='green' shape='circle' size='small'>
                  {t('订阅额度不限')}
                </Tag>
              )}
            </div>
          );
        })}
      </div>
    );
  };

  const renderBillingPreferenceSelect = () => {
    if (!shouldShowSubscription) {
      return null;
    }

    const hasActiveSubscription = activeSubscriptions.length > 0;
    const isSubscriptionPreference =
      billingPreference === 'subscription_first' ||
      billingPreference === 'subscription_only';
    const displayBillingPreference =
      !hasActiveSubscription && isSubscriptionPreference
        ? 'wallet_first'
        : billingPreference;

    return (
      <Select
        value={displayBillingPreference}
        onChange={onChangeBillingPreference}
        size='small'
        style={{ width: 154, fontSize: 12 }}
        optionList={[
          {
            value: 'subscription_first',
            label: hasActiveSubscription
              ? t('优先订阅扣费')
              : `${t('优先订阅扣费')} (${t('暂无激活订阅')})`,
            disabled: !hasActiveSubscription,
          },
          { value: 'wallet_first', label: t('优先钱包扣费') },
          {
            value: 'subscription_only',
            label: hasActiveSubscription
              ? t('仅订阅扣费')
              : `${t('仅订阅扣费')} (${t('暂无激活订阅')})`,
            disabled: !hasActiveSubscription,
          },
          { value: 'wallet_only', label: t('仅钱包扣费') },
        ]}
      />
    );
  };

  const renderTopupContent = () => (
    <div className='flex-1 min-w-0'>
      <div className='mb-3'>
        <span
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: 'var(--semi-color-text-0)',
            letterSpacing: '-0.01em',
          }}
        >
          {t('按量计费充值')}
        </span>
      </div>

      {statusLoading ? (
        <div className='flex justify-center py-8'>
          <Spin size='large' />
        </div>
      ) : shouldShowTopupPanel ? (
        <div className='space-y-4'>
          {hasPresetAmounts && (
            <>
              <div>
                <div className='mb-2.5 flex items-center gap-2'>
                  <span
                    style={{
                      fontSize: 12,
                      fontWeight: 500,
                      color: 'var(--semi-color-text-1)',
                    }}
                  >
                    {t('选择充值额度')}
                  </span>
                </div>
                {renderPresetButtons()}
              </div>

              <div className='flex flex-wrap items-end gap-3'>
                <div className='min-w-[180px] max-w-[260px]'>
                  <div
                    className='mb-1'
                    style={{
                      fontSize: 12,
                      fontWeight: 500,
                      color: 'var(--semi-color-text-1)',
                    }}
                  >
                    {t('充值数量')}
                  </div>
                  <InputNumber
                    value={topUpCount}
                    min={minTopUp}
                    max={999999999}
                    step={1}
                    style={{ width: '100%' }}
                    placeholder={`${t('充值数量')}，${t('最低')} ${minTopUp}`}
                    onChange={handleManualAmountChange}
                  />
                </div>

                {selectedPresetDetail && (
                  <div
                    className='flex flex-wrap items-center gap-2 text-xs'
                    style={{ color: 'var(--semi-color-text-2)' }}
                  >
                    <span>
                      {t('实付金额')}{' '}
                      <strong
                        style={{
                          color: 'var(--semi-color-text-0)',
                          fontFamily: ELEGANT_DISPLAY_FONT,
                          fontSize: 18,
                        }}
                      >
                        {selectedPresetDetail.paymentAmountText}
                      </strong>
                    </span>
                    {selectedPresetDetail.discount < 1 && (
                      <Tag color='green' size='small'>
                        {(selectedPresetDetail.discount * 10).toFixed(1)}
                        {t('折')}
                      </Tag>
                    )}
                  </div>
                )}
              </div>

              <div>
                <div className='mb-2.5 flex items-center justify-between gap-3'>
                  <span
                    style={{
                      fontSize: 12,
                      fontWeight: 500,
                      color: 'var(--semi-color-text-1)',
                    }}
                  >
                    {t('选择支付方式')}
                  </span>
                </div>
                {hasTopupPaymentMethods ? (
                  renderPaymentButtons()
                ) : (
                  <Banner
                    type='info'
                    description={t(
                      '管理员未开启在线充值功能，请联系管理员开启或使用兑换码充值。',
                    )}
                    className='!rounded-xl'
                    closeIcon={null}
                  />
                )}
              </div>
            </>
          )}

          {hasCreemProducts && (
            <div>
              <div className='mb-2.5'>
                <Text strong>{t('Creem 充值')}</Text>
              </div>
              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3'>
                {creemProducts.map((product, index) => (
                  <button
                    key={index}
                    type='button'
                    onClick={() => creemPreTopUp(product)}
                    className='rounded-lg border border-gray-200 bg-white p-4 text-left transition-all hover:border-gray-300 hover:shadow-sm'
                  >
                    <div className='mb-1 text-sm font-medium'>
                      {product.name}
                    </div>
                    <div className='mb-2 text-xs text-gray-500'>
                      {t('充值额度')}: {product.quota}
                    </div>
                    <div className='text-base font-semibold text-blue-600'>
                      {product.currency === 'EUR' ? 'EUR ' : '$'}
                      {product.price}
                    </div>
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      ) : (
        <Banner
          type='info'
          description={t(
            '管理员未开启在线充值功能，请联系管理员开启或使用兑换码充值。',
          )}
          className='!rounded-xl'
          closeIcon={null}
        />
      )}
    </div>
  );

  return (
    <>
      <Card className='!rounded-2xl border-0 shadow-sm'>
        <div className='space-y-5'>
          <div className='flex flex-col gap-3'>
            <div className='flex flex-col justify-between gap-3 xl:flex-row xl:items-center'>
              <div className='flex flex-wrap items-center gap-6 sm:gap-8'>
                {renderAccountMetric(
                  <Wallet
                    size={12}
                    style={{
                      color: 'var(--semi-color-text-1)',
                      opacity: 0.85,
                    }}
                  />,
                  t('当前余额'),
                  renderQuota(userState?.user?.quota),
                )}
                {renderAccountMetric(
                  <TrendingUp
                    size={12}
                    style={{
                      color: 'var(--semi-color-text-1)',
                      opacity: 0.85,
                    }}
                  />,
                  t('历史消耗'),
                  renderQuota(userState?.user?.used_quota),
                  true,
                )}
                {renderAccountMetric(
                  <Activity
                    size={12}
                    style={{
                      color: 'var(--semi-color-text-1)',
                      opacity: 0.85,
                    }}
                  />,
                  t('请求次数'),
                  userState?.user?.request_count || 0,
                  true,
                )}
              </div>

              <div className='flex flex-wrap items-center gap-2'>
                {renderBillingPreferenceSelect()}
                {shouldShowSubscription && (
                  <Button
                    size='small'
                    theme='borderless'
                    type='tertiary'
                    icon={
                      <RefreshCw
                        size={12}
                        className={refreshingSubscriptions ? 'animate-spin' : ''}
                      />
                    }
                    onClick={handleRefreshSubscriptions}
                    loading={refreshingSubscriptions}
                  />
                )}
                <div
                  className='h-4 w-px'
                  style={{ background: 'var(--semi-color-border)' }}
                />
                <Button
                  size='small'
                  theme='outline'
                  type='tertiary'
                  icon={<Gift size={12} />}
                  onClick={() => setRedeemOpen(true)}
                >
                  {t('兑换码')}
                </Button>
                <Button
                  size='small'
                  theme='outline'
                  type='tertiary'
                  icon={<FileText size={12} />}
                  onClick={onOpenHistory}
                >
                  {t('账单')}
                </Button>
              </div>
            </div>
            {renderSubscriptionUsage()}
          </div>

          <div style={{ height: 1, background: 'var(--semi-color-border)' }} />

          <div className='flex max-w-6xl flex-col gap-4 lg:flex-row'>
            {renderTopupContent()}
            <InvitationCard
              t={t}
              userState={userState}
              renderQuota={renderQuota}
              affLink={affLink}
              handleAffLinkClick={handleAffLinkClick}
              invitationRewardInfo={invitationRewardInfo}
            />
          </div>

          {shouldShowSubscription && (
            <>
              <div style={{ height: 1, background: 'var(--semi-color-border)' }} />
              <div>
                <div className='mb-3 flex items-center gap-2'>
                  <Sparkles size={14} color='var(--semi-color-primary)' />
                  <span
                    style={{
                      fontSize: 13,
                      fontWeight: 600,
                      color: 'var(--semi-color-text-0)',
                      letterSpacing: '-0.01em',
                    }}
                  >
                    {t('订阅套餐')}
                  </span>
                </div>
                <SubscriptionPlansCard
                  t={t}
                  loading={subscriptionLoading}
                  plans={subscriptionPlans}
                  payMethods={payMethods}
                  enableOnlineTopUp={enableOnlineTopUp}
                  enableStripeTopUp={enableStripeTopUp}
                  enableCreemTopUp={enableCreemTopUp}
                  billingPreference={billingPreference}
                  onChangeBillingPreference={onChangeBillingPreference}
                  activeSubscriptions={activeSubscriptions}
                  allSubscriptions={allSubscriptions}
                  reloadSubscriptionSelf={reloadSubscriptionSelf}
                  withCard={false}
                  showOverview={false}
                />
              </div>
            </>
          )}
        </div>
      </Card>

      <Modal
        title={t('兑换码充值')}
        visible={redeemOpen}
        onCancel={() => setRedeemOpen(false)}
        onOk={handleRedeem}
        okText={t('兑换')}
        confirmLoading={isSubmitting}
        centered
      >
        <Input
          placeholder={t('请输入兑换码')}
          value={redemptionCode}
          onChange={setRedemptionCode}
          prefix={<IconGift />}
          showClear
        />
        {topUpLink && (
          <div className='mt-2 text-xs'>
            <Text type='tertiary'>{t('在找兑换码？')}</Text>
            <Text
              type='secondary'
              underline
              className='cursor-pointer'
              onClick={openTopUpLink}
            >
              {t('购买兑换码')}
            </Text>
          </div>
        )}
      </Modal>
    </>
  );
};

export default RechargeCard;
