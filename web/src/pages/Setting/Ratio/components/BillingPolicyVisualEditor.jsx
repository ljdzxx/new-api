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
import { useTranslation } from 'react-i18next';
import {
  Button,
  Banner,
  Card,
  Checkbox,
  Input,
  Modal,
  Select,
  Space,
  TabPane,
  Tabs,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';

const { Text } = Typography;
const priceFields = [
  ['input', '输入'],
  ['output', '输出'],
  ['cache_read', '缓存读取'],
  ['cache_write', '缓存写入'],
  ['cache_write_5m', 'Anthropic 缓存写入 5m'],
  ['cache_write_1h', 'Anthropic 缓存写入 1h'],
  ['image_input', '图片输入'],
  ['audio_input', '音频输入'],
  ['audio_output', '音频输出'],
];
const toolPriceFields = [
  ['web_search.standard', 'Web Search 标准模型', 'per_thousand_calls', '10'],
  ['web_search.premium', 'Web Search 高价模型', 'per_thousand_calls', '25'],
  ['claude_web_search', 'Claude Web Search', 'per_thousand_calls', '10'],
  ['file_search', 'File Search', 'per_thousand_calls', '2.5'],
  [
    'image_generation.low.1024x1024',
    '图片生成 low 1024x1024',
    'per_request',
    '0.011',
  ],
  [
    'image_generation.low.1024x1536',
    '图片生成 low 1024x1536',
    'per_request',
    '0.016',
  ],
  [
    'image_generation.low.1536x1024',
    '图片生成 low 1536x1024',
    'per_request',
    '0.016',
  ],
  [
    'image_generation.medium.1024x1024',
    '图片生成 medium 1024x1024',
    'per_request',
    '0.042',
  ],
  [
    'image_generation.medium.1024x1536',
    '图片生成 medium 1024x1536',
    'per_request',
    '0.063',
  ],
  [
    'image_generation.medium.1536x1024',
    '图片生成 medium 1536x1024',
    'per_request',
    '0.063',
  ],
  [
    'image_generation.high.1024x1024',
    '图片生成 high 1024x1024',
    'per_request',
    '0.167',
  ],
  [
    'image_generation.high.1024x1536',
    '图片生成 high 1024x1536',
    'per_request',
    '0.25',
  ],
  [
    'image_generation.high.1536x1024',
    '图片生成 high 1536x1024',
    'per_request',
    '0.25',
  ],
];

const newPrices = () => ({ input: '', output: '' });
const newTier = (index) => ({
  id: `tier_${index + 1}`,
  priority: index + 1,
  fallback: index === 0,
  conditions:
    index === 0
      ? []
      : [{ metric: 'input_total_tokens', operator: 'lte', value: 0 }],
  prices: newPrices(),
});
const newAdjustment = (index) => ({
  id: `adjustment_${index + 1}`,
  multiplier: '1',
  conditions: [
    { source: 'hour', operator: 'gte', value: '0', timezone: 'Asia/Shanghai' },
  ],
});

const adjustmentOperatorOptions = [
  { value: 'eq', label: '等于（eq）' },
  { value: 'contains', label: '包含（contains）' },
  { value: 'exists', label: '存在（exists）' },
  { value: 'lt', label: '小于（lt）' },
  { value: 'lte', label: '小于等于（lte）' },
  { value: 'gt', label: '大于（gt）' },
  { value: 'gte', label: '大于等于（gte）' },
];

const numericOperatorOptions = [
  { value: 'lt', label: '小于（lt）' },
  { value: 'lte', label: '小于等于（lte）' },
  { value: 'gt', label: '大于（gt）' },
  { value: 'gte', label: '大于等于（gte）' },
];

const operatorsBySource = {
  hour: ['eq', 'lt', 'lte', 'gt', 'gte'],
  weekday: ['eq'],
  header: ['eq', 'contains', 'exists'],
  param: ['eq', 'contains', 'exists', 'lt', 'lte', 'gt', 'gte'],
};

function normalizePriceValue(value) {
  return value === null || value === undefined ? '' : String(value);
}

function normalizePrices(prices = {}) {
  const next = {};
  for (const [key] of priceFields) {
    next[key] = normalizePriceValue(prices?.[key]);
  }
  return next;
}

function normalizeToolPrices(tools = {}) {
  const next = JSON.parse(JSON.stringify(tools || {}));
  for (const [key, , unit, defaultPrice] of toolPriceFields) {
    next[key] = {
      unit: next[key]?.unit || unit,
      price: normalizePriceValue(next[key]?.price ?? defaultPrice),
    };
  }
  return next;
}

function normalizeTimeValue(source, value) {
  const raw = String(value ?? '').trim();
  if (!raw || (source !== 'hour' && source !== 'weekday')) return raw;
  const match = raw.match(/^(\d{1,2})(?::\d{2})?$/);
  return match ? match[1] : raw;
}

function normalizePolicyForEditor(policy) {
  if (!policy) return null;
  const source = JSON.parse(JSON.stringify(policy));
  const mode = source.mode || 'per_token';
  return {
    version: Number.isInteger(source.version) ? source.version : 1,
    mode,
    currency: source.currency || 'USD',
    unit:
      mode === 'per_request'
        ? 'per_request'
        : source.unit || 'per_million_tokens',
    price: normalizePriceValue(source.price),
    prices: normalizePrices(source.prices),
    tiers:
      mode === 'tiered'
        ? (source.tiers || [])
            .map((tier, index) => ({
              id: String(tier?.id || `tier_${index + 1}`),
              priority: Number.isInteger(tier?.priority)
                ? tier.priority
                : index + 1,
              fallback: Boolean(tier?.fallback),
              conditions: Array.isArray(tier?.conditions)
                ? tier.conditions.map((condition) => ({
                    metric: condition?.metric || 'input_total_tokens',
                    operator: condition?.operator || 'lte',
                    value: Number.isSafeInteger(condition?.value)
                      ? condition.value
                      : Number(condition?.value) || 0,
                  }))
                : [],
              prices: normalizePrices(tier?.prices),
            }))
            .sort((left, right) => left.priority - right.priority)
        : [],
    adjustments: Array.isArray(source.adjustments)
      ? source.adjustments.map((adjustment, index) => ({
          id: String(adjustment?.id || `adjustment_${index + 1}`),
          multiplier: normalizePriceValue(adjustment?.multiplier || '1'),
          conditions: Array.isArray(adjustment?.conditions)
            ? adjustment.conditions.map((condition) => ({
                source: condition?.source || 'hour',
                path: String(condition?.path || ''),
                operator: condition?.operator || 'eq',
                value: normalizeTimeValue(condition?.source, condition?.value),
                timezone: String(condition?.timezone || ''),
              }))
            : [],
        }))
      : [],
    tools: normalizeToolPrices(source.tools),
  };
}

function getOperatorOptions(source, t) {
  const allowed = operatorsBySource[source] || [];
  return adjustmentOperatorOptions
    .filter((item) => allowed.includes(item.value))
    .map((item) => ({ ...item, label: t(item.label) }));
}

function isValidTimezone(timezone) {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: timezone }).format();
    return true;
  } catch {
    return false;
  }
}

