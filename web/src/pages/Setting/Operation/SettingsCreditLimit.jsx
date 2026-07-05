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
import { Button, Col, Form, Row, Select, Spin, Tag } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';

const DEFAULT_INVITE_RISK_WEIGHTS = {
  ip: 25,
  fingerprint: 30,
  canvas: 10,
  webgl: 10,
  audio: 6,
  fonts: 6,
  ua: 5,
  locale: 4,
  screen: 3,
  hardware: 1,
};

const parseInviteRiskWeights = (raw) => {
  if (!raw) return { ...DEFAULT_INVITE_RISK_WEIGHTS };
  try {
    return {
      ...DEFAULT_INVITE_RISK_WEIGHTS,
      ...JSON.parse(raw),
    };
  } catch (err) {
    return { ...DEFAULT_INVITE_RISK_WEIGHTS };
  }
};

const stringifyInviteRiskWeights = (weights) => JSON.stringify(weights);

export default function SettingsCreditLimit(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    QuotaForNewUser: '',
    PreConsumedQuota: '',
    QuotaForInviter: '',
    QuotaForInvitee: '',
    InviteeSubscriptionPlanId: '0',
    InviteRewardEmailOnly: false,
    InviteRewardEmailRegex: '',
    InviteRiskControlEnabled: false,
    InviteRiskThreshold: 60,
    InviteRiskDailyLimit: 0,
    InviteRiskScoreWeights: stringifyInviteRiskWeights(
      DEFAULT_INVITE_RISK_WEIGHTS,
    ),
    'quota_setting.enable_free_model_pre_consume': true,
  });
  const [subscriptionPlans, setSubscriptionPlans] = useState([]);
  const [plansLoading, setPlansLoading] = useState(false);
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);
  const inviteRiskWeights = parseInviteRiskWeights(
    inputs.InviteRiskScoreWeights,
  );
  const inviteRiskWeightTotal = Object.values(inviteRiskWeights).reduce(
    (sum, value) => sum + Number(value || 0),
    0,
  );

  const updateInviteRiskWeight = (key, value) => {
    const next = {
      ...inviteRiskWeights,
      [key]: Number(value || 0),
    };
    setInputs({
      ...inputs,
      InviteRiskScoreWeights: stringifyInviteRiskWeights(next),
    });
  };

  function onSubmit() {
    if (inviteRiskWeightTotal !== 100) {
      return showError(t('邀请奖励风控权重总分必须等于 100'));
    }
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = inputs[item.key];
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
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
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    if (currentInputs.InviteeSubscriptionPlanId === undefined) {
      currentInputs.InviteeSubscriptionPlanId = '0';
    }
    if (currentInputs.InviteRewardEmailOnly === undefined) {
      currentInputs.InviteRewardEmailOnly = false;
    }
    if (currentInputs.InviteRewardEmailRegex === undefined) {
      currentInputs.InviteRewardEmailRegex = '';
    }
    if (currentInputs.InviteRiskControlEnabled === undefined) {
      currentInputs.InviteRiskControlEnabled = false;
    }
    if (currentInputs.InviteRiskThreshold === undefined) {
      currentInputs.InviteRiskThreshold = 60;
    }
    if (currentInputs.InviteRiskDailyLimit === undefined) {
      currentInputs.InviteRiskDailyLimit = 0;
    }
    if (currentInputs.InviteRiskScoreWeights === undefined) {
      currentInputs.InviteRiskScoreWeights = stringifyInviteRiskWeights(
        DEFAULT_INVITE_RISK_WEIGHTS,
      );
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  useEffect(() => {
    setPlansLoading(true);
    API.get('/api/subscription/admin/plans')
      .then((res) => {
        if (res.data?.success) {
          setSubscriptionPlans(res.data?.data || []);
        } else {
          setSubscriptionPlans([]);
        }
      })
      .catch(() => setSubscriptionPlans([]))
      .finally(() => setPlansLoading(false));
  }, []);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('额度设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('新用户初始额度')}
                  field={'QuotaForNewUser'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  placeholder={''}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      QuotaForNewUser: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('请求预扣费额度')}
                  field={'PreConsumedQuota'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={t('请求结束后多退少补')}
                  placeholder={''}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      PreConsumedQuota: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('邀请新用户奖励额度')}
                  field={'QuotaForInviter'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={''}
                  placeholder={t('例如：2000')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      QuotaForInviter: String(value),
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  label={t('新用户使用邀请码奖励额度')}
                  field={'QuotaForInvitee'}
                  step={1}
                  min={0}
                  suffix={'Token'}
                  extraText={''}
                  placeholder={t('例如：1000')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      QuotaForInvitee: String(value),
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Select
                  label={t('新用户使用邀请码奖励订阅套餐')}
                  field={'InviteeSubscriptionPlanId'}
                  loading={plansLoading}
                  placeholder={t('未选择')}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      InviteeSubscriptionPlanId: String(value || '0'),
                    })
                  }
                >
                  <Select.Option value='0'>{t('未选择')}</Select.Option>
                  {(subscriptionPlans || []).map((item) => {
                    const plan = item?.plan || {};
                    return (
                      <Select.Option key={plan.id} value={String(plan.id)}>
                        {plan.enabled === false
                          ? `${plan.title || `#${plan.id}`} (${t('已禁用')})`
                          : plan.title || `#${plan.id}`}
                      </Select.Option>
                    );
                  })}
                </Form.Select>
              </Col>
            </Row>
            <Row>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  label={t('仅邮箱注册有奖')}
                  field={'InviteRewardEmailOnly'}
                  extraText={t(
                    '开启后，仅邮箱注册用户可触发邀请人奖励和被邀请人奖励',
                  )}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      InviteRewardEmailOnly: value,
                    })
                  }
                />
              </Col>
              <Col xs={24} sm={12} md={16} lg={16} xl={16}>
                <Form.Input
                  label={t('邮箱限定正则')}
                  field={'InviteRewardEmailRegex'}
                  placeholder={t('例如：^[^@]+@example\\.com$')}
                  extraText={t(
                    '为空则不限制；填写后，仅邮箱匹配的新用户可触发邀请奖励',
                  )}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      InviteRewardEmailRegex: value,
                    })
                  }
                />
              </Col>
            </Row>
            <Row>
              <Col>
                <Form.Switch
                  label={t('对免费模型启用预消耗')}
                  field={'quota_setting.enable_free_model_pre_consume'}
                  extraText={t(
                    '开启后，对免费模型（倍率为0，或者价格为0）的模型也会预消耗额度',
                  )}
                  onChange={(value) =>
                    setInputs({
                      ...inputs,
                      'quota_setting.enable_free_model_pre_consume': value,
                    })
                  }
                />
              </Col>
            </Row>
            <Form.Section text={t('邀请奖励风控')}>
              <Row gutter={16}>
                <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                  <Form.Switch
                    label={t('启用邀请奖励风控')}
                    field={'InviteRiskControlEnabled'}
                    extraText={t(
                      '开启后，受邀注册会根据 IP 与浏览器指纹评分决定是否发放邀请奖励',
                    )}
                    onChange={(value) =>
                      setInputs({
                        ...inputs,
                        InviteRiskControlEnabled: value,
                      })
                    }
                  />
                </Col>
                <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                  <Form.InputNumber
                    label={t('风险拦截分数')}
                    field={'InviteRiskThreshold'}
                    min={0}
                    max={100}
                    step={1}
                    extraText={t('风控评分大于等于该值时不发放邀请奖励')}
                    onChange={(value) =>
                      setInputs({
                        ...inputs,
                        InviteRiskThreshold: String(value),
                      })
                    }
                  />
                </Col>
                <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                  <Form.InputNumber
                    label={t('每日有效邀请上限')}
                    field={'InviteRiskDailyLimit'}
                    min={0}
                    step={1}
                    extraText={t(
                      '0 表示不限制；超过上限后当天不再发放邀请奖励',
                    )}
                    onChange={(value) =>
                      setInputs({
                        ...inputs,
                        InviteRiskDailyLimit: String(value),
                      })
                    }
                  />
                </Col>
              </Row>
              <Row>
                <Col span={24}>
                  <Tag
                    color={inviteRiskWeightTotal === 100 ? 'green' : 'red'}
                    shape='circle'
                  >
                    {t('权重总分')}: {inviteRiskWeightTotal}/100
                  </Tag>
                </Col>
              </Row>
              <Row gutter={16}>
                {[
                  {
                    key: 'ip',
                    label: '精确 IP hash 相同',
                    placeholder: 25,
                    extraText: '你明确要求精确 IP，不用 IP 段',
                  },
                  {
                    key: 'fingerprint',
                    label: '浏览器稳定指纹整体 hash 相同',
                    placeholder: 30,
                    extraText:
                      'Canvas/WebGL/Audio/字体/硬件等组合后的 stable hash',
                  },
                  {
                    key: 'canvas',
                    label: 'Canvas hash 相同',
                    placeholder: 10,
                    extraText: '识别度较高',
                  },
                  {
                    key: 'webgl',
                    label: 'WebGL renderer/vendor hash 相同',
                    placeholder: 10,
                    extraText: 'GPU 环境强信号',
                  },
                  {
                    key: 'audio',
                    label: 'AudioContext hash 相同',
                    placeholder: 6,
                    extraText: '中等强度',
                  },
                  {
                    key: 'fonts',
                    label: '字体集合 hash 相同',
                    placeholder: 6,
                    extraText: '有用，但兼容性波动较大',
                  },
                  {
                    key: 'ua',
                    label: 'User-Agent / Client Hints hash 相同',
                    placeholder: 5,
                    extraText: '容易相同，也容易伪造，低权重',
                  },
                  {
                    key: 'locale',
                    label: '时区 + 语言 hash 相同',
                    placeholder: 4,
                    extraText: '辅助信号',
                  },
                  {
                    key: 'screen',
                    label: '屏幕/DPR/颜色深度 hash 相同',
                    placeholder: 3,
                    extraText: '辅助信号',
                  },
                  {
                    key: 'hardware',
                    label: 'CPU/内存/平台能力 hash 相同',
                    placeholder: 1,
                    extraText: '辅助信号，低权重',
                  },
                ].map(({ key, label, placeholder, extraText }) => (
                  <Col key={key} xs={12} sm={12} md={8} lg={6} xl={6}>
                    <Form.InputNumber
                      label={t(label)}
                      field={`InviteRiskScoreWeights.${key}`}
                      min={0}
                      max={100}
                      step={1}
                      placeholder={String(placeholder)}
                      extraText={t(extraText)}
                      value={inviteRiskWeights[key]}
                      onChange={(value) => updateInviteRiskWeight(key, value)}
                    />
                  </Col>
                ))}
              </Row>
            </Form.Section>

            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存额度设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
