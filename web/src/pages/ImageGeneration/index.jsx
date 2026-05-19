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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Empty,
  ImagePreview,
  InputNumber,
  Select,
  SideSheet,
  Spin,
  Tag,
  TextArea,
  Toast,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import {
  CircleHelp,
  FileText,
  ImagePlus,
  RefreshCcw,
  Sparkles,
  Trash2,
} from 'lucide-react';
import { API, showError } from '../../helpers';
import { fetchTokenKey, getServerAddress } from '../../helpers/token';

const { Text, Title, Paragraph } = Typography;

const IMAGE_MODEL = 'gpt-image-2';
const DEFAULT_PROMPT = '';
const FIXED_IMAGE_COUNT = 1;

const ZoomInIcon = () => (
  <svg
    xmlns='http://www.w3.org/2000/svg'
    width='14'
    height='14'
    viewBox='0 0 24 24'
    fill='none'
    stroke='currentColor'
    strokeWidth='2'
    strokeLinecap='round'
    strokeLinejoin='round'
    className='lucide lucide-zoom-in'
    aria-hidden='true'
  >
    <circle cx='11' cy='11' r='8' />
    <line x1='21' x2='16.65' y1='21' y2='16.65' />
    <line x1='11' x2='11' y1='8' y2='14' />
    <line x1='8' x2='14' y1='11' y2='11' />
  </svg>
);

const DownloadIcon = () => (
  <svg
    xmlns='http://www.w3.org/2000/svg'
    width='14'
    height='14'
    viewBox='0 0 24 24'
    fill='none'
    stroke='currentColor'
    strokeWidth='2'
    strokeLinecap='round'
    strokeLinejoin='round'
    className='lucide lucide-download'
    aria-hidden='true'
  >
    <path d='M12 15V3' />
    <path d='M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4' />
    <path d='m7 10 5 5 5-5' />
  </svg>
);

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

function fileToPreview(file) {
  return {
    id: `${file.name}-${file.size}-${file.lastModified}`,
    file,
    url: URL.createObjectURL(file),
  };
}

function buildImageUrl(item) {
  if (item?.b64_json) return `data:image/png;base64,${item.b64_json}`;
  return item?.url || '';
}

function getImageFilename(index) {
  const time = new Date().toISOString().replace(/[:.]/g, '-');
  return `image-generation-${time}-${index + 1}.png`;
}

function downloadImage(src, index) {
  if (!src) return;
  const link = document.createElement('a');
  link.href = src;
  link.download = getImageFilename(index);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
}