function isNonNegativeDecimal(value, required = false) {
  const raw = String(value ?? '').trim();
  if (!raw) return !required;
  return /^\d+(\.\d+)?$/.test(raw) && Number.isFinite(Number(raw));
}

function validatePrices(prices, location, t) {
  if (!isNonNegativeDecimal(prices?.input, true)) {
    return t('{{location}}的输入价格必填，且必须是非负数字', { location });
  }
  for (const [key, label] of priceFields) {
    if (!isNonNegativeDecimal(prices?.[key])) {
      return t('{{location}}的{{label}}价格必须是非负数字', {
        location,
        label: t(label),
      });
    }
  }
  return '';
}

function validatePolicy(policy, t) {
  if (!['per_token', 'per_request', 'tiered'].includes(policy?.mode))
    return t('请选择有效的计费模式');
  if (
    policy.mode === 'per_request' &&
    !isNonNegativeDecimal(policy.price, true)
  )
    return t('每次请求价格必填，且必须是非负数字');
  if (policy.mode === 'per_token') {
    const error = validatePrices(policy.prices, t('基础定价'), t);
    if (error) return error;
  }
  if (policy.mode === 'tiered') {
    if (!policy.tiers?.length) return t('阶梯价格至少需要一个阶梯');
    if (policy.tiers.filter((tier) => tier.fallback).length !== 1)
      return t('阶梯价格必须且只能有一个兜底阶梯');
    const ids = new Set();
    const priorities = new Set();
    for (let index = 0; index < policy.tiers.length; index++) {
      const tier = policy.tiers[index];
      const location = t('阶梯 {{index}}', { index: index + 1 });
      if (!String(tier.id || '').trim())
        return t('{{location}}缺少标识', { location });
      if (ids.has(tier.id))
        return t('阶梯标识不能重复：{{id}}', { id: tier.id });
      ids.add(tier.id);
      if (!Number.isInteger(tier.priority))
        return t('{{location}}的优先级必须是整数', { location });
      if (priorities.has(tier.priority))
        return t('阶梯优先级不能重复：{{priority}}', {
          priority: tier.priority,
        });
      priorities.add(tier.priority);
      if (!tier.fallback && !tier.conditions?.length)
        return t('{{location}}不是兜底阶梯，至少需要一个条件', { location });
      for (const condition of tier.conditions || []) {
        if (
          !['input_total_tokens', 'output_total_tokens'].includes(
            condition.metric,
          )
        )
          return t('{{location}}包含无效的 Token 指标', { location });
        if (!['lt', 'lte', 'gt', 'gte'].includes(condition.operator))
          return t('{{location}}包含无效的比较符号', { location });
        if (!Number.isSafeInteger(condition.value) || condition.value < 0)
          return t('{{location}}的 Token 阈值必须是非负整数', { location });
      }
      const error = validatePrices(tier.prices, location, t);
      if (error) return error;
    }
  }
  const adjustmentIDs = new Set();
  for (let index = 0; index < (policy.adjustments || []).length; index++) {
    const adjustment = policy.adjustments[index];
    const location = t('动态调价规则 {{index}}', { index: index + 1 });
    if (!String(adjustment.id || '').trim())
      return t('{{location}}缺少标识', { location });
    if (adjustmentIDs.has(adjustment.id))
      return t('动态调价规则标识不能重复：{{id}}', { id: adjustment.id });
    adjustmentIDs.add(adjustment.id);
    if (
      !isNonNegativeDecimal(adjustment.multiplier, true) ||
      Number(adjustment.multiplier) <= 0
    )
      return t('{{location}}的价格倍率必须大于 0', { location });
    if (!adjustment.conditions?.length)
      return t('{{location}}至少需要一个条件', { location });
    for (const condition of adjustment.conditions) {
      if (!operatorsBySource[condition.source]?.includes(condition.operator))
        return t('{{location}}的来源与比较符号不匹配', { location });
      if (condition.source === 'hour' || condition.source === 'weekday') {
        if (!isValidTimezone(String(condition.timezone || '').trim()))
          return t('{{location}}的时区无效', { location });
        if (!String(condition.value ?? '').trim())
          return t('{{location}}必须填写时间比较值', { location });
        const value = Number(condition.value);
        const max = condition.source === 'hour' ? 23 : 6;
        if (!Number.isInteger(value) || value < 0 || value > max)
          return t('{{location}}的{{source}}值必须是 0–{{max}} 的整数', {
            location,
            source: t(condition.source === 'hour' ? '小时' : '星期'),
            max,
          });
      } else {
        const path = String(condition.path || '').trim();
        if (!path) return t('{{location}}必须填写字段', { location });
        if (
          condition.source === 'header' &&
          !/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(path)
        )
          return t('{{location}}的请求头名称不合法', { location });
        if (condition.source === 'param' && /\s/.test(path))
          return t('{{location}}的请求参数路径不能包含空白字符', { location });
        if (
          condition.operator !== 'exists' &&
          !String(condition.value ?? '').trim()
        )
          return t('{{location}}必须填写比较值', { location });
        if (
          ['lt', 'lte', 'gt', 'gte'].includes(condition.operator) &&
          !Number.isFinite(Number(condition.value))
        )
          return t('{{location}}使用数值比较时，比较值必须是数字', {
            location,
          });
      }
    }
  }
  return '';
}

