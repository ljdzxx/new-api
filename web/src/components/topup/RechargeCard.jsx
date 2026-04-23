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
import {
  Avatar,
  Typography,
  Card,
  Button,
  Banner,
  Form,
  Space,
  Spin,
  Tag,
  Tooltip,
  Tabs,
  TabPane,
} from '@douyinfe/semi-ui';
import { SiAlipay, SiWechat, SiStripe } from 'react-icons/si';
import {
  CreditCard,
  Wallet,
  BarChart2,
  TrendingUp,
  Receipt,
  Sparkles,
} from 'lucide-react';
import { IconGift } from '@douyinfe/semi-icons';
import {
  convertTopupBaseToPaymentCurrency,
  getCurrencyConfig,
  getPaymentCurrencyConfig,
} from '../../helpers/render';
import SubscriptionPlansCard from './SubscriptionPlansCard';

const { Text } = Typography;

const ELEGANT_DISPLAY_FONT =
  '"Baskerville", "Palatino Linotype", "Book Antiqua", "Songti SC", "STSong", serif';

const resolveCurrencyLabel = (currencyConfig) => {
  if (!currencyConfig) {
    return '';
  }
  if (currencyConfig.type === 'CUSTOM') {
    return currencyConfig.symbol || 'CUSTOM';
  }
  return currencyConfig.type || currencyConfig.symbol || '';
};

