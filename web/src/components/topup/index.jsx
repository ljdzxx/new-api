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

import React, { useEffect, useState, useContext, useRef } from 'react';
import {
  API,
  showError,
  showInfo,
  showSuccess,
  renderQuota,
  renderQuotaWithAmount,
  copy,
  getQuotaPerUnit,
  executePaymentCheckout,
} from '../../helpers';
import { Modal, Toast } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { UserContext } from '../../context/User';
import { StatusContext } from '../../context/Status';
import { REDEMPTION_REWARD_TYPES } from '../../constants/redemption.constants';

import RechargeCard from './RechargeCard';
import InvitationCard from './InvitationCard';
import TransferModal from './modals/TransferModal';
import PaymentConfirmModal from './modals/PaymentConfirmModal';
import TopupHistoryModal from './modals/TopupHistoryModal';

const TOPUP_PROVIDER_METHODS = {
  stripe: {
    type: 'stripe',
    name: 'Stripe',
    color: 'rgba(var(--semi-purple-5), 1)',
  },
  mall: {
    type: 'mall',
    name: '商城',
    color: 'rgba(var(--semi-orange-5), 1)',
  },
};

const PAYMENT_PROVIDER_EXCLUDED_TYPES = new Set(['stripe', 'creem', 'mall']);

const normalizePayMethods = (rawPayMethods, stripeMinTopup = 0) => {
  let payMethods = rawPayMethods || [];
  if (typeof payMethods === 'string') {
    payMethods = JSON.parse(payMethods);
  }
  if (!Array.isArray(payMethods)) {
    return [];
  }
  return payMethods
    .filter((method) => method?.name && method?.type)
    .map((method) => {
      const normalizedMethod = { ...method };
      if (normalizedMethod.type === 'mall') {
        normalizedMethod.name = '商城';
      }
      const normalizedMinTopup = Number(normalizedMethod.min_topup);
      normalizedMethod.min_topup = Number.isFinite(normalizedMinTopup)
        ? normalizedMinTopup
        : 0;
      if (
        normalizedMethod.type === 'stripe' &&
        (!normalizedMethod.min_topup || normalizedMethod.min_topup <= 0)
      ) {
        const stripeMin = Number(stripeMinTopup);
        if (Number.isFinite(stripeMin)) {
          normalizedMethod.min_topup = stripeMin;
        }
      }
      if (!normalizedMethod.color) {
        if (normalizedMethod.type === 'alipay') {
          normalizedMethod.color = 'rgba(var(--semi-blue-5), 1)';
        } else if (normalizedMethod.type === 'wxpay') {
          normalizedMethod.color = 'rgba(var(--semi-green-5), 1)';
        } else if (normalizedMethod.type === 'stripe') {
          normalizedMethod.color = 'rgba(var(--semi-purple-5), 1)';
        } else {
          normalizedMethod.color = 'rgba(var(--semi-primary-5), 1)';
        }
      }
      return normalizedMethod;
    });
};

const buildPayMethodsByProvider = (providerMeta, normalizedPayMethods) => {
  if (!providerMeta || providerMeta.legacy_auto) {
    return normalizedPayMethods;
  }
  if (!providerMeta.enabled || !providerMeta.config_ready) {
    return [];
  }
  switch (providerMeta.provider) {
    case 'epay':
      return normalizedPayMethods.filter(
        (method) => !PAYMENT_PROVIDER_EXCLUDED_TYPES.has(method.type),
      );
    case 'stripe':
      return [TOPUP_PROVIDER_METHODS.stripe];
    case 'mall':
      return [TOPUP_PROVIDER_METHODS.mall];
    default:
      return [];
  }
};

const resolveDefaultTopUpCount = (amountOptions, fallbackValue) => {
  if (Array.isArray(amountOptions) && amountOptions.length > 0) {
    const firstAmount = Number(amountOptions[0]);
    if (Number.isFinite(firstAmount) && firstAmount > 0) {
      return firstAmount;
    }
  }

  const fallback = Number(fallbackValue);
  if (Number.isFinite(fallback) && fallback > 0) {
    return fallback;
  }

  return 1;
};