function getConditionHelp(condition, t) {
  const sourceHelp = {
    hour: {
      title: t('小时条件'),
      text: t(
        '按所选时区的当前小时判断，取值为 0–23 的整数。例：gte 18 表示每天 18:00–23:59；lt 6 表示每天 00:00–05:59。跨午夜的闲时段请拆成两条规则。',
      ),
    },
    weekday: {
      title: t('星期条件'),
      text: t(
        '按所选时区的星期判断：0=周日、1=周一、2=周二、3=周三、4=周四、5=周五、6=周六。例：eq 0 表示每周日。周末需要分别建立周六和周日两条规则。',
      ),
    },
    header: {
      title: t('请求头条件'),
      text: t(
        '字段填写请求头名称，例如 anthropic-beta。请求头名称按小写查找；eq 要求值完全相同，contains 检查值中是否包含指定片段，exists 只检查该请求头是否携带。',
      ),
    },
    param: {
      title: t('请求参数条件'),
      text: t(
        '字段填写 JSON 路径，例如 service_tier 或 metadata.plan。eq 要求参数值完全相同，contains 检查字符串片段，exists 只检查该路径是否存在；数值比较会把参数值转换为数字。',
      ),
    },
  };
  const operatorHelp = {
    eq: t('当前操作符：实际值必须与配置值完全相等。'),
    contains: t('当前操作符：实际字符串包含配置片段即可，区分大小写。'),
    exists: t(
      '当前操作符：只判断字段是否存在，不检查内容；用于小时或星期没有筛选意义。',
    ),
    lt: t('当前操作符：实际值必须小于配置值，两侧都必须能转换为数字。'),
    lte: t('当前操作符：实际值必须小于或等于配置值，两侧都必须能转换为数字。'),
    gt: t('当前操作符：实际值必须大于配置值，两侧都必须能转换为数字。'),
    gte: t('当前操作符：实际值必须大于或等于配置值，两侧都必须能转换为数字。'),
  };
  const source = sourceHelp[condition.source] || sourceHelp.hour;
  return {
    title: source.title,
    text: `${source.text} ${operatorHelp[condition.operator] || ''}`,
  };
}