const ImageGeneration = () => {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState([]);
  const [tokensLoading, setTokensLoading] = useState(false);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [size, setSize] = useState('auto');
  const [quality, setQuality] = useState('auto');
  const [referenceImages, setReferenceImages] = useState([]);
  const [results, setResults] = useState([]);
  const [responseMeta, setResponseMeta] = useState(null);
  const [generating, setGenerating] = useState(false);
  const [docVisible, setDocVisible] = useState(false);
  const [previewVisible, setPreviewVisible] = useState(false);
  const [previewSrc, setPreviewSrc] = useState('');
  const fileInputRef = useRef(null);
  const referenceImagesRef = useRef([]);

  const serverAddress = useMemo(() => getServerAddress(), []);
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

  const hasReferenceImages = referenceImages.length > 0;

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

  useEffect(() => {
    referenceImagesRef.current = referenceImages;
  }, [referenceImages]);

  useEffect(() => {
    return () => {
      referenceImagesRef.current.forEach((item) =>
        URL.revokeObjectURL(item.url),
      );
    };
  }, []);

  const handleFilesChange = (event) => {
    const files = Array.from(event.target.files || []);
    if (files.length === 0) return;
    setReferenceImages((current) => [
      ...current,
      ...files
        .filter((file) => file.type.startsWith('image/'))
        .map(fileToPreview),
    ]);
    event.target.value = '';
  };

  const removeReferenceImage = (id) => {
    setReferenceImages((current) => {
      const target = current.find((item) => item.id === id);
      if (target) URL.revokeObjectURL(target.url);
      return current.filter((item) => item.id !== id);
    });
  };

  const clearReferenceImages = () => {
    referenceImages.forEach((item) => URL.revokeObjectURL(item.url));
    setReferenceImages([]);
  };

  const validateBeforeSubmit = () => {
    if (!selectedTokenId) {
      Toast.warning(t('请选择可用于 gpt-image-2 的令牌'));
      return false;
    }
    if (!prompt.trim()) {
      Toast.warning(t('请输入提示词'));
      return false;
    }
    return true;
  };

  const buildJsonPayload = () => ({
    model: IMAGE_MODEL,
    prompt: prompt.trim(),
    size,
    n: FIXED_IMAGE_COUNT,
    quality,
    response_format: 'url',
  });

  const buildFormPayload = () => {
    const formData = new FormData();
    formData.append('model', IMAGE_MODEL);
    formData.append('prompt', prompt.trim());
    formData.append('size', size);
    formData.append('n', String(FIXED_IMAGE_COUNT));
    formData.append('quality', quality);
    formData.append('response_format', 'url');
    referenceImages.forEach((item) => {
      formData.append('image[]', item.file);
    });
    return formData;
  };

  const handleGenerate = async () => {
    if (!validateBeforeSubmit()) return;

    setGenerating(true);
    try {
      const rawKey = await fetchTokenKey(selectedTokenId);
      const endpoint = hasReferenceImages
        ? '/v1/images/edits'
        : '/v1/images/generations';
      const payload = hasReferenceImages
        ? buildFormPayload()
        : buildJsonPayload();

      const res = await API.post(endpoint, payload, {
        headers: {
          Authorization: `Bearer sk-${rawKey}`,
        },
        //timeout: 120000,
        skipErrorHandler: true,
      });

      const data = res.data || {};
      const imageData = Array.isArray(data.data) ? data.data : [];
      if (imageData.length === 0) {
        Toast.warning(t('接口未返回图片数据'));
      }
      setResults(imageData);
      setResponseMeta({
        created: data.created,
        model: data.model || IMAGE_MODEL,
        size: data.size || size,
        quality: data.quality,
        background: data.background,
        output_format: data.output_format,
        usage: data.usage,
      });
    } catch (error) {
      const message =
        error?.response?.data?.error?.message ||
        error?.response?.data?.message ||
        error?.message ||
        t('生成失败');
      showError(message);
    } finally {
      setGenerating(false);
    }
  };

  const openPreview = (src) => {
    setPreviewSrc(src);
    setPreviewVisible(true);
  };

  const renderDocs = () => (
    <div className='flex flex-col gap-5'>
      <section>
        <Title heading={6} className='!mb-2'>
          {t('接口说明')}
        </Title>
        <Paragraph className='text-sm text-[var(--semi-color-text-2)]'>
          {t(
            '当前页面调用图片生成接口。无参考图时使用 generations，有参考图时使用 edits。',
          )}
        </Paragraph>
      </section>
      <section>
        <Title heading={6} className='!mb-2'>
          Base URL
        </Title>
        <code className='block px-3 py-2 rounded-lg bg-[var(--semi-color-fill-0)] text-sm break-all'>
          {serverAddress}/v1
        </code>
      </section>
      <section>
        <Title heading={6} className='!mb-2'>
          {t('认证方式')}
        </Title>
        <Paragraph className='text-sm text-[var(--semi-color-text-2)]'>
          {t('使用用户 API Key，请求头为 Authorization: Bearer YOUR_API_KEY。')}
        </Paragraph>
      </section>
      <section>
        <Title heading={6} className='!mb-2'>
          {t('请求示例')} - generations
        </Title>
        <pre className='px-4 py-3 rounded-lg bg-[var(--semi-color-fill-0)] text-xs overflow-x-auto whitespace-pre-wrap break-all leading-relaxed'>
          {`curl -X POST '${serverAddress}/v1/images/generations' \\
  -H 'Authorization: Bearer YOUR_API_KEY' \\
  -H 'Content-Type: application/json' \\
  -d '{
    "model": "${IMAGE_MODEL}",
    "prompt": "a cinematic neon city in heavy rain",
    "size": "auto",
    "n": 1,
    "quality": "auto",
    "response_format": "url"
  }'`}
        </pre>
      </section>
      <section>
        <Title heading={6} className='!mb-2'>
          {t('请求示例<带参考图>')} - edits
        </Title>
        <pre className='px-4 py-3 rounded-lg bg-[var(--semi-color-fill-0)] text-xs overflow-x-auto whitespace-pre-wrap break-all leading-relaxed'>
          {`curl -X POST '${serverAddress}/v1/images/edits' \\
  -H 'Authorization: Bearer YOUR_API_KEY' \\
  -F 'model=${IMAGE_MODEL}' \\
  -F 'prompt=a cinematic neon city in heavy rain' \\
  -F 'size=auto' \\
  -F 'n=1' \\
  -F 'quality=auto' \\
  -F 'response_format=url' \\
  -F 'image[]=@/path/to/image1.png' \\
  -F 'image[]=@/path/to/image2.png'`}
        </pre>
      </section>
      <section>
        <Title heading={6} className='!mb-2'>
          {t('注意事项')}
        </Title>
        <ul className='text-sm text-[var(--semi-color-text-2)] list-disc pl-4 flex flex-col gap-1'>
          <li>{t('页面默认请求 URL 返回，后端可将 b64_json 结果转存为临时 URL。')}</li>
          <li>
            {t('品质默认为 auto，由模型自行决定合适的生成质量。')}
          </li>
          <li>{t('同步生成可能耗时 10-60 秒，请耐心等待。')}</li>
        </ul>
      </section>
    </div>
  );

  return (
    <div className='mt-[60px] h-[calc(100vh-84px)] min-h-[620px] overflow-hidden'>
      <div className='h-full flex flex-col lg:flex-row gap-4'>
        <aside className='lg:w-[360px] xl:w-[400px] shrink-0 h-full overflow-y-auto rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)]'>
          <div className='px-4 py-3 border-b border-[var(--semi-color-border)] flex items-center justify-between'>
            <div className='min-w-0'>
              <Title heading={5} className='!mb-0'>
                {t('图像生成')}
              </Title>
              <Text type='tertiary' size='small'>
                {hasReferenceImages ? 'edits' : 'generations'} / {IMAGE_MODEL}
              </Text>
            </div>
            <Tooltip content={t('API 调用文档')}>
              <Button
                icon={<FileText size={16} />}
                theme='borderless'
                type='tertiary'
                onClick={() => setDocVisible(true)}
              />
            </Tooltip>
          </div>

          <div className='p-4 flex flex-col gap-4'>
            <div className='flex flex-col gap-2'>
              <div className='flex items-center justify-between gap-2'>
                <Text strong>{t('令牌')}</Text>
                <Tooltip
                  content={t(
                    '如果没有可用令牌，请先创建一个支持 GPT Image-2 的令牌',
                  )}
                >
                  <CircleHelp size={15} color='var(--semi-color-text-2)' />
                </Tooltip>
              </div>
              <div className='flex gap-2'>
                <Select
                  className='flex-1'
                  placeholder={t('选择令牌')}
                  loading={tokensLoading}
                  optionList={tokenOptions}
                  value={selectedTokenId || undefined}
                  onChange={(value) => setSelectedTokenId(String(value || ''))}
                  emptyContent={t('暂无可用令牌')}
                  filter
                />
                <Tooltip content={t('刷新令牌')}>
                  <Button
                    icon={<RefreshCcw size={15} />}
                    onClick={loadTokens}
                    loading={tokensLoading}
                  />
                </Tooltip>
              </div>
              {tokens.length > 0 && usableTokens.length === 0 && (
                <Text type='warning' size='small'>
                  {t('当前没有支持 gpt-image-2 的启用令牌。')}
                </Text>
              )}
            </div>

            <div className='flex flex-col gap-2'>
              <Text strong>{t('提示词')}</Text>
              <TextArea
                value={prompt}
                onChange={setPrompt}
                autosize={{ minRows: 5, maxRows: 10 }}
                placeholder={t('描述你想生成或编辑的图片')}
                maxCount={4000}
                showClear
              />
            </div>

            <div className='grid grid-cols-2 gap-3'>
              <div className='flex flex-col gap-2 min-w-0'>
                <Text strong>{t('模型')}</Text>
                <Select
                  value={IMAGE_MODEL}
                  optionList={[{ label: IMAGE_MODEL, value: IMAGE_MODEL }]}
                  disabled
                />
              </div>
              <div className='flex flex-col gap-2 min-w-0'>
                <Text strong>{t('尺寸')}</Text>
                <Select
                  value={size}
                  onChange={setSize}
                  optionList={[
                    { label: '1024x1024 (square)', value: '1024x1024' },
                    { label: '1536x1024 (landscape)', value: '1536x1024' },
                    { label: '1024x1536 (portrait)', value: '1024x1536' },
                    { label: '2048x2048 (2K square)', value: '2048x2048' },
                    {
                      label: '2048x1152 (2K landscape)',
                      value: '2048x1152',
                    },
                    {
                      label: '3840x2160 (4K landscape)',
                      value: '3840x2160',
                    },
                    {
                      label: '2160x3840 (4K portrait)',
                      value: '2160x3840',
                    },
                    { label: 'auto (default)', value: 'auto' },
                  ]}
                />
                <Text type='tertiary' size='small'>
                  {t('推荐使用 Auto，由模型自行决定最佳尺寸')}
                </Text>
              </div>
              <div className='flex flex-col gap-2 min-w-0'>
                <Text strong>{t('数量')}</Text>
                <InputNumber
                  min={1}
                  max={1}
                  value={FIXED_IMAGE_COUNT}
                  disabled
                  style={{ width: '100%' }}
                />
              </div>
              <div className='flex flex-col gap-2 min-w-0'>
                <Text strong>{t('品质')}</Text>
                <Select
                  value={quality}
                  onChange={setQuality}
                  optionList={[
                    { label: 'auto', value: 'auto' },
                    { label: 'high', value: 'high' },
                    { label: 'medium', value: 'medium' },
                    { label: 'low', value: 'low' },
                  ]}
                />
              </div>
            </div>

            <div className='rounded-lg border border-[var(--semi-color-border)] p-3'>
              <div className='flex items-center justify-between gap-2 mb-3'>
                <div className='flex items-center gap-2'>
                  <Text strong>{t('参考图')}</Text>
                  <Tag color='grey' size='small'>
                    {t('可选')}
                  </Tag>
                </div>
                <Text type='tertiary' size='small'>
                  {hasReferenceImages
                    ? t('将使用 edits 接口')
                    : t('将使用 generations 接口')}
                </Text>
              </div>

              <div className='grid grid-cols-4 gap-2'>
                <button
                  type='button'
                  className='aspect-[3/4] rounded-lg border-2 border-dashed border-[var(--semi-color-border)] bg-[var(--semi-color-fill-0)] flex items-center justify-center hover:border-[var(--semi-color-primary)] transition-colors'
                  onClick={() => fileInputRef.current?.click()}
                >
                  <ImagePlus size={18} color='var(--semi-color-text-2)' />
                </button>
                {referenceImages.map((item) => (
                  <div
                    key={item.id}
                    className='aspect-[3/4] relative overflow-hidden rounded-lg border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-2)]'
                  >
                    <img
                      src={item.url}
                      alt={item.file.name}
                      className='w-full h-full object-cover'
                    />
                    <button
                      type='button'
                      className='absolute top-1 right-1 w-6 h-6 rounded bg-black/55 text-white flex items-center justify-center'
                      onClick={() => removeReferenceImage(item.id)}
                      aria-label={t('删除')}
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                ))}
              </div>
              <input
                ref={fileInputRef}
                type='file'
                accept='image/*'
                multiple
                hidden
                onChange={handleFilesChange}
              />
              {hasReferenceImages && (
                <Button
                  className='mt-3'
                  size='small'
                  type='tertiary'
                  icon={<Trash2 size={14} />}
                  onClick={clearReferenceImages}
                >
                  {t('清空参考图')}
                </Button>
              )}
            </div>
          </div>

          <div className='p-4 border-t border-[var(--semi-color-border)]'>
            <Button
              block
              type='primary'
              theme='solid'
              size='large'
              icon={<Sparkles size={17} />}
              loading={generating}
              onClick={handleGenerate}
            >
              {generating ? t('生成中...') : t('开始生成')}
            </Button>
          </div>
        </aside>

        <main className='flex-1 min-w-0 h-full rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)] overflow-hidden flex flex-col'>
          <div className='px-5 py-3 border-b border-[var(--semi-color-border)] flex items-center justify-between gap-3'>
            <div>
              <Title heading={5} className='!mb-0'>
                {t('生成结果')}
              </Title>
              <Text type='tertiary' size='small'>
                {results.length > 0
                  ? t('共 {{count}} 张图片', { count: results.length })
                  : t('生成的图片会显示在这里')}
              </Text>
            </div>
            <div className='hidden md:flex items-center gap-2'>
              <Tag color='blue'>response_format=url</Tag>
              <Tag color='teal'>quality={quality}</Tag>
            </div>
          </div>

          <div className='mx-4 mt-3'>
            <Banner
              type='warning'
              bordered={false}
              description={
                <div className='text-xs leading-relaxed'>
                  <div>
                    {t(
                      '图片仅在当前页面临时保存，刷新或离开后无法找回，请及时下载。',
                    )}
                  </div>
                  <div>{t('请勿生成违反法律法规或平台规则的内容。')}</div>
                </div>
              }
            />
          </div>

          <Spin
            spinning={generating}
            tip={t('正在等待图像生成结果...')}
            wrapperClassName='flex-1 min-h-0'
          >
            <div className='h-full overflow-y-auto p-4'>
              {results.length === 0 ? (
                <div className='h-full min-h-[360px] flex items-center justify-center'>
                  <Empty
                    image={
                      <Sparkles size={42} color='var(--semi-color-text-2)' />
                    }
                    title={t('暂无图片')}
                    description={t('填写提示词后点击开始生成')}
                  />
                </div>
              ) : (
                <div className='columns-1 md:columns-2 xl:columns-3 2xl:columns-4 gap-3'>
                  {results.map((item, index) => {
                    const src = buildImageUrl(item);
                    return (
                      <div
                        key={`${src.slice(0, 48)}-${index}`}
                        className='break-inside-avoid mb-3 group relative rounded-xl overflow-hidden bg-[var(--semi-color-bg-2)] border border-[var(--semi-color-border)] cursor-zoom-in'
                        onClick={() => openPreview(src)}
                      >
                        <img
                          src={src}
                          alt={item.revised_prompt || prompt}
                          className='w-full block transition-transform duration-300 group-hover:scale-[1.03]'
                        />
                        <div className='absolute top-2 right-2 flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity'>
                          <Tooltip content={t('放大')}>
                            <Button
                              size='small'
                              type='primary'
                              className='!bg-black/60 !border-0 hover:!bg-black/80 !rounded-full'
                              icon={<ZoomInIcon />}
                              onClick={(event) => {
                                event.stopPropagation();
                                openPreview(src);
                              }}
                            />
                          </Tooltip>
                          <Tooltip content={t('下载')}>
                            <Button
                              size='small'
                              type='primary'
                              className='!bg-black/60 !border-0 hover:!bg-black/80 !rounded-full'
                              icon={<DownloadIcon />}
                              onClick={(event) => {
                                event.stopPropagation();
                                downloadImage(src, index);
                              }}
                            />
                          </Tooltip>
                        </div>
                        {item.revised_prompt && (
                          <div className='absolute bottom-0 left-0 right-0 p-3 bg-gradient-to-t from-black/75 to-transparent opacity-0 group-hover:opacity-100 transition-opacity'>
                            <p className='text-xs text-white/90 leading-relaxed line-clamp-4'>
                              {item.revised_prompt}
                            </p>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}

              {responseMeta && (
                <div className='mt-2 text-xs text-[var(--semi-color-text-2)] flex flex-wrap gap-2'>
                  <span>
                    {t('模型')}: {responseMeta.model}
                  </span>
                  <span>
                    {t('尺寸')}: {responseMeta.size || '-'}
                  </span>
                  <span>
                    {t('格式')}: {responseMeta.output_format || 'png'}
                  </span>
                  {responseMeta.usage?.total_tokens != null && (
                    <span>total_tokens: {responseMeta.usage.total_tokens}</span>
                  )}
                </div>
              )}
            </div>
          </Spin>
        </main>
      </div>

      <SideSheet
        title={t('API 调用文档')}
        visible={docVisible}
        onCancel={() => setDocVisible(false)}
        width={480}
      >
        {renderDocs()}
      </SideSheet>

      <ImagePreview
        src={previewSrc}
        visible={previewVisible}
        onVisibleChange={setPreviewVisible}
      />
    </div>
  );
};

export default ImageGeneration;
