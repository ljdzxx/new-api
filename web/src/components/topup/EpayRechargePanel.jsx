import React, { useEffect, useRef, useState } from 'react';
import {
  Banner,
  Button,
  InputNumber,
  Modal,
  Spin,
  Typography,
} from '@douyinfe/semi-ui';
import { SiAlipay, SiWechat } from 'react-icons/si';
import { CreditCard } from 'lucide-react';
import { API, executePaymentCheckout, showError } from '../../helpers';
import { EPAY_METHOD_NAMES, previewEpayAmount } from '../../helpers/epay';
import { useTranslation } from 'react-i18next';

const money = (amount) =>
  Number(amount || 0).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  });

export default function EpayRechargePanel({
  channels = [],
  exchangeRate,
  amountOptions = [],
}) {
  const { t: translate } = useTranslation();
  const [selected, setSelected] = useState(null);
  const [amount, setAmount] = useState(null);
  const [quote, setQuote] = useState(null);
  const [quoteError, setQuoteError] = useState('');
  const [loading, setLoading] = useState(false);
  const [paying, setPaying] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [quoteVersion, setQuoteVersion] = useState(0);
  const requestSequence = useRef(0);
  const changeAmount = (value) => {
    requestSequence.current++;
    setQuote(null);
    setAmount(value);
    setConfirming(false);
    setQuoteVersion((version) => version + 1);
  };
  const changeMethod = (value) => {
    requestSequence.current++;
    setQuote(null);
    setSelected(value);
    setConfirming(false);
    setQuoteVersion((version) => version + 1);
  };
  const channel = channels.find((item) => item.id === selected?.channelID);
  const selectedMethod = selected?.method;

  useEffect(() => {
    const sequence = ++requestSequence.current;
    setQuote(null);
    setQuoteError('');
    setLoading(false);
    setConfirming(false);
    if (!channel || !selectedMethod || !(Number(amount) > 0)) return;
    setLoading(true);
    const timer = setTimeout(async () => {
      try {
        const response = await API.post('/api/payment/epay/quote', {
          scene: 'topup',
          epay_channel_id: channel.id,
          payment_method: selectedMethod,
          amount_cny: String(amount),
        });
        if (sequence !== requestSequence.current) return;
        if (!response.data.success) throw new Error(response.data.message);
        setQuote(response.data.data);
      } catch (error) {
        if (sequence === requestSequence.current)
          setQuoteError(error.message || translate('获取报价失败'));
      } finally {
        if (sequence === requestSequence.current) setLoading(false);
      }
    }, 250);
    return () => {
      clearTimeout(timer);
      requestSequence.current++;
    };
  }, [channel, selectedMethod, amount, exchangeRate, quoteVersion, translate]);

  const pay = async () => {
    if (!quote?.payable || !channel) return;
    setPaying(true);
    try {
      const response = await API.post('/api/payment/topup/checkout', {
        epay_channel_id: channel.id,
        payment_method: selectedMethod,
        amount_cny: String(amount),
        quote_token: quote.quote_token,
      });
      if (!response.data.success) throw new Error(response.data.message);
      executePaymentCheckout(response.data.data);
      setConfirming(false);
    } catch (error) {
      showError(error.message || translate('发起支付失败'));
      setQuote(null);
      setQuoteError(translate('请重新选择金额获取最新报价'));
      setConfirming(false);
    } finally {
      setPaying(false);
    }
  };

  if (!channels.length) return null;

  return (
    <section className='space-y-5' aria-label={translate('易支付余额充值')}>
      <div className='rounded-2xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-0)] p-4 sm:p-6'>
        <Typography.Title heading={6}>{translate('支付通道')}</Typography.Title>
        <div className='mt-3 grid grid-cols-1 gap-3 md:grid-cols-2'>
          {channels.map((item) => (
            <div
              key={item.id}
              className='rounded-xl border p-4 transition-colors'
              style={{
                borderColor:
                  item.id === channel?.id
                    ? 'var(--semi-color-primary)'
                    : 'var(--semi-color-border)',
                background:
                  item.id === channel?.id
                    ? 'var(--semi-color-primary-light-default)'
                    : 'var(--semi-color-bg-1)',
              }}
            >
              <div className='font-semibold'>{item.name}</div>
              {item.description && (
                <div className='mb-3 mt-1 whitespace-pre-wrap break-words text-xs text-amber-600 dark:text-amber-400'>
                  {item.description}
                </div>
              )}
              <div className='flex flex-wrap gap-2'>
                {item.payment_methods.map((method) => {
                  const active =
                    channel?.id === item.id && selectedMethod === method;
                  return (
                    <button
                      key={method}
                      type='button'
                      aria-pressed={active}
                      disabled={paying}
                      onClick={() =>
                        changeMethod({ channelID: item.id, method })
                      }
                      className='flex min-w-[140px] flex-1 flex-col items-center justify-center gap-2 rounded-lg border px-3 py-3 font-semibold transition-colors'
                      style={{
                        borderColor: active
                          ? 'var(--semi-color-primary)'
                          : 'var(--semi-color-border)',
                        background: active
                          ? 'var(--semi-color-primary-light-active)'
                          : 'var(--semi-color-fill-0)',
                      }}
                    >
                      <span className='flex items-center gap-2'>
                        {method === 'wxpay' ? (
                          <SiWechat size={21} color='#07c160' />
                        ) : method === 'alipay' ? (
                          <SiAlipay size={21} color='#00a4ef' />
                        ) : (
                          <CreditCard size={21} />
                        )}
                        {translate(EPAY_METHOD_NAMES[method] || method)}
                      </span>
                      <span className='text-xs font-normal text-[var(--semi-color-text-2)]'>
                        {translate('单笔实付')} ¥
                        {item.payment_method_limits?.[method]?.min_pay_cny} – ¥
                        {item.payment_method_limits?.[method]?.max_pay_cny}
                      </span>
                      {Number(
                        item.payment_method_limits?.[method]?.daily_limit_cny,
                      ) > 0 && (
                        <span className='text-xs font-normal text-[var(--semi-color-text-2)]'>
                          {translate('每日限额')} ¥
                          {item.payment_method_limits[method].daily_limit_cny} ·{' '}
                          {translate('今日剩余')} ¥
                          {
                            item.payment_method_limits[method]
                              .daily_remaining_cny
                          }
                        </span>
                      )}
                    </button>
                  );
                })}
              </div>
            </div>
          ))}
        </div>
        <div className='mt-4 text-sm text-[var(--semi-color-text-2)]'>
          {channel
            ? `${translate('当前选择')}：${channel.name} · ${translate(EPAY_METHOD_NAMES[selectedMethod] || selectedMethod)}`
            : translate('请选择支付通道及支付方式')}
        </div>
      </div>

      <div className='rounded-2xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-0)] p-4 sm:p-6'>
        <div className='mb-3 text-sm'>{translate('快捷金额（CNY）')}</div>
        <div className='mb-5 grid grid-cols-2 gap-3 sm:grid-cols-3'>
          {amountOptions.map((preset) => {
            const preview = channel
              ? previewEpayAmount(channel, preset, exchangeRate, selectedMethod)
              : null;
            const active = Number(amount) === Number(preset);
            return (
              <button
                type='button'
                key={preset}
                disabled={paying}
                onClick={() => changeAmount(preset)}
                className='relative rounded-lg border px-3 py-4 text-center transition-colors'
                style={{
                  borderColor: active
                    ? 'var(--semi-color-primary)'
                    : preview?.percentage > 0
                      ? 'var(--semi-color-danger)'
                      : 'var(--semi-color-border)',
                  background: active
                    ? 'var(--semi-color-primary-light-default)'
                    : 'var(--semi-color-fill-0)',
                }}
              >
                {preview?.percentage > 0 && (
                  <span
                    className='absolute'
                    style={{
                      top: -10,
                      right: -4,
                      transform: 'rotate(6deg)',
                      background: '#ef4444',
                      color: '#fff',
                      border: '1px solid #fff',
                      borderRadius: 4,
                      padding: '2px 6px',
                      fontSize: 11,
                      fontWeight: 700,
                      lineHeight: '16px',
                    }}
                  >
                    {preview.type === 'bonus' ? '+' : '-'}
                    {Number(preview.percentage.toFixed(4))}%
                  </span>
                )}
                <div className='text-base'>¥{preset}</div>
                {preview && (
                  <div className='mt-1 text-xs text-[var(--semi-color-text-2)]'>
                    {preview.type === 'bonus'
                      ? `${translate('预计到账')} $${money(preview.creditUSD)}`
                      : `${translate('预计实付')} ¥${money(preview.payCNY)}`}
                  </div>
                )}
              </button>
            );
          })}
        </div>
        <label className='mb-2 block text-sm'>
          {translate(
            channel?.topup_offer_type === 'discount'
              ? '优惠前充值金额（CNY）'
              : '自定义金额（CNY）',
          )}
        </label>
        <InputNumber
          value={amount}
          disabled={paying}
          onChange={changeAmount}
          min={0.01}
          max={100000000}
          precision={2}
          step={1}
          prefix='¥'
          style={{ width: '100%' }}
          placeholder={translate('请输入充值金额')}
        />
        <div className='mt-2 text-xs text-[var(--semi-color-text-2)]'>
          {translate('到账基数')} = {translate('输入金额')} ÷ {exchangeRate}{' '}
          CNY/USD
        </div>
        {loading && (
          <div className='mt-3'>
            <Spin size='small' /> {translate('获取报价中')}
          </div>
        )}
        {quoteError && (
          <Banner
            type='danger'
            closeIcon={null}
            description={quoteError}
            className='mt-3'
          />
        )}
        {quote && (
          <div className='mt-4 space-y-1 text-sm'>
            <div>
              {translate('实付金额')}：<strong>¥{quote.pay_cny}</strong>
            </div>
            <div>
              {translate('基础到账')}：${money(quote.base_usd)}
            </div>
            {Number(quote.bonus_usd) > 0 && (
              <div className='text-green-600'>
                {translate('额外赠金')}：${money(quote.bonus_usd)}
              </div>
            )}
            <div>
              {translate('总到账')}：
              <strong>${money(quote.credit_usd)} USD</strong>
            </div>
            {!quote.payable && (
              <Banner
                type='danger'
                closeIcon={null}
                description={quote.unavailable_reason}
              />
            )}
          </div>
        )}
      </div>
      <Button
        block
        size='large'
        theme='solid'
        type='primary'
        disabled={!quote?.payable || loading || paying}
        onClick={() => setConfirming(true)}
      >
        {translate('确认支付')} ¥{quote?.pay_cny || '0.00'}
      </Button>
      <Modal
        title={translate('充值确认')}
        visible={confirming}
        onCancel={() => setConfirming(false)}
        onOk={pay}
        confirmLoading={paying}
        okButtonProps={{ disabled: !quote?.payable }}
      >
        {quote && (
          <div className='space-y-3'>
            <div>
              {channel?.name} ·{' '}
              {translate(EPAY_METHOD_NAMES[selectedMethod] || selectedMethod)}
            </div>
            <div>
              {translate('实付金额')}：¥{quote.pay_cny}
            </div>
            <div>
              {translate('基础到账')}：${money(quote.base_usd)}
            </div>
            <div>
              {translate('额外赠金')}：${money(quote.bonus_usd)}
            </div>
            <div>
              {translate('总到账')}：${money(quote.credit_usd)} USD
            </div>
          </div>
        )}
      </Modal>
    </section>
  );
}
