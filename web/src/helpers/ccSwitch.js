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

import { encodeToBase64 } from './base64';

const USAGE_SCRIPT = String.raw`({
  request: {
    url: "{{baseUrl}}".replace(/\/+$/, "").replace(/\/v1$/, "") + "/v1/usage",
    method: "GET",
    headers: {
      "Authorization": "Bearer {{apiKey}}"
    }
  },
  extractor: function(response) {
    if (response.isValid !== true) {
      return {
        isValid: false,
        invalidMessage: response.message || "查询失败"
      };
    }

    var result = {
      isValid: true,
      planName: response.planName || "令牌额度",
      unit: response.unit || "USD"
    };

    if (response.remaining === -1) {
      result.extra = "无限额度";
    } else if (typeof response.remaining === "number") {
      result.remaining = response.remaining;
    } else {
      result.extra = "无可用额度来源";
    }

    if (response.quota) {
      result.total = response.quota.limit;
      result.used = response.quota.used;
    }

    return result;
  }
})`;

export function getCCSwitchName(systemName, tokenName) {
  const name = typeof systemName === 'string' ? systemName.trim() : '';
  const validName = name && name !== 'undefined' && name !== 'null';
  return `${validName ? name : 'NewApi'}-${tokenName || ''}`;
}

export function buildCCSwitchURL({ app, name, models, apiKey, serverAddress }) {
  const baseUrl = serverAddress.trim().replace(/\/+$/, '').replace(/\/v1$/, '');
  const endpoint = app === 'codex' ? `${baseUrl}/v1` : baseUrl;
  const params = new URLSearchParams({
    resource: 'provider',
    app,
    name,
    endpoint,
    apiKey,
    homepage: baseUrl,
    enabled: 'true',
    usageEnabled: 'true',
    usageAutoInterval: '30',
    usageScript: encodeToBase64(USAGE_SCRIPT),
  });
  for (const [key, value] of Object.entries(models)) {
    if (value) params.set(key, value);
  }
  return `ccswitch://v1/import?${params.toString()}`;
}
