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
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const defaultInvoiceInputs = {
  'invoice_setting.min_amount': 300,
  'invoice_setting.online_time': '2026-08-01 00:00:00',
  'invoice_setting.r2_enabled': false,
  'invoice_setting.r2_account_id': '',
  'invoice_setting.r2_bucket': '',
  'invoice_setting.r2_endpoint': '',
  'invoice_setting.r2_access_key_id': '',
  'invoice_setting.r2_secret': '',
  'invoice_setting.r2_object_prefix': 'invoices/',
  'invoice_setting.r2_url_expire_hours': 24,
};

export default function SettingsInvoice(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [testingR2, setTestingR2] = useState(false);
  const [inputs, setInputs] = useState(defaultInvoiceInputs);
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const buildRequest = (item) => {
      // 敏感信息不回显，未填写新值时不提交
      if (item.key === 'invoice_setting.r2_secret' && !inputs[item.key]) {
        return null;
      }
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = String(inputs[item.key] ?? '');
      }
      return { key: item.key, value };
    };
    // 启用开关放最后提交，避免后端校验先于其他字段保存完成
    const enabledKey = 'invoice_setting.r2_enabled';
    const otherPayloads = updateArray
      .filter((item) => item.key !== enabledKey)
      .map(buildRequest)
      .filter(Boolean);
    const enabledPayloads = updateArray
      .filter((item) => item.key === enabledKey)
      .map(buildRequest)
      .filter(Boolean);

    setLoading(true);
    Promise.all(otherPayloads.map((p) => API.put('/api/option/', p)))
      .then(async (res) => {
        if (otherPayloads.length === 1) {
          if (res.includes(undefined)) return;
        } else if (otherPayloads.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        for (const p of enabledPayloads) {
          const r = await API.put('/api/option/', p);
          if (r?.data && r.data.success === false) {
            return showError(r.data.message || t('保存失败，请重试'));
          }
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  async function onTestR2() {
    setTestingR2(true);
    try {
      const res = await API.post('/api/invoice/r2/test', {
        account_id: inputs['invoice_setting.r2_account_id'],
        bucket: inputs['invoice_setting.r2_bucket'],
        endpoint: inputs['invoice_setting.r2_endpoint'],
        access_key_id: inputs['invoice_setting.r2_access_key_id'],
        // 密钥不回显：留空时后端自动使用已保存的值
        secret: inputs['invoice_setting.r2_secret'],
        object_prefix: inputs['invoice_setting.r2_object_prefix'],
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('R2 连接测试成功，配置可用'));
      } else {
        showError(message || t('R2 连接测试失败'));
      }
    } catch (e) {
      showError(t('R2 连接测试失败'));
    } finally {
      setTestingR2(false);
    }
  }

  useEffect(() => {
    const currentInputs = { ...defaultInvoiceInputs };
    for (let key in props.options) {
      if (Object.keys(defaultInvoiceInputs).includes(key)) {
        if (typeof defaultInvoiceInputs[key] === 'boolean') {
          currentInputs[key] =
            props.options[key] === 'true' || props.options[key] === true;
        } else if (key === 'invoice_setting.min_amount') {
          const parsed = parseFloat(props.options[key]);
          currentInputs[key] = isNaN(parsed)
            ? defaultInvoiceInputs[key]
            : parsed;
        } else if (typeof defaultInvoiceInputs[key] === 'number') {
          currentInputs[key] =
            parseInt(props.options[key]) || defaultInvoiceInputs[key];
        } else {
          currentInputs[key] = props.options[key];
        }
      }
    }
    // 敏感信息不回显，保持为空
    currentInputs['invoice_setting.r2_secret'] = '';
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section text={t('发票设置')}>
          <Banner
            type='info'
            description={t(
              '用户可对支付成功的 EPay 订单或实付金额大于 0 的兑换码订单自助申请开票；仅上线时间之后完成、且单笔实付金额达到阈值的订单可申请。',
            )}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field={'invoice_setting.min_amount'}
                label={t('开票金额阈值')}
                min={0}
                precision={2}
                extraText={t('单笔充值金额达到该值才可申请开票')}
                placeholder={'300'}
                onChange={handleFieldChange('invoice_setting.min_amount')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Input
                field={'invoice_setting.online_time'}
                label={t('开票上线时间')}
                placeholder={'2026-08-01 00:00:00'}
                extraText={t('仅此时间之后完成的订单才能申请开票，格式：2006-01-02 15:04:05')}
                onChange={handleFieldChange('invoice_setting.online_time')}
                showClear
              />
            </Col>
          </Row>
        </Form.Section>

        <Form.Section text={t('发票文件 R2 存储')}>
          <Banner
            type='info'
            description={t(
              '开启后，管理员开具的发票文件将上传到 Cloudflare R2，用户通过预签名 URL 下载，系统邮件中也会附带下载链接。',
            )}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={16}>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Switch
                field={'invoice_setting.r2_enabled'}
                label={t('启用发票 R2 存储')}
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                onChange={handleFieldChange('invoice_setting.r2_enabled')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Input
                field={'invoice_setting.r2_account_id'}
                label={t('R2 Account ID')}
                placeholder={t('Cloudflare Account ID')}
                onChange={handleFieldChange('invoice_setting.r2_account_id')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Input
                field={'invoice_setting.r2_bucket'}
                label={t('R2 Bucket')}
                placeholder={t('存储桶名称')}
                onChange={handleFieldChange('invoice_setting.r2_bucket')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Input
                field={'invoice_setting.r2_endpoint'}
                label={t('R2 Endpoint')}
                placeholder={'https://<account_id>.r2.cloudflarestorage.com'}
                extraText={t('留空时根据 Account ID 自动生成')}
                onChange={handleFieldChange('invoice_setting.r2_endpoint')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Input
                field={'invoice_setting.r2_access_key_id'}
                label={t('R2 Access Key ID')}
                placeholder={t('帐户 API 令牌中的访问密钥 ID')}
                onChange={handleFieldChange('invoice_setting.r2_access_key_id')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Input
                field={'invoice_setting.r2_secret'}
                label={t('R2 Secret Access Key')}
                mode='password'
                placeholder={t('敏感信息不会发送到前端显示')}
                onChange={handleFieldChange('invoice_setting.r2_secret')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.Input
                field={'invoice_setting.r2_object_prefix'}
                label={t('R2 对象前缀')}
                placeholder='invoices/'
                extraText={t('建议和 R2 生命周期规则前缀保持一致')}
                onChange={handleFieldChange('invoice_setting.r2_object_prefix')}
              />
            </Col>
            <Col xs={24} sm={12} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field={'invoice_setting.r2_url_expire_hours'}
                label={t('URL 有效期（小时）')}
                min={1}
                max={168}
                extraText={t('R2 presigned URL 最长建议不超过 7 天')}
                onChange={handleFieldChange(
                  'invoice_setting.r2_url_expire_hours',
                )}
              />
            </Col>
          </Row>
          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存发票设置')}
            </Button>
            <Button
              size='default'
              type='secondary'
              style={{ marginLeft: 8 }}
              loading={testingR2}
              onClick={onTestR2}
            >
              {t('测试 R2 连接')}
            </Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
