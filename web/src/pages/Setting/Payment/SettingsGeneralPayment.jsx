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

import React, { useEffect, useMemo, useState } from 'react';
import {
  Button,
  Col,
  Form,
  Input,
  InputGroup,
  Row,
  Select,
  Spin,
} from '@douyinfe/semi-ui';
import { API, removeTrailingSlash, showError, showSuccess } from '../../../helpers';
import { useTranslation } from 'react-i18next';

const PAYMENT_DISPLAY_TYPE_FIELD = 'payment_setting.display_currency_type';
const PAYMENT_DISPLAY_SYMBOL_FIELD = 'payment_setting.display_currency_symbol';
const PAYMENT_DISPLAY_RATE_FIELD = 'payment_setting.display_currency_exchange_rate';

const DEFAULT_PAYMENT_DISPLAY_INPUTS = {
  ServerAddress: '',
  [PAYMENT_DISPLAY_TYPE_FIELD]: 'FOLLOW_QUOTA',
  [PAYMENT_DISPLAY_SYMBOL_FIELD]: '\u00A4',
  [PAYMENT_DISPLAY_RATE_FIELD]: '1',
};

function syncPaymentDisplayToLocalStorage(values, usdExchangeRate) {
  const nextType = values[PAYMENT_DISPLAY_TYPE_FIELD] || 'FOLLOW_QUOTA';
  const nextSymbol = values[PAYMENT_DISPLAY_SYMBOL_FIELD] || '\u00A4';
  const nextRate = String(values[PAYMENT_DISPLAY_RATE_FIELD] || '1');

  console.log('[payment-debug][SettingsGeneralPayment] sync localStorage', {
    nextType,
    nextSymbol,
    nextRate,
    usdExchangeRate,
  });

  localStorage.setItem('payment_display_currency_type', nextType);
  localStorage.setItem('payment_display_currency_symbol', nextSymbol);
  localStorage.setItem('payment_display_currency_exchange_rate', nextRate);

  const statusRaw = localStorage.getItem('status');
  if (!statusRaw) {
    return;
  }

  try {
    const status = JSON.parse(statusRaw);
    status.payment_display_currency_type = nextType;
    status.payment_display_currency_symbol = nextSymbol;
    status.payment_display_currency_exchange_rate = Number(nextRate) || 1;
    if (usdExchangeRate !== undefined) {
      status.usd_exchange_rate = usdExchangeRate;
    }
    localStorage.setItem('status', JSON.stringify(status));
  } catch (error) {
    console.error('failed to sync payment display currency to localStorage', error);
  }
}

