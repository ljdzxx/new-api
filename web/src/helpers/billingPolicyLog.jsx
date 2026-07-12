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

function number(value, fallback = 0) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function displayUSD(value, digits = 6) {
  const { symbol, rate } = getCurrencyConfig();
  return `${symbol}${(number(value) * number(rate, 1)).toFixed(digits)}`;
}

export function getBillingPolicyModeLabel(other, t) {
  const mode = other?.billing_policy?.calculation?.mode;
  return mode ? t(modeLabels[mode] || mode) : '';
}

export function getBillingPolicyLogLines(other, t) {
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

  for (const item of calculation.line_items) {
    if (item.field === 'request') {
      lines.push(
        `${t('按次')}：1 × ${displayUSD(item.unit_price)} = ${displayUSD(item.cost_usd)}`,
      );
      continue;
    }
    const label = t(fieldLabels[item.field] || item.field);
    lines.push(
      `${label}：${number(item.tokens).toLocaleString()} × ${displayUSD(item.price_per_million)} / 1M = ${displayUSD(item.cost_usd)}`,
    );
  }

  lines.push(`${t('模型计费小计')}：${displayUSD(calculation.subtotal_usd)}`);

  for (const adjustment of calculation.applied_adjustments || []) {
    lines.push(`${t('策略调整')} ${adjustment.id}：× ${adjustment.multiplier}`);
  }
  if (number(calculation.adjustment_multiplier, 1) !== 1) {
    lines.push(
      `${t('策略调整后')}：${displayUSD(calculation.total_usd)}（× ${calculation.adjustment_multiplier}）`,
    );
  }

  const hasStructuredCharges = Object.prototype.hasOwnProperty.call(
    snapshot,
    'additional_charges',
  );
  for (const charge of snapshot.additional_charges || []) {
    const label = t(additionalChargeLabels[charge.field] || charge.field);
    const denominator =
      charge.unit === 'per_thousand_calls'
        ? ' / 1K'
        : charge.unit === 'per_million_tokens'
          ? ' / 1M'
          : '';
    lines.push(
      `${label}：${number(charge.units).toLocaleString()} × ${displayUSD(charge.unit_price)}${denominator} = ${displayUSD(charge.cost_usd)}`,
    );
  }
  if (hasStructuredCharges && number(snapshot.additional_charges_usd) > 0) {
    lines.push(
      `${t('附加费用小计')}：${displayUSD(snapshot.additional_charges_usd)}`,
    );
  }
  if (hasStructuredCharges) {
    lines.push(
      `${t('结算前小计')}：${displayUSD(snapshot.billable_subtotal_usd)}`,
    );
  } else {
    // Compatibility for active-policy logs written before structured charges.
    if (other?.web_search_call_count > 0) {
      lines.push(
        `${t('Web搜索')}：${other.web_search_call_count} × ${displayUSD(number(other.web_search_price) / 1000)}`,
      );
    }
    if (other?.file_search_call_count > 0) {
      lines.push(
        `${t('文件搜索')}：${other.file_search_call_count} × ${displayUSD(number(other.file_search_price) / 1000)}`,
      );
    }
    if (other?.image_generation_call_price > 0) {
      lines.push(
        `${t('图片生成调用')}：${displayUSD(other.image_generation_call_price)}`,
      );
    }
    if (other?.audio_input_token_count > 0 && other?.audio_input_price > 0) {
      lines.push(
        `${t('音频输入')}：${number(other.audio_input_token_count).toLocaleString()} × ${displayUSD(other.audio_input_price)} / 1M`,
      );
    }
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

export function renderBillingPolicyLogDetail(other, t) {
  const lines = getBillingPolicyLogLines(other, t);
  if (!lines.length) {
    return null;
  }
  return (
    <article>
      {lines.map((line, index) => (
        <p key={`${index}-${line}`}>{line}</p>
      ))}
    </article>
  );
}
