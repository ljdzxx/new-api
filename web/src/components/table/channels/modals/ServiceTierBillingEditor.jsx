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
import React from 'react';
import {
  Button,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  SERVICE_TIER_TAG_COLORS,
  SERVICE_TIER_TAG_TYPES,
} from '../../../../helpers/serviceTierBilling';

export default function ServiceTierBillingEditor({ value, onChange }) {
  const { t } = useTranslation();
  const config = value || { enabled: false, rules: [] };
  const rules = config.rules || [];
  const update = (index, key, next) =>
    onChange({
      ...config,
      rules: rules.map((rule, i) =>
        i === index ? { ...rule, [key]: next } : rule,
      ),
    });
  return (
    <div className='mt-4 mb-4'>
      <Space>
        <Typography.Text strong>{t('固定服务档位计费')}</Typography.Text>
        <Switch
          checked={config.enabled === true}
          onChange={(enabled) => onChange({ ...config, enabled })}
          aria-label={t('固定服务档位计费')}
        />
      </Space>
      <div className='text-xs text-tertiary mt-2 mb-2'>
        {t(
          '按映射前模型匹配，精确规则优先，* 匹配任意模型；仅调整模型费用，不调整独立工具费用，不修改上游请求参数。',
        )}
      </div>
      {config.enabled && (
        <>
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', minWidth: 720 }}>
              <thead>
                <tr>
                  {[
                    t('模型'),
                    'service_tier',
                    t('标签颜色'),
                    t('标签样式'),
                    t('计费倍率'),
                    t('预览'),
                    t('操作'),
                  ].map((label) => (
                    <th key={label} style={{ textAlign: 'left', padding: 4 }}>
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rules.map((rule, index) => (
                  <tr key={index}>
                    <td style={{ padding: 4 }}>
                      <Input
                        aria-label={t('模型')}
                        value={rule.model}
                        placeholder={t('例如：{{example}}', {
                          example: 'gpt-6-astra / *',
                        })}
                        onChange={(v) => update(index, 'model', v)}
                      />
                    </td>
                    <td style={{ padding: 4 }}>
                      <Input
                        aria-label='service_tier'
                        value={rule.tier}
                        maxLength={64}
                        placeholder={t('例如：{{example}}', {
                          example: 'Ultrafast',
                        })}
                        onChange={(v) => update(index, 'tier', v)}
                      />
                    </td>
                    <td style={{ padding: 4 }}>
                      <Select
                        aria-label={t('标签颜色')}
                        value={rule.tag_color}
                        style={{ width: 130 }}
                        onChange={(v) => update(index, 'tag_color', v)}
                        optionList={SERVICE_TIER_TAG_COLORS.map((color) => ({
                          value: color,
                          label: <Tag color={color}>{color}</Tag>,
                        }))}
                      />
                    </td>
                    <td style={{ padding: 4 }}>
                      <Select
                        aria-label={t('标签样式')}
                        value={rule.tag_type}
                        style={{ width: 90 }}
                        onChange={(v) => update(index, 'tag_type', v)}
                        optionList={SERVICE_TIER_TAG_TYPES.map((type) => ({
                          value: type,
                          label: type,
                        }))}
                      />
                    </td>
                    <td style={{ padding: 4 }}>
                      <InputNumber
                        aria-label={t('计费倍率')}
                        value={rule.multiplier}
                        min={0.000001}
                        max={1000}
                        style={{ width: 95 }}
                        onChange={(v) => update(index, 'multiplier', v)}
                      />
                    </td>
                    <td style={{ padding: 4 }}>
                      <Tag color={rule.tag_color} type={rule.tag_type}>
                        {rule.tier || 'service_tier'}
                      </Tag>
                    </td>
                    <td style={{ padding: 4 }}>
                      <Button
                        type='danger'
                        theme='borderless'
                        onClick={() =>
                          onChange({
                            ...config,
                            rules: rules.filter((_, i) => i !== index),
                          })
                        }
                      >
                        {t('删除')}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Button
            className='mt-2'
            onClick={() =>
              onChange({
                ...config,
                rules: [
                  ...rules,
                  {
                    model: '',
                    tier: '',
                    tag_color: 'blue',
                    tag_type: 'light',
                    multiplier: 1,
                  },
                ],
              })
            }
          >
            {t('添加规则')}
          </Button>
        </>
      )}
    </div>
  );
}
