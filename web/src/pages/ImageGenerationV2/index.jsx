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

import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Select, Toast, Typography } from '@douyinfe/semi-ui';
import { ArrowLeft, RefreshCcw, Sparkles } from 'lucide-react';
import { API, showError } from '../../helpers';
import { fetchTokenKey } from '../../helpers/token';

const { Text, Title } = Typography;

const IMAGE_MODEL = 'gpt-image-2';
const IMAGE_SITE_URL = 'https://image.jucodex.com';
const PROMPT_REF_URL = 'https://prompt.doingfb.com';

function tokenSupportsModel(token, model) {
  if (!token || token.status !== 1) return false;
  if (!token.model_limits_enabled) return true;

  const limits = String(token.model_limits || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);

  if (limits.length === 0) return false;
  return limits.includes(model);
}

const ImageGenerationV2 = () => {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState([]);
  const [tokensLoading, setTokensLoading] = useState(false);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [jumping, setJumping] = useState(false);
  const [imageSiteUrl, setImageSiteUrl] = useState('');

  const usableTokens = useMemo(
    () => tokens.filter((token) => tokenSupportsModel(token, IMAGE_MODEL)),
    [tokens],
  );

  const tokenOptions = useMemo(
    () =>
      usableTokens.map((token) => ({
        value: String(token.id),
        label: (
          <div className='flex items-center justify-between gap-3 min-w-0'>
            <span className='truncate'>{token.name || `#${token.id}`}</span>
            <span className='text-xs text-[var(--semi-color-text-2)] shrink-0'>
              {token.key ? `sk-${token.key}` : `#${token.id}`}
            </span>
          </div>
        ),
      })),
    [usableTokens],
  );

  const loadTokens = async () => {
    setTokensLoading(true);
    try {
      const res = await API.get('/api/token/?p=1&size=100');
      const { success, data, message } = res.data || {};
      if (!success) {
        showError(message || t('加载令牌失败'));
        return;
      }
      const items = Array.isArray(data) ? data : data?.items || [];
      setTokens(items);
      const firstUsable = items.find((token) =>
        tokenSupportsModel(token, IMAGE_MODEL),
      );
      if (firstUsable) {
        setSelectedTokenId((current) => current || String(firstUsable.id));
      }
    } catch (error) {
      showError(error);
    } finally {
      setTokensLoading(false);
    }
  };

  useEffect(() => {
    loadTokens();
  }, []);

  const handleStart = async () => {
    if (!selectedTokenId) {
      Toast.warning(t('请先选择生图令牌'));
      return;
    }
    setJumping(true);
    try {
      const rawKey = await fetchTokenKey(selectedTokenId);
      const targetUrl = `${IMAGE_SITE_URL}?apiKey=${encodeURIComponent(
        `sk-${rawKey}`,
      )}&apiMode=images&model=${IMAGE_MODEL}`;
      setImageSiteUrl(targetUrl);
    } catch (error) {
      Toast.error({
        content: error?.message || t('获取令牌失败'),
        duration: 0,
      });
    } finally {
      setJumping(false);
    }
  };

  const inGenerateView = Boolean(imageSiteUrl);

  return (
    <div className='mt-[60px] h-[calc(100vh-84px)] min-h-[560px] pb-4 flex flex-col gap-4'>
      <div className='shrink-0 rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)] px-4 py-3'>
        <div className='flex flex-col gap-3 md:flex-row md:items-center'>
          <Text strong className='shrink-0'>
            {t('请选择生图令牌')}
          </Text>
          <Select
            className='flex-1 min-w-0 md:max-w-[520px]'
            placeholder={t('选择令牌')}
            loading={tokensLoading}
            optionList={tokenOptions}
            value={selectedTokenId || undefined}
            onChange={(value) => setSelectedTokenId(String(value || ''))}
            emptyContent={t('暂无可用令牌')}
            filter
          />
          <div className='flex items-center gap-2 shrink-0'>
            <Button
              icon={<RefreshCcw size={15} />}
              onClick={loadTokens}
              loading={tokensLoading}
            />
            <Button
              type='primary'
              theme='solid'
              icon={<Sparkles size={16} />}
              loading={jumping}
              onClick={handleStart}
            >
              {t('开始生图')}
            </Button>
          </div>
        </div>
        {tokens.length > 0 && usableTokens.length === 0 && (
          <Text type='warning' size='small' className='block mt-2'>
            {t('当前没有支持 gpt-image-2 的启用令牌。')}
          </Text>
        )}
      </div>

      <div className='flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)]'>
        <div className='shrink-0 px-4 py-3 border-b border-[var(--semi-color-border)] flex items-center justify-between gap-3'>
          <Title heading={5} className='!mb-0'>
            {inGenerateView ? t('图像生成-V2') : t('提示词参考')}
          </Title>
          {inGenerateView && (
            <Button
              size='small'
              icon={<ArrowLeft size={14} />}
              onClick={() => setImageSiteUrl('')}
            >
              {t('返回提示词参考')}
            </Button>
          )}
        </div>
        {inGenerateView ? (
          <iframe
            key={imageSiteUrl}
            src={imageSiteUrl}
            title={t('图像生成-V2')}
            className='min-h-0 flex-1 w-full border-0'
            sandbox='allow-scripts allow-same-origin allow-popups allow-forms allow-downloads'
          />
        ) : (
          <iframe
            src={PROMPT_REF_URL}
            title={t('提示词参考')}
            className='min-h-0 flex-1 w-full border-0'
            sandbox='allow-scripts allow-same-origin allow-popups allow-forms'
          />
        )}
      </div>
    </div>
  );
};

export default ImageGenerationV2;
