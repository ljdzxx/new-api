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

export const SERVICE_TIER_TAG_COLORS = [
  'grey',
  'red',
  'pink',
  'purple',
  'violet',
  'indigo',
  'blue',
  'light-blue',
  'cyan',
  'teal',
  'green',
  'light-green',
  'lime',
  'yellow',
  'amber',
  'orange',
  'white',
];
export const SERVICE_TIER_TAG_TYPES = ['light', 'solid', 'ghost'];

export function validateServiceTierBilling(config) {
  if (!config) return true;
  const rules = config.rules;
  if (!Array.isArray(rules) || (config.enabled && rules.length === 0))
    return false;
  const seen = new Set();
  return rules.every((rule) => {
    if (
      !rule ||
      typeof rule.model !== 'string' ||
      !rule.model ||
      rule.model !== rule.model.trim() ||
      new TextEncoder().encode(rule.model).length > 255 ||
      (rule.model.includes('*') && rule.model !== '*') ||
      seen.has(rule.model)
    )
      return false;
    seen.add(rule.model);
    return (
      typeof rule.tier === 'string' &&
      rule.tier.trim() === rule.tier &&
      rule.tier.length > 0 &&
      new TextEncoder().encode(rule.tier).length <= 64 &&
      SERVICE_TIER_TAG_COLORS.includes(rule.tag_color) &&
      SERVICE_TIER_TAG_TYPES.includes(rule.tag_type) &&
      typeof rule.multiplier === 'number' &&
      Number.isFinite(rule.multiplier) &&
      rule.multiplier > 0 &&
      rule.multiplier <= 1000
    );
  });
}
