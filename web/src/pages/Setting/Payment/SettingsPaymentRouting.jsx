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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Form, Spin } from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsPaymentRouting(props) {
  const { t } = useTranslation();
  const providerOptions = [
    { value: 'legacy_auto', label: 'Legacy Auto' },
    { value: 'disabled', label: 'Disabled' },
    { value: 'epay', label: 'Epay' },
    { value: 'stripe', label: 'Stripe' },
    { value: 'creem', label: 'Creem' },
    { value: 'mall', label: t('商城') },
  ];
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    TopupProvider: 'legacy_auto',
    SubscriptionProvider: 'legacy_auto',
  });
  const [originInputs, setOriginInputs] = useState(inputs);
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        TopupProvider: props.options.TopupProvider || 'legacy_auto',
        SubscriptionProvider:
          props.options.SubscriptionProvider || 'legacy_auto',
      };
      setInputs(currentInputs);
      setOriginInputs(currentInputs);
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const modeDescription = useMemo(() => {
    if (
      inputs.TopupProvider === 'legacy_auto' ||
      inputs.SubscriptionProvider === 'legacy_auto'
    ) {
      return t(
        '当前仍可使用兼容模式。兼容模式下会继续按照旧规则推断支付路由，建议在确认配置完整后切换到显式支付渠道。',
      );
    }
    return t(
      '显式路由模式下，充值和订阅都会严格按照这里选择的支付渠道执行，不再根据字段是否存在自动猜测。',
    );
  }, [inputs.SubscriptionProvider, inputs.TopupProvider, t]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitPaymentRouting = async () => {
    setLoading(true);
    try {
      const options = [];
      if (originInputs.TopupProvider !== inputs.TopupProvider) {
        options.push({
          key: 'payment_route.topup_provider',
          value: inputs.TopupProvider,
        });
      }
      if (
        originInputs.SubscriptionProvider !== inputs.SubscriptionProvider
      ) {
        options.push({
          key: 'payment_route.subscription_provider',
          value: inputs.SubscriptionProvider,
        });
      }

      if (options.length === 0) {
        showSuccess(t('没有变更需要保存'));
        setLoading(false);
        return;
      }

      const requestQueue = options.map((opt) =>
        API.put('/api/option/', {
          key: opt.key,
          value: opt.value,
        }),
      );
      const results = await Promise.all(requestQueue);
      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => {
          showError(res.data.message);
        });
      } else {
        showSuccess(t('更新成功'));
        setOriginInputs({ ...inputs });
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    }
    setLoading(false);
  };

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={t('支付路由')}>
          <Banner type='info' description={modeDescription} closeIcon={null} />
          <Form.Select
            field='TopupProvider'
            label={t('充值支付渠道')}
            optionList={providerOptions}
            placeholder={t('请选择充值支付渠道')}
          />
          <Form.Select
            field='SubscriptionProvider'
            label={t('订阅支付渠道')}
            optionList={providerOptions}
            placeholder={t('请选择订阅支付渠道')}
          />
          <Button onClick={submitPaymentRouting}>
            {t('更新支付路由')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
