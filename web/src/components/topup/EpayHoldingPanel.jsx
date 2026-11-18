import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Modal } from '@douyinfe/semi-ui';
import { API, showError } from '../../helpers';
import { EPAY_METHOD_NAMES } from '../../helpers/epay';
import { submitPaymentForm } from '../../helpers/payment';

export function useEpayHolding(userID) {
  const [order, setOrder] = useState(null);
  const [ready, setReady] = useState(false);
  const [outcome, setOutcome] = useState(null);
  const tracked = useRef(null);
  const flight = useRef(null);
  const storageKey = `epay-holding:${userID}`;
  const remember = useCallback(
    (target) => {
      tracked.current = target;
      try {
        if (target) sessionStorage.setItem(storageKey, JSON.stringify(target));
        else sessionStorage.removeItem(storageKey);
      } catch {
        /* Storage is optional in private browsing. */
      }
    },
    [storageKey],
  );
  const refresh = useCallback(async () => {
    if (!userID || flight.current) return;
    const request = {};
    flight.current = request;
    const target = tracked.current;
    try {
      if (target) {
        const { data, status } = await API.get('/api/payment/epay/order', {
          params: target,
          disableDuplicate: true,
          validateStatus: (code) => code === 200 || code === 404,
        });
        if (flight.current !== request) return;
        if (status !== 404) {
          const row = data.data;
          if (
            !data.success ||
            row?.trade_no !== target.trade_no ||
            row.scene !== target.scene
          )
            return;
          if (row.status === 'pending') {
            setOrder(row);
            setReady(true);
            return;
          }
          setOutcome(row);
        }
        remember(null);
        setOrder(null);
      }
      const { data } = await API.get('/api/payment/epay/pending', {
        disableDuplicate: true,
      });
      if (data.success && flight.current === request) {
        if (data.data)
          remember({ trade_no: data.data.trade_no, scene: data.data.scene });
        setOrder(data.data);
        setReady(true);
      }
    } catch {
      /* Retain the known state until the next successful poll. */
    } finally {
      if (flight.current === request) flight.current = null;
    }
  }, [userID, remember]);
  const trackCheckout = (checkout) => {
    const tradeNo = checkout?.form?.fields?.out_trade_no;
    if (!tradeNo || checkout.provider !== 'epay') return refresh();
    flight.current = null;
    remember({ trade_no: tradeNo, scene: checkout.scene });
    setOutcome(null);
    setReady(false);
    return refresh();
  };
  useEffect(() => {
    tracked.current = null;
    try {
      const saved = JSON.parse(sessionStorage.getItem(storageKey));
      if (saved?.trade_no && ['topup', 'subscription'].includes(saved.scene))
        tracked.current = saved;
    } catch {
      /* Ignore unavailable or invalid session storage. */
    }
    setReady(false);
    setOrder(null);
    setOutcome(null);
    refresh();
    window.addEventListener('focus', refresh);
    return () => {
      flight.current = null;
      window.removeEventListener('focus', refresh);
    };
  }, [refresh, storageKey]);
  useEffect(() => {
    const timer = setInterval(refresh, order || !ready ? 4000 : 30000);
    return () => clearInterval(timer);
  }, [refresh, order?.trade_no, ready]);
  return {
    order,
    ready,
    refresh,
    trackCheckout,
    outcome,
    dismiss: () => setOutcome(null),
  };
}

const cardClass =
  'rounded-2xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)] shadow-sm';
const buttonClass =
  'inline-flex items-center justify-center rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] px-4 py-2.5 text-sm font-medium text-[var(--semi-color-text-0)] shadow-sm transition-colors enabled:hover:bg-[var(--semi-color-fill-1)] focus-visible:outline-[var(--semi-color-primary)] disabled:cursor-not-allowed disabled:opacity-50';

export function EpayHoldingPanel({ order, refresh, t }) {
  const [now, setNow] = useState(Date.now());
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const seconds = Math.max(0, order.expires_at - Math.floor(now / 1000));
  const scene = order.scene === 'subscription' ? t('订阅套餐') : t('余额充值');
  const method = order.payment_method || order.form?.fields?.type;
  const cancel = async () => {
    setBusy(true);
    try {
      const { data } = await API.post('/api/payment/epay/cancel', {
        trade_no: order.trade_no,
        scene: order.scene,
      });
      if (!data.success) throw new Error(data.message);
    } catch (err) {
      showError(t(err.message));
    } finally {
      setConfirming(false);
      await refresh();
      setBusy(false);
    }
  };
  return (
    <section
      aria-label={t('等待支付')}
      className='mx-auto w-full max-w-[960px] space-y-4 px-4 py-8 text-center sm:px-8'
    >
      <p className='break-words text-sm text-[var(--semi-color-text-2)]'>
        {order.channel_name || scene}
        {method && ` · ${t(EPAY_METHOD_NAMES[method] || method)}`}
      </p>
      <div className={`${cardClass} p-6`}>
        <div className='flex flex-col items-center gap-4 py-4'>
          <span
            aria-hidden='true'
            className='h-10 w-10 rounded-full border-4 border-[var(--semi-color-primary)] motion-safe:animate-spin'
            style={{ borderTopColor: 'transparent' }}
          />
          <p className='text-sm text-[var(--semi-color-text-2)]'>
            {seconds
              ? t('支付页面已在新窗口打开，请在新窗口中完成支付后返回此页面')
              : t('正在确认订单状态')}
          </p>
          {order.form && (
            <button
              type='button'
              className={buttonClass}
              disabled={!seconds || busy}
              onClick={() => submitPaymentForm(order.form, true)}
            >
              {t('重新打开支付页面')}
            </button>
          )}
        </div>
      </div>
      <div className={`${cardClass} p-4`}>
        <p
          role='timer'
          aria-label={t('剩余时间')}
          className='mt-1 text-2xl font-bold tabular-nums text-[var(--semi-color-text-0)]'
        >
          {String(Math.floor(seconds / 60)).padStart(2, '0')}:
          {String(seconds % 60).padStart(2, '0')}
        </p>
        <p className='mt-1 text-xs text-[var(--semi-color-text-2)]'>
          {t('等待支付')}...
        </p>
      </div>
      <button
        type='button'
        className={`${buttonClass} w-full`}
        disabled={!seconds || busy}
        onClick={() => setConfirming(true)}
      >
        {t('取消订单')}
      </button>
      <Modal
        title={t('取消订单')}
        visible={confirming}
        onOk={cancel}
        onCancel={() => setConfirming(false)}
        confirmLoading={busy}
      >
        {t('取消后请关闭收银台。此订单后续付款不会入账。')}
      </Modal>
    </section>
  );
}