const TopUp = () => {
  const { t } = useTranslation();
  const [userState, userDispatch] = useContext(UserContext);
  const [statusState] = useContext(StatusContext);

  const [redemptionCode, setRedemptionCode] = useState('');
  const [amount, setAmount] = useState(0.0);
  const [minTopUp, setMinTopUp] = useState(statusState?.status?.min_topup || 1);
  const [topUpCount, setTopUpCount] = useState(
    statusState?.status?.min_topup || 1,
  );
  const [topUpLink, setTopUpLink] = useState(
    statusState?.status?.top_up_link || '',
  );
  const [enableOnlineTopUp, setEnableOnlineTopUp] = useState(
    statusState?.status?.enable_online_topup || false,
  );
  const [priceRatio, setPriceRatio] = useState(statusState?.status?.price || 1);

  const [enableStripeTopUp, setEnableStripeTopUp] = useState(
    statusState?.status?.enable_stripe_topup || false,
  );
  const [statusLoading, setStatusLoading] = useState(true);

  const [creemProducts, setCreemProducts] = useState([]);
  const [enableCreemTopUp, setEnableCreemTopUp] = useState(false);
  const [creemOpen, setCreemOpen] = useState(false);
  const [selectedCreemProduct, setSelectedCreemProduct] = useState(null);

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [open, setOpen] = useState(false);
  const [payWay, setPayWay] = useState('');
  const [amountLoading, setAmountLoading] = useState(false);
  const [paymentLoading, setPaymentLoading] = useState(false);
  const [confirmLoading, setConfirmLoading] = useState(false);
  const [payMethods, setPayMethods] = useState([]);
  const [hasMallPayMethod, setHasMallPayMethod] = useState(false);

  const affFetchedRef = useRef(false);

  const [affLink, setAffLink] = useState('');
  const [openTransfer, setOpenTransfer] = useState(false);
  const [transferAmount, setTransferAmount] = useState(0);

  const [openHistory, setOpenHistory] = useState(false);

  const [subscriptionPlans, setSubscriptionPlans] = useState([]);
  const [subscriptionLoading, setSubscriptionLoading] = useState(true);
  const [billingPreference, setBillingPreference] =
    useState('subscription_first');
  const [activeSubscriptions, setActiveSubscriptions] = useState([]);
  const [allSubscriptions, setAllSubscriptions] = useState([]);

  const [presetAmounts, setPresetAmounts] = useState([]);
  const [selectedPreset, setSelectedPreset] = useState(null);

  const [topupInfo, setTopupInfo] = useState({
    amount_options: [],
    discount: {},
    mall_links: {},
  });

  const topUp = async () => {
    if (redemptionCode === '') {
      showInfo(t('请输入兑换码！'));
      return;
    }
    setIsSubmitting(true);
    try {
      const res = await API.post('/api/user/topup', {
        key: redemptionCode,
      });
      const { success, message, data } = res.data;
      if (success) {
        const rewardType =
          Number(data?.reward_type || REDEMPTION_REWARD_TYPES.QUOTA) ||
          REDEMPTION_REWARD_TYPES.QUOTA;
        showSuccess(t('兑换成功！'));
        if (rewardType === REDEMPTION_REWARD_TYPES.SUBSCRIPTION) {
          Modal.success({
            title: t('兑换成功！'),
            content: t('订阅已兑换成功，套餐 #{{planId}}', {
              planId: Number(data?.plan_id || 0) || '-',
            }),
            centered: true,
          });
          await getSubscriptionSelf();
        } else {
          const quotaValue = Number(data?.quota || 0);
          Modal.success({
            title: t('兑换成功！'),
            content: t('额度已兑换成功 {{quota}}', {
              quota: renderQuota(quotaValue),
            }),
            centered: true,
          });
          if (userState.user) {
            const updatedUser = {
              ...userState.user,
              quota: userState.user.quota + quotaValue,
            };
            userDispatch({ type: 'login', payload: updatedUser });
          }
        }
        setRedemptionCode('');
      } else {
        showError(message);
      }
    } catch (err) {
      showError(t('操作失败'));
    } finally {
      setIsSubmitting(false);
    }
  };

  const openTopUpLink = () => {
    if (!topUpLink) {
      showError('Top-up link is not configured');
      return;
    }
    window.open(topUpLink, '_blank');
  };

  const requestUnifiedTopupCheckout = async (payload) => {
    const res = await API.post('/api/payment/topup/checkout', payload);
    if (!res.data?.success) {
      throw new Error(res.data?.message || 'Payment failed');
    }
    executePaymentCheckout(res.data.data);
    showSuccess('Payment started');
  };

  const preTopUp = async (payment) => {
    if (payment === 'stripe') {
      if (!enableStripeTopUp) {
        showError('Stripe top-up is not enabled');
        return;
      }
    } else if (payment === 'mall') {
      if (!hasMallPayMethod) {
        showError(t('商城支付方式暂不可用'));
        return;
      }
    } else {
      if (!enableOnlineTopUp) {
        showError('Online top-up is not enabled');
        return;
      }
    }

    setPayWay(payment);
    setPaymentLoading(true);
    try {
      if (payment === 'stripe') {
        await getStripeAmount();
      } else {
        await getAmount();
      }

      if (topUpCount < minTopUp) {
        showError('Top-up amount is below minimum ' + minTopUp);
        return;
      }
      setOpen(true);
    } catch (error) {
      showError('Failed to get amount');
    } finally {
      setPaymentLoading(false);
    }
  };

  const onlineTopUp = async () => {
    if (payWay === 'stripe') {
      if (amount === 0) {
        await getStripeAmount();
      }
    } else if (payWay !== 'mall') {
      if (amount === 0) {
        await getAmount();
      }
    }
    if (topUpCount < minTopUp) {
      showError('Top-up amount is below minimum');
      return;
    }
    setConfirmLoading(true);
    try {
      await requestUnifiedTopupCheckout({
        amount: parseInt(topUpCount),
        payment_method: payWay,
      });
    } catch (err) {
      console.log(err);
      showError('Payment request failed');
    } finally {
      setOpen(false);
      setConfirmLoading(false);
    }
  };
  const creemPreTopUp = async (product) => {
    if (!enableCreemTopUp) {
      showError('Creem top-up is not enabled');
      return;
    }
    setSelectedCreemProduct(product);
    setCreemOpen(true);
  };

  const onlineCreemTopUp = async () => {
    if (!selectedCreemProduct) {
      showError('Please select a product');
      return;
    }
    if (!selectedCreemProduct.productId) {
      showError('Product configuration is invalid');
      return;
    }
    setConfirmLoading(true);
    try {
      await requestUnifiedTopupCheckout({
        product_id: selectedCreemProduct.productId,
        payment_method: 'creem',
      });
    } catch (err) {
      console.log(err);
      showError('Payment request failed');
    } finally {
      setCreemOpen(false);
      setConfirmLoading(false);
    }
  };
  const getUserQuota = async () => {
    let res = await API.get(`/api/user/self`);
    const { success, message, data } = res.data;
    if (success) {
      userDispatch({ type: 'login', payload: data });
    } else {
      showError(message);
    }
  };

  const getSubscriptionPlans = async () => {
    setSubscriptionLoading(true);
    try {
      const res = await API.get('/api/subscription/plans');
      if (res.data?.success) {
        setSubscriptionPlans(res.data.data || []);
      }
    } catch (e) {
      setSubscriptionPlans([]);
    } finally {
      setSubscriptionLoading(false);
    }
  };

  const getSubscriptionSelf = async () => {
    try {
      const res = await API.get('/api/subscription/self');
      if (res.data?.success) {
        setBillingPreference(
          res.data.data?.billing_preference || 'subscription_first',
        );
        // Active subscriptions
        const activeSubs = res.data.data?.subscriptions || [];
        setActiveSubscriptions(activeSubs);
        // All subscriptions (including expired)
        const allSubs = res.data.data?.all_subscriptions || [];
        setAllSubscriptions(allSubs);
      }
    } catch (e) {
      // ignore
    }
  };

  const updateBillingPreference = async (pref) => {
    const previousPref = billingPreference;
    setBillingPreference(pref);
    try {
      const res = await API.put('/api/subscription/self/preference', {
        billing_preference: pref,
      });
      if (res.data?.success) {
        showSuccess('Updated successfully');
        const normalizedPref =
          res.data?.data?.billing_preference || pref || previousPref;
        setBillingPreference(normalizedPref);
      } else {
        showError(res.data?.message || t('更新失败'));
        setBillingPreference(previousPref);
      }
    } catch (e) {
      showError(t('操作失败'));
      setBillingPreference(previousPref);
    }
  };

  // Load top-up meta and provider capabilities
  const getTopupInfo = async () => {
    try {
      const res = await API.get('/api/payment/topup/meta');
      const { data, success } = res.data;
      if (!success) {
        console.error('Failed to load top-up info', data);
        return;
      }

      const providerMeta = data.provider_meta || null;
      setTopupInfo({
        amount_options: data.amount_options || [],
        discount: data.discount || {},
        mall_links: data.mall_links || {},
      });

      const normalizedPrice = Number(data?.price);
      if (Number.isFinite(normalizedPrice) && normalizedPrice > 0) {
        setPriceRatio(normalizedPrice);
      }

      let payMethods = data.pay_methods || [];
      try {
        if (typeof payMethods === 'string') {
          payMethods = JSON.parse(payMethods);
        }
        if (payMethods && payMethods.length > 0) {
          payMethods = payMethods.filter((method) => method.name && method.type);
          payMethods = payMethods.map((method) => {
            const normalizedMinTopup = Number(method.min_topup);
            method.min_topup = Number.isFinite(normalizedMinTopup)
              ? normalizedMinTopup
              : 0;

            if (
              method.type === 'stripe' &&
              (!method.min_topup || method.min_topup <= 0)
            ) {
              const stripeMin = Number(data.stripe_min_topup);
              if (Number.isFinite(stripeMin)) {
                method.min_topup = stripeMin;
              }
            }

            if (!method.color) {
              if (method.type === 'alipay') {
                method.color = 'rgba(var(--semi-blue-5), 1)';
              } else if (method.type === 'wxpay') {
                method.color = 'rgba(var(--semi-green-5), 1)';
              } else if (method.type === 'stripe') {
                method.color = 'rgba(var(--semi-purple-5), 1)';
              } else {
                method.color = 'rgba(var(--semi-primary-5), 1)';
              }
            }

            return method;
          });
        } else {
          payMethods = [];
        }

        setPayMethods(payMethods);
        const enableStripeTopUp = data.enable_stripe_topup || false;
        const enableOnlineTopUp = data.enable_online_topup || false;
        const enableCreemTopUp = data.enable_creem_topup || false;
        const hasMallMethod = payMethods.some((method) => method.type === 'mall');
        const minTopUpValue = enableOnlineTopUp || hasMallMethod
          ? data.min_topup
          : enableStripeTopUp
            ? data.stripe_min_topup
            : 1;
        let resolvedMinTopUp = minTopUpValue;

        setHasMallPayMethod(hasMallMethod);
        setEnableOnlineTopUp(enableOnlineTopUp);
        setEnableStripeTopUp(enableStripeTopUp);
        setEnableCreemTopUp(enableCreemTopUp);
        setMinTopUp(minTopUpValue);

        if (providerMeta && !providerMeta.legacy_auto) {
          const providerPayMethods = buildPayMethodsByProvider(
            providerMeta,
            normalizePayMethods(data.pay_methods || [], data.stripe_min_topup),
          );
          const currentProvider = providerMeta.provider;
          const currentProviderReady =
            providerMeta.enabled && providerMeta.config_ready;
          resolvedMinTopUp = currentProvider === 'stripe'
            ? data.stripe_min_topup
            : currentProvider === 'epay' || currentProvider === 'mall'
              ? data.min_topup
              : 1;

          setPayMethods(providerPayMethods);
          setEnableOnlineTopUp(
            currentProvider === 'epay' && currentProviderReady,
          );
          setEnableStripeTopUp(
            currentProvider === 'stripe' && currentProviderReady,
          );
          setEnableCreemTopUp(
            currentProvider === 'creem' && currentProviderReady,
          );
          setHasMallPayMethod(
            currentProvider === 'mall' && currentProviderReady,
          );
          setMinTopUp(resolvedMinTopUp);
        }

        try {
          const products = JSON.parse(data.creem_products || '[]');
          setCreemProducts(products);
        } catch (e) {
          setCreemProducts([]);
        }

        let defaultTopUpCount = resolvedMinTopUp;
        if (data.amount_options && data.amount_options.length > 0) {
          const customPresets = data.amount_options.map((amount) => ({
            value: amount,
            discount: data.discount[amount] || 1.0,
          }));
          setPresetAmounts(customPresets);
          defaultTopUpCount = resolveDefaultTopUpCount(
            data.amount_options,
            resolvedMinTopUp,
          );
        } else {
          const generatedPresets = generatePresetAmounts(resolvedMinTopUp);
          setPresetAmounts(generatedPresets);
          defaultTopUpCount = resolveDefaultTopUpCount(
            generatedPresets.map((preset) => preset.value),
            resolvedMinTopUp,
          );
        }

        setTopUpCount(defaultTopUpCount);
        setSelectedPreset(defaultTopUpCount);
        getAmount(defaultTopUpCount);
      } catch (e) {
        console.log('Failed to normalize top-up methods', e);
        setPayMethods([]);
        setHasMallPayMethod(false);
      }
    } catch (error) {
      console.error('Failed to load top-up info', error);
    }
  };

  // Load invitation link
  const getAffLink = async () => {
    const res = await API.get('/api/user/aff');
    const { success, message, data } = res.data;
    if (success) {
      const link = `${window.location.origin}/register?aff=${data}`;
      setAffLink(link);
    } else {
      showError(message);
    }
  };

  // Transfer quota to a referred user
  const transfer = async () => {
    if (transferAmount < getQuotaPerUnit()) {
      showError('Transfer amount must be at least ' + renderQuota(getQuotaPerUnit()));
      return;
    }
    const res = await API.post(`/api/user/aff_transfer`, {
      quota: transferAmount,
    });
    const { success, message } = res.data;
    if (success) {
      showSuccess(message);
      setOpenTransfer(false);
      getUserQuota().then();
    } else {
      showError(message);
    }
  };

  // Copy invitation link
  const handleAffLinkClick = async () => {
    await copy(affLink);
    showSuccess('Invitation link copied');
  };

  useEffect(() => {
    getUserQuota().then();
    setTransferAmount(getQuotaPerUnit());
  }, []);

  useEffect(() => {
    if (affFetchedRef.current) return;
    affFetchedRef.current = true;
    getAffLink().then();
  }, []);

  useEffect(() => {
    getTopupInfo().then();
    getSubscriptionPlans().then();
    getSubscriptionSelf().then();
  }, []);

  useEffect(() => {
    if (statusState?.status) {
      setTopUpLink(statusState.status.top_up_link || '');
      setStatusLoading(false);
    }
  }, [statusState?.status]);

  const renderAmount = () => {
    return String(amount);
  };

  const getAmount = async (value) => {
    if (value === undefined) {
      value = topUpCount;
    }
    setAmountLoading(true);
    try {
      const res = await API.post('/api/user/amount', {
        amount: parseFloat(value),
      });
      if (res !== undefined) {
        const { message, data } = res.data;
        if (message === 'success') {
          setAmount(parseFloat(data));
        } else {
          setAmount(0);
          Toast.error({ content: 'Error: ' + data, id: 'getAmount' });
        }
      } else {
        showError(res);
      }
    } catch (err) {
      console.log(err);
    }
    setAmountLoading(false);
  };

  const getStripeAmount = async (value) => {
    if (value === undefined) {
      value = topUpCount;
    }
    setAmountLoading(true);
    try {
      const res = await API.post('/api/user/stripe/amount', {
        amount: parseFloat(value),
      });
      if (res !== undefined) {
        const { message, data } = res.data;
        if (message === 'success') {
          setAmount(parseFloat(data));
        } else {
          setAmount(0);
          Toast.error({ content: 'Error: ' + data, id: 'getAmount' });
        }
      } else {
        showError(res);
      }
    } catch (err) {
      console.log(err);
    } finally {
      setAmountLoading(false);
    }
  };

  const handleCancel = () => {
    setOpen(false);
  };

  const handleTransferCancel = () => {
    setOpenTransfer(false);
  };

  const handleOpenHistory = () => {
    setOpenHistory(true);
  };

  const handleHistoryCancel = () => {
    setOpenHistory(false);
  };

  const handleCreemCancel = () => {
    setCreemOpen(false);
    setSelectedCreemProduct(null);
  };

  const selectPresetAmount = (preset) => {
    setTopUpCount(preset.value);
    setSelectedPreset(preset.value);
    const discount = preset.discount || topupInfo.discount[preset.value] || 1.0;
    const discountedAmount = preset.value * priceRatio * discount;
    setAmount(discountedAmount);
  };

  const formatLargeNumber = (num) => {
    return num.toString();
  };

  const generatePresetAmounts = (minAmount) => {
    const multipliers = [1, 5, 10, 30, 50, 100, 300, 500];
    return multipliers.map((multiplier) => ({
      value: minAmount * multiplier,
    }));
  };


  return (
    <div className='w-full max-w-7xl mx-auto relative min-h-screen lg:min-h-0 mt-[60px] px-2'>
      {/* Transfer modal */}
      <TransferModal
        t={t}
        openTransfer={openTransfer}
        transfer={transfer}
        handleTransferCancel={handleTransferCancel}
        userState={userState}
        renderQuota={renderQuota}
        getQuotaPerUnit={getQuotaPerUnit}
        transferAmount={transferAmount}
        setTransferAmount={setTransferAmount}
      />

      {/* Payment confirmation modal */}
      <PaymentConfirmModal
        t={t}
        open={open}
        onlineTopUp={onlineTopUp}
        handleCancel={handleCancel}
        confirmLoading={confirmLoading}
        topUpCount={topUpCount}
        renderQuotaWithAmount={renderQuotaWithAmount}
        amountLoading={amountLoading}
        renderAmount={renderAmount}
        payWay={payWay}
        payMethods={payMethods}
        amountNumber={amount}
        discountRate={topupInfo?.discount?.[topUpCount] || 1.0}
        topupRate={priceRatio}
      />

      {/* Top-up history modal */}
      <TopupHistoryModal
        visible={openHistory}
        onCancel={handleHistoryCancel}
        t={t}
      />

      {/* Creem confirmation modal */}
      <Modal
        title={t('确认 Creem 充值')}
        visible={creemOpen}
        onOk={onlineCreemTopUp}
        onCancel={handleCreemCancel}
        maskClosable={false}
        size='small'
        centered
        confirmLoading={confirmLoading}
      >
        {selectedCreemProduct && (
          <>
            <p>
              {'Product: '}{selectedCreemProduct.name}
            </p>
            <p>
              {'Price: '}{selectedCreemProduct.currency === 'EUR' ? 'EUR ' : '$'}
              {selectedCreemProduct.price}
            </p>
            <p>
              {'Quota: '}{selectedCreemProduct.quota}
            </p>
            <p>{'Confirm top-up?'}</p>
          </>
        )}
      </Modal>

      {/* Main content */}
      <div className='grid grid-cols-1 lg:grid-cols-2 gap-6'>
        <RechargeCard
          t={t}
          enableOnlineTopUp={enableOnlineTopUp}
          enableStripeTopUp={enableStripeTopUp}
          enableCreemTopUp={enableCreemTopUp}
          creemProducts={creemProducts}
          creemPreTopUp={creemPreTopUp}
          presetAmounts={presetAmounts}
          selectedPreset={selectedPreset}
          selectPresetAmount={selectPresetAmount}
          formatLargeNumber={formatLargeNumber}
          priceRatio={priceRatio}
          topUpCount={topUpCount}
          minTopUp={minTopUp}
          renderQuotaWithAmount={renderQuotaWithAmount}
          getAmount={getAmount}
          setTopUpCount={setTopUpCount}
          setSelectedPreset={setSelectedPreset}
          renderAmount={renderAmount}
          amountLoading={amountLoading}
          payMethods={payMethods}
          hasMallPayMethod={hasMallPayMethod}
          preTopUp={preTopUp}
          paymentLoading={paymentLoading}
          payWay={payWay}
          redemptionCode={redemptionCode}
          setRedemptionCode={setRedemptionCode}
          topUp={topUp}
          isSubmitting={isSubmitting}
          topUpLink={topUpLink}
          openTopUpLink={openTopUpLink}
          userState={userState}
          renderQuota={renderQuota}
          statusLoading={statusLoading}
          topupInfo={topupInfo}
          onOpenHistory={handleOpenHistory}
          subscriptionLoading={subscriptionLoading}
          subscriptionPlans={subscriptionPlans}
          billingPreference={billingPreference}
          onChangeBillingPreference={updateBillingPreference}
          activeSubscriptions={activeSubscriptions}
          allSubscriptions={allSubscriptions}
          reloadSubscriptionSelf={getSubscriptionSelf}
        />
        <InvitationCard
          t={t}
          userState={userState}
          renderQuota={renderQuota}
          setOpenTransfer={setOpenTransfer}
          affLink={affLink}
          handleAffLinkClick={handleAffLinkClick}
        />
      </div>
    </div>
  );
};

export default TopUp;