function PriceFields({ value = {}, onChange }) {
  const { t } = useTranslation();
  return (
    <div className='grid grid-cols-1 gap-3 md:grid-cols-2'>
      {priceFields.map(([key, label]) => (
        <div key={key}>
          <Text type='tertiary'>{t(label)} ($ / 1M tokens)</Text>
          <Input
            value={value[key] || ''}
            onChange={(next) => onChange({ ...value, [key]: next })}
            placeholder={key === 'input' ? t('必填') : t('留空表示不单独计价')}
          />
        </div>
      ))}
    </div>
  );
}

function ToolPriceFields({ value = {}, onChange }) {
  const { t } = useTranslation();
  return (
    <div className='grid grid-cols-1 gap-3 md:grid-cols-2'>
      {toolPriceFields.map(([key, label, unit]) => (
        <div key={key}>
          <Text type='tertiary'>
            {t(label)} (
            {unit === 'per_request' ? '$ / request' : '$ / 1K calls'})
          </Text>
          <Input
            value={value[key]?.price || ''}
            onChange={(price) => onChange({ ...value, [key]: { unit, price } })}
          />
        </div>
      ))}
    </div>
  );
}

function TierEditor({ tiers, onChange }) {
  const { t } = useTranslation();
  const updateTier = (index, patch) =>
    onChange(
      tiers.map((tier, i) => (i === index ? { ...tier, ...patch } : tier)),
    );
  return (
    <div className='space-y-3'>
      <Banner
        type='info'
        title={t('阶梯优先级说明')}
        description={t(
          '优先级数字越小越优先。系统会按优先级从小到大检查非兜底阶梯，并使用第一个满足全部条件的阶梯；只有所有非兜底阶梯都不匹配时，才使用兜底阶梯。优先级不能重复。',
        )}
      />
      {tiers.map((tier, index) => (
        <Card
          key={index}
          title={
            <span className='font-semibold'>
              {t('阶梯')} {index + 1}
            </span>
          }
          headerExtraContent={
            <Button
              type='danger'
              onClick={() => onChange(tiers.filter((_, i) => i !== index))}
            >
              {t('删除')}
            </Button>
          }
        >
          <div className='grid grid-cols-1 gap-3 md:grid-cols-3'>
            <Input
              value={tier.id}
              prefix={`${t('标识')}：`}
              onChange={(id) => updateTier(index, { id })}
            />
            <Input
              value={String(tier.priority)}
              prefix={`${t('优先级')}：`}
              onChange={(priority) =>
                updateTier(index, { priority: Number(priority) || 0 })
              }
            />
            <Checkbox
              checked={Boolean(tier.fallback)}
              onChange={(event) =>
                updateTier(index, {
                  fallback: event.target.checked,
                  conditions: event.target.checked
                    ? []
                    : tier.conditions?.length
                      ? tier.conditions
                      : [
                          {
                            metric: 'input_total_tokens',
                            operator: 'lte',
                            value: 0,
                          },
                        ],
                })
              }
            >
              {t('兜底阶梯')}
            </Checkbox>
          </div>
          {!tier.fallback && (
            <div className='my-3 space-y-2'>
              {(tier.conditions || []).map((condition, conditionIndex) => (
                <Space key={conditionIndex} wrap>
                  <Select
                    value={condition.metric}
                    onChange={(metric) =>
                      updateTier(index, {
                        conditions: tier.conditions.map((item, i) =>
                          i === conditionIndex ? { ...item, metric } : item,
                        ),
                      })
                    }
                    optionList={[
                      {
                        value: 'input_total_tokens',
                        label: t('总输入 Tokens'),
                      },
                      {
                        value: 'output_total_tokens',
                        label: t('总输出 Tokens'),
                      },
                    ]}
                  />
                  <Select
                    value={condition.operator}
                    onChange={(operator) =>
                      updateTier(index, {
                        conditions: tier.conditions.map((item, i) =>
                          i === conditionIndex ? { ...item, operator } : item,
                        ),
                      })
                    }
                    optionList={numericOperatorOptions.map((item) => ({
                      ...item,
                      label: t(item.label),
                    }))}
                  />
                  <Input
                    value={String(condition.value)}
                    onChange={(value) =>
                      updateTier(index, {
                        conditions: tier.conditions.map((item, i) =>
                          i === conditionIndex
                            ? { ...item, value: Number(value) || 0 }
                            : item,
                        ),
                      })
                    }
                  />
                  <Button
                    onClick={() =>
                      updateTier(index, {
                        conditions: tier.conditions.filter(
                          (_, i) => i !== conditionIndex,
                        ),
                      })
                    }
                  >
                    {t('删除条件')}
                  </Button>
                </Space>
              ))}
              <Button
                onClick={() =>
                  updateTier(index, {
                    conditions: [
                      ...(tier.conditions || []),
                      {
                        metric: 'input_total_tokens',
                        operator: 'lte',
                        value: 0,
                      },
                    ],
                  })
                }
              >
                {t('添加条件')}
              </Button>
            </div>
          )}
          <PriceFields
            value={tier.prices}
            onChange={(prices) => updateTier(index, { prices })}
          />
        </Card>
      ))}
      <Button
        theme='solid'
        onClick={() => onChange([...tiers, newTier(tiers.length)])}
      >
        {t('添加阶梯')}
      </Button>
    </div>
  );
}

