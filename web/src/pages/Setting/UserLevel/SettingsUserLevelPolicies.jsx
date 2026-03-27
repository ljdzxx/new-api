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
import { Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import {
  API,
  compareObjects,
  showError,
  showSuccess,
  showWarning,
  verifyJSON,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

const defaultPolicies = [
  {
    id: 1,
    level: 'Tier 1',
    icon: '/t1.png',
    discount: '0',
    channel: [],
    rate: 50,
    recharge: '0',
    group_day_limit: '100',
  },
  {
    id: 2,
    level: 'Tier 2',
    icon: '/t2.png',
    discount: '0.1',
    channel: [],
    rate: 100,
    recharge: '500',
    group_day_limit: '0',
  },
  {
    id: 3,
    level: 'Tier 3',
    icon: '/t3.png',
    discount: '0.2',
    channel: [],
    rate: 500,
    recharge: '2000',
    group_day_limit: '0',
  },
  {
    id: 4,
    level: 'Tier 4',
    icon: '/t4.png',
    discount: '0.4',
    channel: [],
    rate: 0,
    recharge: '10000',
    group_day_limit: '0',
  },
];

export default function SettingsUserLevelPolicies(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    UserLevelPolicies: '[]',
  });
  const [inputsRow, setInputsRow] = useState(inputs);
  const refForm = useRef();

  function fillTemplate() {
    const value = JSON.stringify(defaultPolicies, null, 2);
    setInputs((prev) => ({ ...prev, UserLevelPolicies: value }));
    refForm.current?.setValue('UserLevelPolicies', value);
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) {
      return showWarning(t('你似乎并没有修改什么'));
    }

    setLoading(true);
    Promise.all(
      updateArray.map((item) =>
        API.put('/api/option/', {
          key: item.key,
          value: inputs[item.key],
        }),
      ),
    )
      .then((res) => {
        for (let i = 0; i < res.length; i++) {
          if (!res[i]?.data?.success) {
            return showError(res[i]?.data?.message || t('保存失败，请重试'));
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

  useEffect(() => {
    const currentInputs = {};
    for (const key in inputs) {
      if (Object.prototype.hasOwnProperty.call(props.options, key)) {
        currentInputs[key] = props.options[key];
      }
    }
    setInputs(currentInputs);
    setInputsRow(structuredClone(currentInputs));
    refForm.current?.setValues(currentInputs);
  }, [props.options]);

  return (
    <Spin spinning={loading}>
      <Form
        values={inputs}
        getFormApi={(formAPI) => (refForm.current = formAPI)}
        style={{ marginBottom: 15 }}
      >
        <Form.Section
          text={t('等级/限制')}
          extraText={t('说明：level 对应用户分组（user.group）')}
        >
          <Row>
            <Col xs={24} sm={24} md={18} lg={16}>
              <Form.TextArea
                field='UserLevelPolicies'
                label={t('等级策略 JSON')}
                autosize={{ minRows: 8, maxRows: 24 }}
                trigger='blur'
                stopValidateWithError
                rules={[
                  {
                    validator: (rule, value) => verifyJSON(value),
                    message: t('不是合法的 JSON 字符串'),
                  },
                ]}
                placeholder={JSON.stringify(defaultPolicies, null, 2)}
                onChange={(value) =>
                  setInputs((prev) => ({ ...prev, UserLevelPolicies: value }))
                }
                extraText={
                  <div>
                    <p>{t('字段说明：')}</p>
                    <ul>
                      <li>{t('level：用户等级（匹配用户分组）')}</li>
                      <li>{t('id：等级主键ID（用户关联该ID）')}</li>
                      <li>{t('recharge：升至该等级所需累计充值金额')}</li>
                      <li>{t('discount：折扣，0.1 表示 10% 折扣')}</li>
                      <li>{t('icon：等级图标路径，如 t1.png')}</li>
                      <li>{t('channel：可用渠道数组，[] 表示不限渠道')}</li>
                      <li>{t('rate：每分钟请求次数，0 表示不限')}</li>
                      <li>{t('group_day_limit：该等级组日消耗金额上限，0 表示不限')}</li>
                    </ul>
                  </div>
                }
              />
            </Col>
          </Row>
          <Row>
            <Button size='default' type='tertiary' onClick={fillTemplate}>
              {t('填充示例')}
            </Button>
            <Button size='default' style={{ marginLeft: 8 }} onClick={onSubmit}>
              {t('保存用户等级配置')}
            </Button>
          </Row>
        </Form.Section>
      </Form>
    </Spin>
  );
}
