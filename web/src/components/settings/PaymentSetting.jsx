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

import React, { useEffect, useState } from 'react';
import { Card, Spin } from '@douyinfe/semi-ui';
import SettingsPaymentRouting from '../../pages/Setting/Payment/SettingsPaymentRouting';
import SettingsGeneralPayment from '../../pages/Setting/Payment/SettingsGeneralPayment';
import SettingsPaymentGateway from '../../pages/Setting/Payment/SettingsPaymentGateway';
import SettingsPaymentGatewayStripe from '../../pages/Setting/Payment/SettingsPaymentGatewayStripe';
import SettingsPaymentGatewayCreem from '../../pages/Setting/Payment/SettingsPaymentGatewayCreem';
import { API, showError, toBoolean } from '../../helpers';
import { useTranslation } from 'react-i18next';

const PaymentSetting = () => {
  const { t } = useTranslation();
  const [inputs, setInputs] = useState({
    ServerAddress: '',
    PayAddress: '',
    EpayId: '',
    EpayKey: '',
    Price: 7.3,
    MinTopUp: 1,
    TopupGroupRatio: '',
    CustomCallbackAddress: '',
    PayMethods: '',
    AmountOptions: '',
    AmountDiscount: '',
    MallLinks: '',

    StripeApiSecret: '',
    StripeWebhookSecret: '',
    StripePriceId: '',
    StripeUnitPrice: 8.0,
    StripeMinTopUp: 1,
    StripePromotionCodesEnabled: false,
    TopupProvider: 'legacy_auto',
    SubscriptionProvider: 'legacy_auto',
    'payment_setting.display_currency_type': 'FOLLOW_QUOTA',
    'payment_setting.display_currency_symbol': '\u00A4',
    'payment_setting.display_currency_exchange_rate': '1',
  });

  const [loading, setLoading] = useState(false);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (!success) {
      showError(t(message));
      return;
    }

    const newInputs = {};
    data.forEach((item) => {
      switch (item.key) {
        case 'TopupGroupRatio':
          try {
            newInputs[item.key] = JSON.stringify(JSON.parse(item.value), null, 2);
          } catch (error) {
            console.error('解析 TopupGroupRatio 出错:', error);
            newInputs[item.key] = item.value;
          }
          break;
        case 'payment_setting.amount_options':
          try {
            newInputs.AmountOptions = JSON.stringify(JSON.parse(item.value), null, 2);
          } catch (error) {
            console.error('解析 AmountOptions 出错:', error);
            newInputs.AmountOptions = item.value;
          }
          break;
        case 'payment_setting.amount_discount':
          try {
            newInputs.AmountDiscount = JSON.stringify(JSON.parse(item.value), null, 2);
          } catch (error) {
            console.error('解析 AmountDiscount 出错:', error);
            newInputs.AmountDiscount = item.value;
          }
          break;
        case 'payment_setting.mall_links':
          try {
            newInputs.MallLinks = JSON.stringify(JSON.parse(item.value), null, 2);
          } catch (error) {
            console.error('解析 MallLinks 出错:', error);
            newInputs.MallLinks = item.value;
          }
          break;
        case 'payment_route.topup_provider':
          newInputs.TopupProvider = item.value || 'legacy_auto';
          break;
        case 'payment_route.subscription_provider':
          newInputs.SubscriptionProvider = item.value || 'legacy_auto';
          break;
        case 'Price':
        case 'MinTopUp':
        case 'StripeUnitPrice':
        case 'StripeMinTopUp':
          newInputs[item.key] = parseFloat(item.value);
          break;
        default:
          if (item.key.endsWith('Enabled')) {
            newInputs[item.key] = toBoolean(item.value);
          } else {
            newInputs[item.key] = item.value;
          }
          break;
      }
    });

    console.log('[payment-debug][PaymentSetting] getOptions parsed values', {
      paymentDisplayType: newInputs['payment_setting.display_currency_type'],
      paymentDisplaySymbol: newInputs['payment_setting.display_currency_symbol'],
      paymentDisplayRate:
        newInputs['payment_setting.display_currency_exchange_rate'],
      serverAddress: newInputs.ServerAddress,
    });

    setInputs((prev) => ({ ...prev, ...newInputs }));
  };

  async function onRefresh() {
    try {
      setLoading(true);
      await getOptions();
    } catch (error) {
      showError(t('刷新失败'));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    onRefresh();
  }, []);

  return (
    <Spin spinning={loading} size='large'>
      <Card style={{ marginTop: '10px' }}>
        <SettingsPaymentRouting options={inputs} refresh={onRefresh} />
      </Card>
      <Card style={{ marginTop: '10px' }}>
        <SettingsGeneralPayment options={inputs} refresh={onRefresh} />
      </Card>
      <Card style={{ marginTop: '10px' }}>
        <SettingsPaymentGateway options={inputs} refresh={onRefresh} />
      </Card>
      <Card style={{ marginTop: '10px' }}>
        <SettingsPaymentGatewayStripe options={inputs} refresh={onRefresh} />
      </Card>
      <Card style={{ marginTop: '10px' }}>
        <SettingsPaymentGatewayCreem options={inputs} refresh={onRefresh} />
      </Card>
    </Spin>
  );
};

export default PaymentSetting;