function AdjustmentEditor({ adjustments, onChange }) {
  const { t } = useTranslation();
  const update = (index, patch) =>
    onChange(
      adjustments.map((item, i) =>
        i === index ? { ...item, ...patch } : item,
      ),
    );
  return (
    <div className='space-y-3'>
      {adjustments.map((adjustment, index) => (
        <Card
          key={index}
          title={
            <span className='font-semibold'>
              {t('动态调价规则')} {index + 1}
            </span>
          }
          headerExtraContent={
            <Button
              type='danger'
              onClick={() =>
                onChange(adjustments.filter((_, i) => i !== index))
              }
            >
              {t('删除')}
            </Button>
          }
        >
          <Space wrap>
            <Input
              value={adjustment.id}
              prefix={`${t('标识')}：`}
              onChange={(id) => update(index, { id })}
            />
            <Input
              value={adjustment.multiplier}
              prefix={`${t('价格倍率')}：`}
              onChange={(multiplier) => update(index, { multiplier })}
            />
          </Space>
          <div className='mt-3 space-y-2'>
            {(adjustment.conditions || []).map((condition, conditionIndex) => {
              const setCondition = (patch) =>
                update(index, {
                  conditions: adjustment.conditions.map((item, i) =>
                    i === conditionIndex ? { ...item, ...patch } : item,
                  ),
                });
              const help = getConditionHelp(condition, t);
              return (
                <Card key={conditionIndex} shadows='hover'>
                  <Space wrap>
                    <Select
                      value={condition.source}
                      onChange={(source) =>
                        setCondition({
                          source,
                          operator: 'eq',
                          value: '',
                          path: '',
                          timezone:
                            source === 'hour' || source === 'weekday'
                              ? 'Asia/Shanghai'
                              : '',
                        })
                      }
                      optionList={[
                        ['hour', '小时'],
                        ['weekday', '星期'],
                        ['header', '请求头'],
                        ['param', '请求参数'],
                      ].map(([value, label]) => ({ value, label: t(label) }))}
                    />
                    {(condition.source === 'header' ||
                      condition.source === 'param') && (
                      <Input
                        value={condition.path || ''}
                        prefix={`${t('字段')}：`}
                        onChange={(path) => setCondition({ path })}
                        placeholder={
                          condition.source === 'header'
                            ? 'anthropic-beta'
                            : 'service_tier'
                        }
                      />
                    )}
                    {(condition.source === 'hour' ||
                      condition.source === 'weekday') && (
                      <Input
                        value={condition.timezone || ''}
                        prefix={`${t('时区')}：`}
                        onChange={(timezone) => setCondition({ timezone })}
                        placeholder='Asia/Shanghai'
                      />
                    )}
                    <Select
                      value={condition.operator}
                      onChange={(operator) =>
                        setCondition({
                          operator,
                          value: operator === 'exists' ? '' : condition.value,
                        })
                      }
                      optionList={getOperatorOptions(condition.source, t)}
                    />
                    {condition.operator !== 'exists' && (
                      <Input
                        value={condition.value || ''}
                        prefix={`${t('值')}：`}
                        onChange={(value) => setCondition({ value })}
                        placeholder={
                          condition.source === 'hour'
                            ? '18'
                            : condition.source === 'weekday'
                              ? '0'
                              : 'priority'
                        }
                      />
                    )}
                    <Button
                      onClick={() =>
                        update(index, {
                          conditions: adjustment.conditions.filter(
                            (_, i) => i !== conditionIndex,
                          ),
                        })
                      }
                    >
                      {t('删除条件')}
                    </Button>
                  </Space>
                  <Banner
                    className='mt-3'
                    type='info'
                    title={help.title}
                    description={help.text}
                  />
                </Card>
              );
            })}
            <Button
              onClick={() =>
                update(index, {
                  conditions: [
                    ...(adjustment.conditions || []),
                    {
                      source: 'hour',
                      operator: 'gte',
                      value: '0',
                      timezone: 'Asia/Shanghai',
                    },
                  ],
                })
              }
            >
              {t('添加条件')}
            </Button>
          </div>
        </Card>
      ))}
      <Button
        onClick={() =>
          onChange([...adjustments, newAdjustment(adjustments.length)])
        }
      >
        {t('添加动态调价规则')}
      </Button>
    </div>
  );
}

