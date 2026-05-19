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

import React, { useEffect, useState, useRef } from 'react';
import { Banner, Button, Col, Form, Row, Spin, Tag } from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const defaultDrawingInputs = {
  DrawingEnabled: false,
  MjNotifyEnabled: false,
  MjAccountFilterEnabled: false,
  MjForwardUrlEnabled: false,
  MjModeClearEnabled: false,
  MjActionCheckSuccessEnabled: false,
  'image_storage_setting.r2_enabled': false,
  'image_storage_setting.r2_account_id': '',
  'image_storage_setting.r2_bucket': '',
  'image_storage_setting.r2_endpoint': '',
  'image_storage_setting.r2_access_key_id': '',
  'image_storage_setting.r2_secret': '',
  'image_storage_setting.r2_object_prefix': 'generated-images/',
  'image_storage_setting.r2_url_expire_hours': 24,
};

export default function SettingsDrawing(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState(defaultDrawingInputs);
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      if (item.key === 'image_storage_setting.r2_secret' && !inputs[item.key]) {
        return null;
      }
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = String(inputs[item.key] ?? '');
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    }).filter(Boolean);
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
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

  useEffect(() => {
    const currentInputs = { ...defaultDrawingInputs };
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        if (typeof defaultDrawingInputs[key] === 'boolean') {
          currentInputs[key] =
            props.options[key] === 'true' || props.options[key] === true;
        } else if (typeof defaultDrawingInputs[key] === 'number') {
          currentInputs[key] =
            parseInt(props.options[key]) || defaultDrawingInputs[key];
        } else {
          currentInputs[key] = props.options[key];
        }
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
    localStorage.setItem('mj_notify_enabled', String(inputs.MjNotifyEnabled));
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('绘图设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'DrawingEnabled'}
                  label={t('启用绘图功能')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) => {
                    setInputs({
                      ...inputs,
                      DrawingEnabled: value,
                    });
                  }}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'MjNotifyEnabled'}
                  label={t('允许回调（会泄露服务器 IP 地址）')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      MjNotifyEnabled: value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'MjAccountFilterEnabled'}
                  label={t('允许 AccountFilter 参数')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      MjAccountFilterEnabled: value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'MjForwardUrlEnabled'}
                  label={t('开启之后将上游地址替换为服务器地址')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      MjForwardUrlEnabled: value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'MjModeClearEnabled'}
                  label={
                    <>
                      {t('开启之后会清除用户提示词中的')} <Tag>--fast</Tag> 、
                      <Tag>--relax</Tag> {t('以及')} <Tag>--turbo</Tag>{' '}
                      {t('参数')}
                    </>
                  }
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      MjModeClearEnabled: value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'MjActionCheckSuccessEnabled'}
                  label={t('检测必须等待绘图成功才能进行放大等操作')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      MjActionCheckSuccessEnabled: value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存绘图设置')}
              </Button>
            </Row>
          </Form.Section>

          <Form.Section text={t('图片生成 R2 存储')}>
            <Banner
              type='info'
              description={t(
                '开启后，图片生成接口会将上游返回的 b64_json 图片上传到 Cloudflare R2 并返回临时 URL；上游已经返回 URL 时会直接透传。',
              )}
              style={{ marginBottom: 16 }}
            />
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'image_storage_setting.r2_enabled'}
                  label={t('启用 R2 图片存储')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_enabled': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'image_storage_setting.r2_account_id'}
                  label={t('R2 Account ID')}
                  placeholder={t('Cloudflare Account ID')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_account_id': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'image_storage_setting.r2_bucket'}
                  label={t('R2 Bucket')}
                  placeholder={t('存储桶名称')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_bucket': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'image_storage_setting.r2_endpoint'}
                  label={t('R2 Endpoint')}
                  placeholder={'https://<account_id>.r2.cloudflarestorage.com'}
                  extraText={t('留空时根据 Account ID 自动生成')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_endpoint': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'image_storage_setting.r2_access_key_id'}
                  label={t('R2 Access Key ID')}
                  placeholder={t('帐户 API 令牌中的访问密钥 ID')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_access_key_id': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'image_storage_setting.r2_secret'}
                  label={t('R2 Secret Access Key')}
                  mode='password'
                  placeholder={t('敏感信息不会发送到前端显示')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_secret': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'image_storage_setting.r2_object_prefix'}
                  label={t('R2 对象前缀')}
                  placeholder='generated-images/'
                  extraText={t('建议和 R2 生命周期规则前缀保持一致')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_object_prefix': value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'image_storage_setting.r2_url_expire_hours'}
                  label={t('URL 有效期（小时）')}
                  min={1}
                  max={168}
                  extraText={t('R2 presigned URL 最长建议不超过 7 天')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'image_storage_setting.r2_url_expire_hours': value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存绘图设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