export default function SettingsGeneralPayment(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(DEFAULT_PAYMENT_DISPLAY_INPUTS);

  useEffect(() => {
    if (!props.options) {
      return;
    }

    const currentInputs = {
      ServerAddress: props.options.ServerAddress || '',
      [PAYMENT_DISPLAY_TYPE_FIELD]:
        props.options[PAYMENT_DISPLAY_TYPE_FIELD] || 'FOLLOW_QUOTA',
      [PAYMENT_DISPLAY_SYMBOL_FIELD]:
        props.options[PAYMENT_DISPLAY_SYMBOL_FIELD] || '\u00A4',
      [PAYMENT_DISPLAY_RATE_FIELD]:
        props.options[PAYMENT_DISPLAY_RATE_FIELD] !== undefined &&
        props.options[PAYMENT_DISPLAY_RATE_FIELD] !== ''
          ? String(props.options[PAYMENT_DISPLAY_RATE_FIELD])
          : '1',
    };

    console.log('[payment-debug][SettingsGeneralPayment] props.options -> currentInputs', {
      paymentDisplayType: props.options[PAYMENT_DISPLAY_TYPE_FIELD],
      paymentDisplaySymbol: props.options[PAYMENT_DISPLAY_SYMBOL_FIELD],
      paymentDisplayRate: props.options[PAYMENT_DISPLAY_RATE_FIELD],
      currentInputs,
    });

    setInputs(currentInputs);
  }, [props.options]);

  const handleFormChange = (values) => {
    console.log('[payment-debug][SettingsGeneralPayment] handleFormChange', values);
    setInputs((prev) => ({ ...prev, ...values }));
  };

  const paymentDisplayRatePreview = useMemo(() => {
    const type = inputs[PAYMENT_DISPLAY_TYPE_FIELD];
    if (type === 'FOLLOW_QUOTA' || type === 'USD') {
      return '1';
    }
    if (type === 'CNY') {
      return String(props.options?.USDExchangeRate || '');
    }
    return String(inputs[PAYMENT_DISPLAY_RATE_FIELD] || '');
  }, [inputs, props.options]);

  const submitSettings = async () => {
    const displayType = inputs[PAYMENT_DISPLAY_TYPE_FIELD] || 'FOLLOW_QUOTA';
    const displaySymbol =
      displayType === 'CUSTOM'
        ? inputs[PAYMENT_DISPLAY_SYMBOL_FIELD] || '\u00A4'
        : '\u00A4';
    const displayRate =
      displayType === 'CUSTOM'
        ? String(inputs[PAYMENT_DISPLAY_RATE_FIELD] || '1')
        : '1';

    if (displayType === 'CUSTOM') {
      const numericRate = Number(displayRate);
      if (!Number.isFinite(numericRate) || numericRate <= 0) {
        showError(t('自定义支付汇率必须大于 0'));
        return;
      }
    }

    setLoading(true);
    try {
      console.log('[payment-debug][SettingsGeneralPayment] submitSettings before PUT', {
        rawInputs: inputs,
        displayType,
        displaySymbol,
        displayRate,
      });
      const requestQueue = [
        API.put('/api/option/', {
          key: 'ServerAddress',
          value: removeTrailingSlash(inputs.ServerAddress || ''),
        }),
        API.put('/api/option/', {
          key: PAYMENT_DISPLAY_TYPE_FIELD,
          value: displayType,
        }),
        API.put('/api/option/', {
          key: PAYMENT_DISPLAY_SYMBOL_FIELD,
          value: displaySymbol,
        }),
        API.put('/api/option/', {
          key: PAYMENT_DISPLAY_RATE_FIELD,
          value: displayRate,
        }),
      ];
      const results = await Promise.all(requestQueue);
      console.log(
        '[payment-debug][SettingsGeneralPayment] submitSettings PUT results',
        results.map((res) => res.data),
      );
      const failed = results.find((res) => !res.data?.success);
      if (failed) {
        showError(failed.data?.message || t('更新失败'));
        return;
      }

      syncPaymentDisplayToLocalStorage(
        {
          [PAYMENT_DISPLAY_TYPE_FIELD]: displayType,
          [PAYMENT_DISPLAY_SYMBOL_FIELD]: displaySymbol,
          [PAYMENT_DISPLAY_RATE_FIELD]: displayRate,
        },
        props.options?.USDExchangeRate,
      );

      showSuccess(t('更新成功'));
      props.refresh && props.refresh();
    } catch (error) {
      showError(t('更新失败'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Spin spinning={loading}>
      <Form>
        <Form.Section text={t('通用支付设置')}>
          <Row gutter={16}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='ServerAddress'
                value={inputs.ServerAddress}
                onChange={(value) => handleFormChange({ ServerAddress: value })}
                label={t('服务器地址')}
                placeholder='https://yourdomain.com'
                extraText={t(
                  '该服务器地址将影响支付回调地址以及默认首页展示的地址，请确保正确配置',
                )}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Slot label={t('支付金额显示货币及汇率')}>
                <InputGroup style={{ width: '100%' }}>
                  <Input
                    prefix='1 USD = '
                    style={{ width: '50%' }}
                    value={paymentDisplayRatePreview}
                    disabled={
                      inputs[PAYMENT_DISPLAY_TYPE_FIELD] === 'FOLLOW_QUOTA' ||
                      inputs[PAYMENT_DISPLAY_TYPE_FIELD] === 'USD' ||
                      inputs[PAYMENT_DISPLAY_TYPE_FIELD] === 'CNY'
                    }
                    onChange={(value) =>
                      handleFormChange({
                        [PAYMENT_DISPLAY_RATE_FIELD]: value,
                      })
                    }
                  />
                  <Select
                    style={{ width: '50%' }}
                    value={inputs[PAYMENT_DISPLAY_TYPE_FIELD]}
                    onChange={(value) =>
                      handleFormChange({
                        [PAYMENT_DISPLAY_TYPE_FIELD]: value,
                      })
                    }
                  >
                    <Select.Option value='FOLLOW_QUOTA'>
                      {t('跟随额度显示')}
                    </Select.Option>
                    <Select.Option value='USD'>USD ($)</Select.Option>
                    <Select.Option value='CNY'>CNY (¥)</Select.Option>
                    <Select.Option value='CUSTOM'>
                      {t('自定义货币')}
                    </Select.Option>
                  </Select>
                </InputGroup>
              </Form.Slot>
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field={PAYMENT_DISPLAY_SYMBOL_FIELD}
                value={inputs[PAYMENT_DISPLAY_SYMBOL_FIELD]}
                onChange={(value) =>
                  handleFormChange({
                    [PAYMENT_DISPLAY_SYMBOL_FIELD]: value,
                  })
                }
                label={t('支付自定义货币符号')}
                placeholder={t('例如 ¥, €, £, Rp, HK$')}
                disabled={inputs[PAYMENT_DISPLAY_TYPE_FIELD] !== 'CUSTOM'}
              />
            </Col>
          </Row>
          <Row>
            <Button onClick={submitSettings}>{t('保存支付设置')}</Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
