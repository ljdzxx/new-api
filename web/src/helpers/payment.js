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

export function submitPaymentForm({ url, fields }) {
  if (!url) {
    throw new Error('支付表单地址无效');
  }
  const form = document.createElement('form');
  form.action = url;
  form.method = 'POST';
  const isSafari =
    navigator.userAgent.indexOf('Safari') > -1 &&
    navigator.userAgent.indexOf('Chrome') < 1;
  if (!isSafari) {
    form.target = '_blank';
  }
  Object.keys(fields || {}).forEach((key) => {
    const input = document.createElement('input');
    input.type = 'hidden';
    input.name = key;
    input.value = fields[key];
    form.appendChild(input);
  });
  document.body.appendChild(form);
  form.submit();
  document.body.removeChild(form);
}

export function executePaymentCheckout(result) {
  if (!result) {
    throw new Error('支付响应为空');
  }

  switch (result.action_type) {
    case 'redirect_url':
      if (!result.url) {
        throw new Error('支付跳转地址无效');
      }
      window.open(result.url, '_blank');
      return;
    case 'form_post':
      if (!result.form?.url) {
        throw new Error('支付表单信息无效');
      }
      submitPaymentForm({
        url: result.form.url,
        fields: result.form.fields || {},
      });
      return;
    case 'qr_code':
      if (!result.qr_code_url) {
        throw new Error('支付二维码地址无效');
      }
      window.open(result.qr_code_url, '_blank');
      return;
    default:
      throw new Error('不支持的支付类型');
  }
}
