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
import { Button, Tag, Tooltip } from '@douyinfe/semi-ui';
import { Copy, Gauge } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../../helpers';
import { handleCopyUrl, handleSpeedTest } from '../../../helpers/dashboard';

export default function TokenAPIEndpoints() {
  const { t } = useTranslation();
  const [endpoints, setEndpoints] = useState([]);
  useEffect(() => {
    let active = true;
    API.get('/api/user/self/api-info')
      .then(({ data }) => {
        if (!active) return;
        if (data.success)
          setEndpoints(Array.isArray(data.data) ? data.data : []);
        else showError(data.message);
      })
      .catch((error) => {
        if (active) showError(error.message);
      });
    return () => {
      active = false;
    };
  }, []);

  if (!endpoints.length) return null;

  return (
    <div
      className='flex flex-wrap items-center gap-2 pb-4'
      aria-label={t('API端点')}
    >
      {endpoints.map((api, index) => (
        <div
          key={`${api.id ?? api.url}-${index}`}
          className='flex max-w-full items-center gap-2 rounded-lg border px-3 py-1 text-xs'
          style={{
            borderColor: 'var(--semi-color-border)',
            background: 'var(--semi-color-fill-0)',
          }}
        >
          <span
            className='shrink-0'
            style={{ color: 'var(--semi-color-text-2)' }}
          >
            {t('API端点')}
          </span>
          <Tooltip content={api.description || api.route}>
            <Tag size='small' color={api.color || 'blue'} className='shrink-0'>
              {api.route}
            </Tag>
          </Tooltip>
          <Tooltip content={t('复制API地址')}>
            <button
              type='button'
              className='min-w-0 cursor-pointer break-all border-0 bg-transparent p-0 text-left font-mono hover:underline'
              style={{ color: 'var(--semi-color-primary)' }}
              onClick={() => handleCopyUrl(api.url, t)}
              aria-label={`${t('复制API地址')} ${api.url}`}
            >
              {api.url}
            </button>
          </Tooltip>
          <Tooltip content={t('复制API地址')}>
            <Button
              size='small'
              theme='borderless'
              type='tertiary'
              className='shrink-0'
              icon={<Copy size={13} />}
              aria-label={`${t('复制API地址')} ${api.route}`}
              onClick={() => handleCopyUrl(api.url, t)}
            />
          </Tooltip>
          <Tooltip content={t('测速')}>
            <Button
              size='small'
              theme='borderless'
              type='tertiary'
              className='shrink-0'
              icon={<Gauge size={13} />}
              aria-label={`${t('测速')} ${api.route}`}
              onClick={() => handleSpeedTest(api.url)}
            />
          </Tooltip>
        </div>
      ))}
    </div>
  );
}
