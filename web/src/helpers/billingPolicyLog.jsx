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
import { Typography } from '@douyinfe/semi-ui';
import { getCurrencyConfig, renderQuota } from './render';

const fieldLabels = {
  input: '输入',
  output: '输出',
  cache_read: '缓存读取',
  cache_write: '缓存写入',
  cache_write_5m: 'Anthropic 缓存写入 5m',
  cache_write_1h: 'Anthropic 缓存写入 1h',
  image_input: '图片输入',
  audio_input: '音频输入',
  audio_output: '音频输出',
};

const modeLabels = {
  per_token: '按 Token',
  per_request: '按次',
  tiered: '阶梯定价',
};

const ratioLabels = {
  n: '数量倍率',
  seconds: '时长倍率',
  size: '尺寸倍率',
  prompt_extend: '提示词扩展倍率',
};

const additionalChargeLabels = {
  web_search: 'Web搜索',
  claude_web_search: 'Claude Web搜索',
  file_search: '文件搜索',
  audio_input: '音频输入',
  image_generation: '图片生成调用',
};

const toolFieldLabels = {
  'web_search.standard': 'Web搜索（标准）',
  'web_search.premium': 'Web搜索（高级）',
  claude_web_search: 'Claude Web搜索',
  file_search: '文件搜索',
};

function number(value, fallback = 0) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function displayUSD(value, digits = 6) {
  const { symbol, rate } = getCurrencyConfig();
  return `${symbol}${(number(value) * number(rate, 1)).toFixed(digits)}`;
}

function displayRatio(value) {
  const ratio = number(value, 1);
  return Number.isInteger(ratio) ? ratio.toFixed(1) : String(ratio);
}

export function isToolBillingLineItem(item) {
  return (
    item?.kind === 'tool' ||
    (item?.field !== 'request' && number(item?.units) > 0)
  );
}

function toolLabel(field, t) {
  if (field?.startsWith('image_generation.')) {
    const [, quality, size] = field.split('.');
    const detail = [quality, size].filter(Boolean).join(' · ');
    return detail ? `${t('图片生成调用')} (${detail})` : t('图片生成调用');
  }
  return t(toolFieldLabels[field] || additionalChargeLabels[field] || field);
}

function toolUnitPrice(unit, unitPrice, t) {
  if (unit === 'per_thousand_calls') {
    return `${displayUSD(unitPrice)} / 1K ${t('次')}`;
  }
  return `${displayUSD(unitPrice)} / ${t('次')}`;
}

function toolRow(field, units, unit, unitPrice, costUSD, t) {
  return {
    field,
    label: toolLabel(field, t),
    units: number(units),
    unit,
    unitPrice,
    costUSD,
  };
}

export function getBillingPolicyToolRows(other, t) {
  const snapshot = other?.billing_policy;
  const lineItems = snapshot?.calculation?.line_items;
  const toolItems = Array.isArray(lineItems)
    ? lineItems.filter(isToolBillingLineItem)
    : [];
  if (toolItems.length > 0) {
    return toolItems.map((item) =>
      toolRow(
        item.field,
        item.units,
        item.unit ||
          (item.field?.startsWith('image_generation.')
            ? 'per_request'
            : 'per_thousand_calls'),
        item.unit_price,
        item.cost_usd,
        t,
      ),
    );
  }

  const structuredCharges = Array.isArray(snapshot?.additional_charges)
    ? snapshot.additional_charges.filter(
        (charge) => charge?.field !== 'audio_input',
      )
    : [];
  if (structuredCharges.length > 0) {
    return structuredCharges.map((charge) =>
      toolRow(
        charge.field,
        charge.units,
        charge.unit,
        charge.unit_price,
        charge.cost_usd,
        t,
      ),
    );
  }

  const rows = [];
  const appendLegacy = (
    field,
    units,
    unitPrice,
    unit = 'per_thousand_calls',
  ) => {
    if (number(units) <= 0 || number(unitPrice) <= 0) {
      return;
    }
    const divisor = unit === 'per_thousand_calls' ? 1000 : 1;
    rows.push(
      toolRow(
        field,
        units,
        unit,
        unitPrice,
        (number(units) * number(unitPrice)) / divisor,
        t,
      ),
    );
  };
  appendLegacy(
    'web_search',
    other?.web_search_call_count,
    other?.web_search_price,
  );
  appendLegacy(
    'claude_web_search',
    other?.claude_web_search_call_count,
    other?.claude_web_search_price,
  );
  appendLegacy(
    'file_search',
    other?.file_search_call_count,
    other?.file_search_price,
  );
  appendLegacy(
    'image_generation',
    other?.image_generation_call ? 1 : 0,
    other?.image_generation_call_price,
    'per_request',
  );
  return rows;
}

export function getBillingPolicyModeLabel(other, t) {
  const mode = other?.billing_policy?.calculation?.mode;
  return mode ? t(modeLabels[mode] || mode) : '';
}