const renderAmountHighlight = (value, accentColor = '#111827') => {
  const text = String(value || '');
  const matched = text.match(/^([^0-9-]*)([0-9][0-9,]*(?:\.[0-9]+)?)$/);

  if (!matched) {
    return (
      <span
        style={{
          fontFamily: ELEGANT_DISPLAY_FONT,
          fontSize: '1.35rem',
          fontWeight: 700,
          color: accentColor,
        }}
      >
        {text}
      </span>
    );
  }

  const [, prefix, amount] = matched;
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'baseline',
        gap: '4px',
        color: accentColor,
      }}
    >
      {prefix ? (
        <span
          style={{
            fontFamily: ELEGANT_DISPLAY_FONT,
            fontSize: '1rem',
            fontWeight: 600,
            opacity: 0.9,
          }}
        >
          {prefix}
        </span>
      ) : null}
      <span
        style={{
          fontFamily: ELEGANT_DISPLAY_FONT,
          fontSize: '1.65rem',
          lineHeight: 1,
          fontWeight: 700,
          letterSpacing: '0.01em',
        }}
      >
        {amount}
      </span>
    </span>
  );
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
  formatLargeNumber,
  priceRatio,
  topUpCount,
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
}) => {
  const redeemFormApiRef = useRef(null);
  const initialTabSetRef = useRef(false);
  const [activeTab, setActiveTab] = useState('topup');
  const shouldShowSubscription =
    !subscriptionLoading && subscriptionPlans.length > 0;

  useEffect(() => {
    if (initialTabSetRef.current) return;
    if (subscriptionLoading) return;
    setActiveTab(shouldShowSubscription ? 'subscription' : 'topup');
    initialTabSetRef.current = true;
  }, [shouldShowSubscription, subscriptionLoading]);

  useEffect(() => {
    if (!shouldShowSubscription && activeTab !== 'topup') {
      setActiveTab('topup');
    }
  }, [shouldShowSubscription, activeTab]);

  const hasPresetAmounts = Array.isArray(presetAmounts) && presetAmounts.length > 0;
  const hasTopupPaymentMethods =
    enableOnlineTopUp || enableStripeTopUp || hasMallPayMethod;
  const hasCreemProducts =
    enableCreemTopUp &&
    Array.isArray(creemProducts) &&
    creemProducts.length > 0;
  const shouldShowTopupPanel =
    hasPresetAmounts || hasCreemProducts || (payMethods?.length ?? 0) > 0;

  const getPresetDisplayDetail = (preset) => {
    if (!preset) {
      return null;
    }

    const discount =
      preset.discount || topupInfo?.discount?.[preset.value] || 1.0;
    const actualPay = preset.value * priceRatio * discount;
    const quotaCurrency = getCurrencyConfig();
    const paymentCurrency = getPaymentCurrencyConfig();
    const topupRate =
      Number.isFinite(Number(priceRatio)) && Number(priceRatio) > 0
        ? Number(priceRatio)
        : 1;
    const currentMultiplier =
      topupRate > 0 ? (1 / topupRate).toFixed(2) : '0.00';

    let displayValue = preset.value;
    if (quotaCurrency.type === 'CNY') {
      displayValue = preset.value * topupRate;
    } else if (quotaCurrency.type === 'CUSTOM') {
      displayValue = preset.value * quotaCurrency.rate;
    }

    return {
      discount,
      paymentAmountCompact: convertTopupBaseToPaymentCurrency(
        actualPay,
        topupRate,
        0,
      ),
      paymentAmountText: convertTopupBaseToPaymentCurrency(actualPay, topupRate),
      quotaAmountText: `${quotaCurrency.symbol}${formatLargeNumber(displayValue)}`,
      currentMultiplier,
      paymentCurrencyLabel: resolveCurrencyLabel(paymentCurrency),
      quotaCurrencyLabel: resolveCurrencyLabel(quotaCurrency),
    };
  };

  const selectedPresetConfig = presetAmounts.find(
    (preset) => preset.value === selectedPreset,
  );
  const selectedPresetDetail = getPresetDisplayDetail(selectedPresetConfig);

  const renderPresetCards = () => {
    return (
      <div className='grid grid-cols-2 gap-3 md:grid-cols-4'>
        {presetAmounts.map((preset, index) => {
          const presetDetail = getPresetDisplayDetail(preset);

          return (
            <Card
              key={index}
              style={{
                cursor: 'pointer',
                border:
                  selectedPreset === preset.value
                    ? '2px solid var(--semi-color-primary)'
                    : '1px solid var(--semi-color-border)',
                height: '100%',
                width: '100%',
                boxShadow:
                  selectedPreset === preset.value
                    ? '0 8px 24px rgba(var(--semi-blue-5), 0.18)'
                    : 'none',
              }}
              className='transition-all hover:-translate-y-0.5 hover:shadow-md'
              bodyStyle={{ padding: '18px 14px' }}
              onClick={() => selectPresetAmount(preset)}
            >
              <div className='flex min-h-[72px] items-center justify-center text-center'>
                <div
                  className='font-bold leading-none text-gray-900'
                  style={{
                    fontSize: 'clamp(28px, 4vw, 38px)',
                    fontFamily: ELEGANT_DISPLAY_FONT,
                    letterSpacing: '0.01em',
                  }}
                >
                  {presetDetail?.paymentAmountCompact}
                </div>
              </div>
            </Card>
          );
        })}
      </div>
    );
  };

  const renderPaymentButtons = () => {
    if (!payMethods || payMethods.length === 0) {
      return (
        <div className='text-gray-500 text-sm p-3 bg-gray-50 rounded-lg border border-dashed border-gray-300'>
          {t('暂无可用的支付方式，请联系管理员配置')}
        </div>
      );
    }

    return (
      <div className='grid grid-cols-2 gap-3 sm:flex'>
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
              className={`relative flex h-[64px] flex-col items-center justify-center rounded-lg border px-3 transition-all sm:flex-1 ${
                isActive
                  ? 'border-blue-400 bg-blue-50 text-gray-900 shadow-sm'
                  : 'border-gray-200 bg-white text-gray-700 hover:border-gray-300'
              } ${disabled ? 'cursor-not-allowed opacity-60' : ''}`}
            >
              <span className='flex items-center gap-2'>
                {payMethod.type === 'alipay' ? (
                  <SiAlipay size={22} color='#1677FF' />
                ) : payMethod.type === 'wxpay' ? (
                  <SiWechat size={22} color='#07C160' />
                ) : payMethod.type === 'stripe' ? (
                  <SiStripe size={22} color='#635BFF' />
                ) : payMethod.type === 'mall' ? (
                  <img
                    src='/taobao_75px.png'
                    alt='Taobao'
                    style={{
                      width: 22,
                      height: 22,
                      objectFit: 'contain',
                    }}
                  />
                ) : (
                  <CreditCard
                    size={22}
                    color={payMethod.color || 'var(--semi-color-text-2)'}
                  />
                )}
                                <span className='text-base font-semibold'>
                                  {payMethod.type === 'mall'
                                    ? t('商城')
                                    : payMethod.name}
                                </span>
              </span>
              {paymentLoading && payWay === payMethod.type && (
                <span className='mt-1 text-xs text-blue-600'>
                  {t('加载中')}
                </span>
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

  const renderTopupContent = () => (
    <Space vertical style={{ width: '100%' }}>
      <Card
        className='!rounded-xl w-full'
        cover={
          <div
            className='relative h-30'
            style={{
              '--palette-primary-darkerChannel': '37 99 235',
              backgroundImage: `linear-gradient(0deg, rgba(var(--palette-primary-darkerChannel) / 80%), rgba(var(--palette-primary-darkerChannel) / 80%)), url('/cover-4.webp')`,
              backgroundSize: 'cover',
              backgroundPosition: 'center',
              backgroundRepeat: 'no-repeat',
            }}
          >
            <div className='relative z-10 h-full flex flex-col justify-between p-4'>
              <div className='flex justify-between items-center'>
                <Text strong style={{ color: 'white', fontSize: '16px' }}>
                  {t('账户统计')}
                </Text>
              </div>

              <div className='grid grid-cols-3 gap-6 mt-4'>
                <div className='text-center'>
                  <div
                    className='text-base sm:text-2xl font-bold mb-2'
                    style={{ color: 'white' }}
                  >
                    {renderQuota(userState?.user?.quota)}
                  </div>
                  <div className='flex items-center justify-center text-sm'>
                    <Wallet
                      size={14}
                      className='mr-1'
                      style={{ color: 'rgba(255,255,255,0.8)' }}
                    />
                    <Text
                      style={{
                        color: 'rgba(255,255,255,0.8)',
                        fontSize: '12px',
                      }}
                    >
                      {t('当前余额')}
                    </Text>
                  </div>
                </div>

                <div className='text-center'>
                  <div
                    className='text-base sm:text-2xl font-bold mb-2'
                    style={{ color: 'white' }}
                  >
                    {renderQuota(userState?.user?.used_quota)}
                  </div>
                  <div className='flex items-center justify-center text-sm'>
                    <TrendingUp
                      size={14}
                      className='mr-1'
                      style={{ color: 'rgba(255,255,255,0.8)' }}
                    />
                    <Text
                      style={{
                        color: 'rgba(255,255,255,0.8)',
                        fontSize: '12px',
                      }}
                    >
                      {t('历史消耗')}
                    </Text>
                  </div>
                </div>

                <div className='text-center'>
                  <div
                    className='text-base sm:text-2xl font-bold mb-2'
                    style={{ color: 'white' }}
                  >
                    {userState?.user?.request_count || 0}
                  </div>
                  <div className='flex items-center justify-center text-sm'>
                    <BarChart2
                      size={14}
                      className='mr-1'
                      style={{ color: 'rgba(255,255,255,0.8)' }}
                    />
                    <Text
                      style={{
                        color: 'rgba(255,255,255,0.8)',
                        fontSize: '12px',
                      }}
                    >
                      {t('请求次数')}
                    </Text>
                  </div>
                </div>
              </div>
            </div>
          </div>
        }
      >
        {statusLoading ? (
          <div className='py-8 flex justify-center'>
            <Spin size='large' />
          </div>
        ) : shouldShowTopupPanel ? (
          <div className='space-y-6'>
            {hasPresetAmounts && (
              <>
                <div>
                  <div className='mb-3 flex items-center gap-2'>
                    <Text strong>{t('选择充值金额')}</Text>
                    {(() => {
                      const { symbol, rate, type } = getCurrencyConfig();
                      if (type === 'USD') return null;

                      return (
                        <span
                          style={{
                            color: 'var(--semi-color-text-2)',
                            fontSize: '12px',
                            fontWeight: 'normal',
                          }}
                        >
                          (1 $ = {rate.toFixed(2)} {symbol})
                        </span>
                      );
                    })()}
                  </div>
                  {renderPresetCards()}
                </div>

                {selectedPresetDetail && (
                  <div
                    className='rounded-2xl border px-4 py-4'
                    style={{
                      borderColor: 'var(--semi-color-border)',
                      background:
                        'linear-gradient(135deg, var(--semi-color-bg-0) 0%, var(--semi-color-bg-1) 46%, var(--semi-color-fill-0) 100%)',
                      boxShadow: '0 12px 28px rgba(15, 23, 42, 0.12)',
                    }}
                  >
                    <div
                      className='space-y-3 text-sm'
                      style={{ color: 'var(--semi-color-text-1)' }}
                    >
                      <div className='flex items-end justify-between gap-4'>
                        <span style={{ color: 'var(--semi-color-text-2)' }}>
                          {t('支付金额')}
                        </span>
                        {renderAmountHighlight(
                          selectedPresetDetail.paymentAmountText,
                          '#8b5cf6',
                        )}
                      </div>
                      <div className='flex items-end justify-between gap-4'>
                        <span style={{ color: 'var(--semi-color-text-2)' }}>
                          {t('到账余额')}
                        </span>
                        {renderAmountHighlight(
                          selectedPresetDetail.quotaAmountText,
                          'var(--semi-color-text-0)',
                        )}
                      </div>
                      <div className='flex items-center justify-between gap-4'>
                        <span style={{ color: 'var(--semi-color-text-2)' }}>
                          {t('当前倍率')}
                        </span>
                        <span
                          style={{
                            fontFamily: ELEGANT_DISPLAY_FONT,
                            fontSize: '1rem',
                            color: 'var(--semi-color-text-1)',
                          }}
                        >
                          1 {selectedPresetDetail.paymentCurrencyLabel} ={' '}
                          {selectedPresetDetail.currentMultiplier}{' '}
                          {selectedPresetDetail.quotaCurrencyLabel}
                        </span>
                      </div>
                      <div className='flex items-center justify-between gap-4'>
                        <span style={{ color: 'var(--semi-color-text-2)' }}>
                          {t('折扣')}
                        </span>
                        {selectedPresetDetail.discount < 1 ? (
                          <Tag color='green' size='large'>
                            {(selectedPresetDetail.discount * 10).toFixed(1)}
                            {t('折')}
                          </Tag>
                        ) : (
                          <span
                            className='text-sm'
                            style={{ color: 'var(--semi-color-text-2)' }}
                          >
                            {t('无')}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                )}

                <div>
                  <div className='mb-3 flex items-center justify-between gap-3'>
                    <Text strong>{t('选择支付方式')}</Text>
                    <Text type='tertiary' size='small'>
                      {t('已选额度')} {topUpCount}
                    </Text>
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
                <div className='mb-3'>
                  <Text strong>{t('Creem 充值')}</Text>
                </div>
                <div className='grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3'>
                  {creemProducts.map((product, index) => (
                    <Card
                      key={index}
                      onClick={() => creemPreTopUp(product)}
                      className='cursor-pointer !rounded-2xl transition-all hover:shadow-md border-gray-200 hover:border-gray-300'
                      bodyStyle={{ textAlign: 'center', padding: '16px' }}
                    >
                      <div className='font-medium text-lg mb-2'>
                        {product.name}
                      </div>
                      <div className='text-sm text-gray-600 mb-2'>
                        {t('充值额度')}: {product.quota}
                      </div>
                      <div className='text-lg font-semibold text-blue-600'>
                        {product.currency === 'EUR' ? 'EUR ' : '$'}
                        {product.price}
                      </div>
                    </Card>
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
      </Card>

      {redeemCard}
    </Space>
  );

  const redeemCard = (
    <Card
      className='!rounded-xl w-full'
      title={
        <Text type='tertiary' strong>
          {t('兑换码充值')}
        </Text>
      }
    >
      <Form
        getFormApi={(api) => (redeemFormApiRef.current = api)}
        initValues={{ redemptionCode: redemptionCode }}
      >
        <Form.Input
          field='redemptionCode'
          noLabel={true}
          placeholder={t('请输入兑换码')}
          value={redemptionCode}
          onChange={(value) => setRedemptionCode(value)}
          prefix={<IconGift />}
          suffix={
            <div className='flex items-center gap-2'>
              <Button
                type='primary'
                theme='solid'
                onClick={topUp}
                loading={isSubmitting}
              >
                {t('兑换额度')}
              </Button>
            </div>
          }
          showClear
          style={{ width: '100%' }}
          extraText={
            topUpLink && (
              <Text type='tertiary'>
                {t('在找兑换码？')}
                <Text
                  type='secondary'
                  underline
                  className='cursor-pointer'
                  onClick={openTopUpLink}
                >
                  {t('购买兑换码')}
                </Text>
              </Text>
            )
          }
        />
      </Form>
    </Card>
  );

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center justify-between mb-4'>
        <div className='flex items-center'>
          <Avatar size='small' color='blue' className='mr-3 shadow-md'>
            <CreditCard size={16} />
          </Avatar>
          <div>
            <Typography.Text className='text-lg font-medium'>
              {t('账户充值')}
            </Typography.Text>
            <div className='text-xs'>{t('多种充值方式，安全便捷')}</div>
          </div>
        </div>
        <Button icon={<Receipt size={16} />} theme='solid' onClick={onOpenHistory}>
          {t('账单')}
        </Button>
      </div>

      {shouldShowSubscription ? (
        <Tabs type='card' activeKey={activeTab} onChange={setActiveTab}>
          <TabPane
            tab={
              <div className='flex items-center gap-2'>
                <Sparkles size={16} />
                {t('订阅套餐')}
              </div>
            }
            itemKey='subscription'
          >
            <div className='py-2'>
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
              />
              <div className='pt-4'>{redeemCard}</div>
            </div>
          </TabPane>
          <TabPane
            tab={
              <div className='flex items-center gap-2'>
                <Wallet size={16} />
                {t('额度充值')}
              </div>
            }
            itemKey='topup'
          >
            <div className='py-2'>{renderTopupContent()}</div>
          </TabPane>
        </Tabs>
      ) : (
        renderTopupContent()
      )}
    </Card>
  );
};

export default RechargeCard;