export default function BillingPolicyVisualEditor({
  visible,
  model,
  policy,
  onCancel,
  onSave,
  saving = false,
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState(() => normalizePolicyForEditor(policy));
  useEffect(() => {
    setDraft(normalizePolicyForEditor(policy));
  }, [policy, visible]);
  if (!draft) return null;
  const save = () => {
    const error = validatePolicy(draft, t);
    if (error) {
      Toast.error(error);
      return;
    }
    onSave(draft);
  };
  const setMode = (mode) =>
    setDraft({
      ...draft,
      mode,
      unit: mode === 'per_request' ? 'per_request' : 'per_million_tokens',
      price: mode === 'per_request' ? draft.price || '' : '',
      prices: mode === 'per_token' ? draft.prices || newPrices() : {},
      tiers:
        mode === 'tiered'
          ? draft.tiers?.length
            ? draft.tiers
            : [newTier(0)]
          : [],
    });
  return (
    <Modal
      title={
        <span>
          {t('可视化编辑模型计费策略')} · {model || ''}
        </span>
      }
      visible={visible}
      onCancel={onCancel}
      onOk={save}
      confirmLoading={saving}
      width={1080}
      bodyStyle={{ maxHeight: '72vh', overflowY: 'auto' }}
    >
      <Space wrap className='mb-4'>
        <Select
          value={draft.mode}
          onChange={setMode}
          optionList={[
            { value: 'per_token', label: t('按 Token') },
            { value: 'per_request', label: t('按次') },
            { value: 'tiered', label: t('阶梯价格') },
          ]}
        />
        <Select
          value={draft.currency || 'USD'}
          onChange={(currency) => setDraft({ ...draft, currency })}
          optionList={[{ value: 'USD', label: 'USD' }]}
        />
        <Text type='tertiary'>
          {t('价格均为模型原始价格，不包含分组、等级、全局或渠道倍率')}
        </Text>
      </Space>
      <Tabs type='line'>
        <TabPane tab={t('基础定价')} itemKey='pricing'>
          {draft.mode === 'per_request' && (
            <Input
              value={draft.price || ''}
              prefix={`${t('每次请求价格')} $`}
              onChange={(price) => setDraft({ ...draft, price })}
            />
          )}
          {draft.mode === 'per_token' && (
            <PriceFields
              value={draft.prices}
              onChange={(prices) => setDraft({ ...draft, prices })}
            />
          )}
          {draft.mode === 'tiered' && (
            <TierEditor
              tiers={draft.tiers || []}
              onChange={(tiers) => setDraft({ ...draft, tiers })}
            />
          )}
        </TabPane>
        <TabPane tab={t('闲时与忙时')} itemKey='adjustments'>
          <Banner
            type='warning'
            title={t('规则计算方式')}
            description={t(
              '同一规则里的多个条件必须全部满足（AND）；多条规则可以同时命中，命中的价格倍率会依次相乘。例如闲时折扣 0.8 与特殊请求倍率 1.2 同时命中，最终动态倍率为 0.96。规则只调整模型原始价格，不替代分组倍率或用户等级折扣。',
            )}
          />
          <div className='mt-3'>
            <AdjustmentEditor
              adjustments={draft.adjustments || []}
              onChange={(adjustments) => setDraft({ ...draft, adjustments })}
            />
          </div>
        </TabPane>
        <TabPane tab={t('工具定价')} itemKey='tools'>
          <ToolPriceFields
            value={draft.tools || {}}
            onChange={(tools) => setDraft({ ...draft, tools })}
          />
        </TabPane>
        <TabPane tab={t('JSON 预览')} itemKey='json'>
          <TextArea
            value={JSON.stringify(draft, null, 2)}
            readonly
            autosize={{ minRows: 18, maxRows: 30 }}
            style={{ fontFamily: 'monospace' }}
          />
        </TabPane>
      </Tabs>
    </Modal>
  );
}