export function getBillingPolicyLogLines(other, t, showTokenScaling = false) {
  const snapshot = other?.billing_policy;
  const calculation = snapshot?.calculation;
  if (!calculation || !Array.isArray(calculation.line_items)) {
    return [];
  }

  const mode = t(modeLabels[calculation.mode] || calculation.mode);
  const tier = calculation.tier_id
    ? ` · ${t('阶梯')} ${calculation.tier_id}`
    : '';
  const lines = [`${t('计费模式')}：${mode} · v${snapshot.revision}${tier}`];
  const tokenScaling = showTokenScaling
    ? other?.admin_info?.billing_policy_token_scaling
    : null;
  const rawLineItemTokens = tokenScaling?.raw_line_item_tokens;
  const ratioFormula = tokenScaling
    ? [
        tokenScaling.system_global_model_ratio,
        tokenScaling.channel_model_ratio,
        tokenScaling.user_global_model_ratio,
      ]
        .map(displayRatio)
        .join('×')
    : '';

  const toolRows = getBillingPolicyToolRows(other, t);
  let modelSubtotal = 0;
  for (const item of calculation.line_items) {
    if (isToolBillingLineItem(item)) {
      continue;
    }
    modelSubtotal += number(item.cost_usd);
    if (item.field === 'request') {
      lines.push(
        `${t('按次')}：1 × ${displayUSD(item.unit_price)} = ${displayUSD(item.cost_usd)}`,
      );
      continue;
    }
    const label = t(fieldLabels[item.field] || item.field);
    const scaledTokens = number(item.tokens);
    const rawTokens = number(rawLineItemTokens?.[item.field], NaN);
    const tokenDisplay =
      ratioFormula && Number.isFinite(rawTokens)
        ? `(${scaledTokens.toLocaleString()}=${rawTokens.toLocaleString()}×${ratioFormula})`
        : scaledTokens.toLocaleString();
    lines.push(
      `${label}：${tokenDisplay} × ${displayUSD(item.price_per_million)} / 1M = ${displayUSD(item.cost_usd)}`,
    );
  }

  if (toolRows.length > 0) {
    const toolSubtotal = toolRows.reduce(
      (total, row) => total + number(row.costUSD),
      0,
    );
    lines.push(`${t('模型费用小计')}：${displayUSD(modelSubtotal)}`);
    lines.push(`${t('工具费用小计')}：${displayUSD(toolSubtotal)}`);
  }
  lines.push(`${t('计费小计')}：${displayUSD(calculation.subtotal_usd)}`);

  for (const adjustment of calculation.applied_adjustments || []) {
    lines.push(`${t('策略调整')} ${adjustment.id}：× ${adjustment.multiplier}`);
  }
  if (number(calculation.adjustment_multiplier, 1) !== 1) {
    lines.push(
      `${t('策略调整后')}：${displayUSD(calculation.total_usd)}（× ${calculation.adjustment_multiplier}）`,
    );
  }

  const baseGroupRatio = number(other?.base_group_ratio, NaN);
  const levelRatio = number(other?.user_level_ratio, NaN);
  const effectiveGroupRatio = number(snapshot.group_ratio, 1);
  if (other?.group_ratio_source === 'user_group_special') {
    lines.push(`${t('专属倍率')}：× ${effectiveGroupRatio}`);
  } else {
    if (Number.isFinite(baseGroupRatio)) {
      lines.push(`${t('分组倍率')}：× ${baseGroupRatio}`);
    }
    if (Number.isFinite(levelRatio) && levelRatio !== 1) {
      lines.push(`${t('等级折扣')}：× ${levelRatio}`);
    }
    if (
      !Number.isFinite(baseGroupRatio) ||
      !Number.isFinite(levelRatio) ||
      baseGroupRatio * levelRatio !== effectiveGroupRatio
    ) {
      lines.push(`${t('最终分组倍率')}：× ${effectiveGroupRatio}`);
    }
  }

  for (const [name, ratio] of Object.entries(snapshot.other_ratios || {})) {
    lines.push(`${t(ratioLabels[name] || name)}：× ${ratio}`);
  }

  lines.push(`${t('实际扣费')}：${renderQuota(snapshot.actual_quota, 6)}`);
  return lines;
}

export function renderBillingPolicyLogDetail(
  other,
  t,
  showTokenScaling = false,
) {
  const lines = getBillingPolicyLogLines(other, t, showTokenScaling);
  const toolRows = getBillingPolicyToolRows(other, t);
  if (!lines.length) {
    return null;
  }
  return (
    <article>
      {lines.map((line, index) => (
        <p key={`${index}-${line}`}>{line}</p>
      ))}
      {toolRows.length > 0 && (
        <div style={{ marginTop: 12, overflowX: 'auto' }}>
          <Typography.Text strong>{t('工具费用明细')}</Typography.Text>
          <table
            style={{
              width: '100%',
              minWidth: 520,
              marginTop: 8,
              borderCollapse: 'collapse',
              fontSize: 12,
            }}
          >
            <thead>
              <tr style={{ background: 'var(--semi-color-fill-0)' }}>
                {[t('项目'), t('用量'), t('单价'), t('小计')].map((heading) => (
                  <th
                    key={heading}
                    style={{
                      padding: '7px 10px',
                      textAlign: 'left',
                      fontWeight: 600,
                    }}
                  >
                    {heading}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {toolRows.map((row, index) => (
                <tr
                  key={`${row.field}-${index}`}
                  style={{ borderBottom: '1px solid var(--semi-color-border)' }}
                >
                  <td style={{ padding: '8px 10px' }}>{row.label}</td>
                  <td style={{ padding: '8px 10px' }}>
                    {number(row.units).toLocaleString()} {t('次')}
                  </td>
                  <td style={{ padding: '8px 10px' }}>
                    {toolUnitPrice(row.unit, row.unitPrice, t)}
                  </td>
                  <td style={{ padding: '8px 10px' }}>
                    {displayUSD(row.costUSD)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </article>
  );
}
