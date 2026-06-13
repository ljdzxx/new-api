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
  Modal,
  Select,
  SideSheet,
  Spin,
  Switch,
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
const REQUEST_ID_HEADER_NAMES = [
  'x-oneapi-request-id',
  'x-request-id',
  'request-id',
];
const DEFAULT_REFERENCE_IMAGE_COMPRESSION = {
  enabled: true,
  thresholdMb: 8,
  targetSizeMb: 8,
  maxSide: 2048,
  minJpegQuality: 65,
};
const MAX_JPEG_QUALITY = 0.92;

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

function normalizePositiveNumber(value, fallback) {
  const numericValue = Number(value);
  return Number.isFinite(numericValue) && numericValue > 0
    ? numericValue
    : fallback;
}

function getReferenceImageCompressionConfig(status) {
  const raw = status?.image_reference_compression || {};
  const source = raw;
  const sourceName =
    Object.keys(raw).length > 0
      ? status?._imageReferenceCompressionSourceName || 'status-context'
      : 'default';
  const minJpegQuality = normalizePositiveNumber(
    source.min_jpeg_quality,
    DEFAULT_REFERENCE_IMAGE_COMPRESSION.minJpegQuality,
  );

  return {
    enabled:
      source.enabled === undefined
        ? DEFAULT_REFERENCE_IMAGE_COMPRESSION.enabled
        : source.enabled === true,
    thresholdMb: normalizePositiveNumber(
      source.threshold_mb,
      DEFAULT_REFERENCE_IMAGE_COMPRESSION.thresholdMb,
    ),
    targetSizeMb: normalizePositiveNumber(
      source.target_size_mb,
      DEFAULT_REFERENCE_IMAGE_COMPRESSION.targetSizeMb,
    ),
    maxSide: normalizePositiveNumber(
      source.max_side,
      DEFAULT_REFERENCE_IMAGE_COMPRESSION.maxSide,
    ),
    minJpegQuality: Math.min(92, Math.max(1, minJpegQuality)),
    sourceName,
    raw: source,
  };
}

function getImageEditsBaseUrl(status) {
  const fromStatus = status?.image_edits_base_url;
  if (typeof fromStatus === 'string') return fromStatus.trim();
  return '';
}

function buildImageEndpoint(baseUrl, path) {
  const cleanBaseUrl = String(baseUrl || '')
    .trim()
    .replace(/\/+$/, '');
  const cleanPath = String(path || '').replace(/^\/+/, '');
  if (!cleanBaseUrl) return `/${cleanPath}`;
  if (cleanBaseUrl.endsWith('/v1') && cleanPath.startsWith('v1/')) {
    return `${cleanBaseUrl}/${cleanPath.slice(3)}`;
  }
  return `${cleanBaseUrl}/${cleanPath}`;
}

function buildImageGenerationsEndpoint(baseUrl) {
  return buildImageEndpoint(baseUrl, '/v1/images/generations');
}

function buildImageEditsEndpoint(baseUrl) {
  return buildImageEndpoint(baseUrl, '/v1/images/edits');
}

function formatFileSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function normalizeDownloadPercent(percent) {
  if (!Number.isFinite(percent)) return 0;
  return Math.max(0, Math.min(100, Math.round(percent)));
}

function getCompressedFileName(fileName) {
  const cleanName = String(fileName || 'reference-image').replace(
    /\.[^.]+$/,
    '',
  );
  return `${cleanName || 'reference-image'}.jpg`;
}

function canvasToBlob(canvas, quality) {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) {
          resolve(blob);
        } else {
          reject(new Error('图片压缩失败'));
        }
      },
      'image/jpeg',
      quality,
    );
  });
}

async function createBitmapFromFile(file) {
  try {
    return await createImageBitmap(file, { imageOrientation: 'from-image' });
  } catch {
    return createImageBitmap(file);
  }
}

async function compressReferenceImageFile(file, config) {
  const thresholdBytes = config.thresholdMb * 1024 * 1024;
  const targetBytes = config.targetSizeMb * 1024 * 1024;
  console.info('[image compression] start', {
    name: file.name,
    type: file.type,
    originalSize: file.size,
    originalSizeText: formatFileSize(file.size),
    config: {
      enabled: config.enabled,
      thresholdMb: config.thresholdMb,
      targetSizeMb: config.targetSizeMb,
      maxSide: config.maxSide,
      minJpegQuality: config.minJpegQuality,
      sourceName: config.sourceName,
      raw: config.raw,
    },
  });
  if (!config.enabled || file.size <= thresholdBytes) {
    console.info('[image compression] skipped', {
      name: file.name,
      reason: !config.enabled
        ? 'disabled by system/user setting'
        : 'file size is not greater than threshold',
      originalSize: file.size,
      thresholdBytes,
      thresholdText: formatFileSize(thresholdBytes),
    });
    return {
      file,
      compressed: false,
      originalSize: file.size,
      finalSize: file.size,
    };
  }

  const bitmap = await createBitmapFromFile(file);
  const originalWidth = bitmap.width;
  const originalHeight = bitmap.height;
  const scale = Math.min(
    1,
    config.maxSide / Math.max(originalWidth, originalHeight),
  );
  const width = Math.max(1, Math.round(originalWidth * scale));
  const height = Math.max(1, Math.round(originalHeight * scale));
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d', { alpha: false });
  if (!ctx) {
    throw new Error('无法创建 Canvas 2D 上下文');
  }
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(bitmap, 0, 0, width, height);
  bitmap.close?.();
  console.info('[image compression] decoded and resized', {
    name: file.name,
    originalWidth,
    originalHeight,
    canvasWidth: width,
    canvasHeight: height,
    scale,
  });

  const minQuality = config.minJpegQuality / 100;
  let low = minQuality;
  let high = MAX_JPEG_QUALITY;
  let bestBlob = await canvasToBlob(canvas, high);
  console.info('[image compression] jpeg encode initial', {
    name: file.name,
    quality: high,
    size: bestBlob.size,
    sizeText: formatFileSize(bestBlob.size),
    targetBytes,
    targetText: formatFileSize(targetBytes),
  });

  if (bestBlob.size > targetBytes) {
    bestBlob = null;
    for (let i = 0; i < 7; i += 1) {
      const mid = (low + high) / 2;
      const blob = await canvasToBlob(canvas, mid);
      console.info('[image compression] jpeg encode attempt', {
        name: file.name,
        attempt: i + 1,
        quality: Number(mid.toFixed(4)),
        size: blob.size,
        sizeText: formatFileSize(blob.size),
        accepted: blob.size <= targetBytes,
      });
      if (blob.size <= targetBytes) {
        bestBlob = blob;
        low = mid;
      } else {
        high = mid;
      }
    }
    if (!bestBlob) {
      bestBlob = await canvasToBlob(canvas, minQuality);
      console.info('[image compression] jpeg encode min quality fallback', {
        name: file.name,
        quality: minQuality,
        size: bestBlob.size,
        sizeText: formatFileSize(bestBlob.size),
      });
    }
  }

  if (bestBlob.size >= file.size) {
    console.info(
      '[image compression] kept original because compressed is not smaller',
      {
        name: file.name,
        originalSize: file.size,
        compressedSize: bestBlob.size,
        originalSizeText: formatFileSize(file.size),
        compressedSizeText: formatFileSize(bestBlob.size),
      },
    );
    return {
      file,
      compressed: false,
      originalSize: file.size,
      finalSize: file.size,
    };
  }

  const compressedFile = new File(
    [bestBlob],
    getCompressedFileName(file.name),
    {
      type: 'image/jpeg',
      lastModified: Date.now(),
    },
  );
  console.info('[image compression] success', {
    name: file.name,
    outputName: compressedFile.name,
    originalSize: file.size,
    compressedSize: compressedFile.size,
    originalSizeText: formatFileSize(file.size),
    compressedSizeText: formatFileSize(compressedFile.size),
  });
  return {
    file: compressedFile,
    compressed: true,
    originalSize: file.size,
    finalSize: compressedFile.size,
  };
}

async function prepareReferenceImageFiles(items, shouldCompress, config) {
  const effectiveConfig = {
    ...config,
    enabled: shouldCompress && config.enabled,
  };
  console.info('[image compression] prepare reference images', {
    shouldCompress,
    systemEnabled: config.enabled,
    effectiveEnabled: effectiveConfig.enabled,
    fileCount: items.length,
    config: effectiveConfig,
  });
  const results = [];
  for (const item of items) {
    try {
      const result = await compressReferenceImageFile(
        item.file,
        effectiveConfig,
      );
      console.info('[image compression] file result', {
        name: item.file.name,
        compressed: result.compressed,
        originalSize: result.originalSize,
        finalSize: result.finalSize,
        originalSizeText: formatFileSize(result.originalSize),
        finalSizeText: formatFileSize(result.finalSize),
        outputName: result.file.name,
        outputType: result.file.type,
      });
      results.push(result);
    } catch (error) {
      console.error('[image compression] failed', {
        name: item.file.name,
        type: item.file.type,
        size: item.file.size,
        sizeText: formatFileSize(item.file.size),
        error,
        message: error?.message,
        stack: error?.stack,
      });
      throw error;
    }
  }
  const summary = {
    files: results.map((item) => item.file),
    compressedCount: results.filter((item) => item.compressed).length,
    originalBytes: results.reduce((sum, item) => sum + item.originalSize, 0),
    finalBytes: results.reduce((sum, item) => sum + item.finalSize, 0),
  };
  console.info('[image compression] summary', {
    compressedCount: summary.compressedCount,
    originalBytes: summary.originalBytes,
    finalBytes: summary.finalBytes,
    originalText: formatFileSize(summary.originalBytes),
    finalText: formatFileSize(summary.finalBytes),
  });
  return summary;
}

function buildImageUrl(item) {
  if (item?.b64_json) return `data:image/png;base64,${item.b64_json}`;
  return item?.url || '';
}

function getImageFilename(index) {
  const time = new Date().toISOString().replace(/[:.]/g, '-');
  return `image-generation-${time}-${index + 1}.png`;
}

function createImageRecordId() {
  const random =
    window.crypto?.randomUUID?.() ||
    `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
  return `img_${String(random).replace(/[^a-zA-Z0-9_-]/g, '')}`;
}

function getImageDownloadKey(item, index) {
  if (item?._record_id) {
    return `${item._record_id}-${item._record_index ?? index}`;
  }
  return `${index}-${buildImageUrl(item).slice(0, 64)}`;
}

function triggerBrowserDownload(blobOrUrl, filename) {
  const link = document.createElement('a');
  const objectUrl =
    blobOrUrl instanceof Blob ? URL.createObjectURL(blobOrUrl) : blobOrUrl;
  link.href = objectUrl;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  if (blobOrUrl instanceof Blob) {
    window.setTimeout(() => URL.revokeObjectURL(objectUrl), 1000);
  }
}

async function readBlobError(blob) {
  if (!(blob instanceof Blob)) return 'download failed';
  try {
    const text = await blob.text();
    if (!text) return 'download failed';
    try {
      const data = JSON.parse(text);
      return data?.message || data?.error?.message || text;
    } catch {
      return text;
    }
  } catch {
    return 'download failed';
  }
}

async function fetchImageRecordBlob(item, onProgress) {
  const res = await API.get(`/api/image_records/${item._record_id}/download`, {
    params: { index: item._record_index ?? 0 },
    responseType: 'blob',
    skipErrorHandler: true,
    disableDuplicate: true,
    onDownloadProgress: (progressEvent) => {
      const total = progressEvent.total || 0;
      if (total > 0) {
        onProgress?.((progressEvent.loaded / total) * 100);
      } else if (progressEvent.loaded > 0) {
        onProgress?.(5);
      }
    },
  });
  const blob = res.data;
  if (!(blob instanceof Blob) || blob.size === 0) {
    throw new Error('download failed: empty image');
  }
  const type = String(blob.type || '').toLowerCase();
  if (type.includes('application/json') || type.startsWith('text/')) {
    throw new Error(await readBlobError(blob));
  }
  return blob;
}

async function fetchImageBlobWithProgress(src, onProgress) {
  const response = await fetch(src);
  if (!response.ok) {
    throw new Error(`download failed: ${response.status}`);
  }

  const total = Number(response.headers.get('content-length')) || 0;
  if (!response.body || total <= 0) {
    onProgress?.(5);
    return await response.blob();
  }

  const reader = response.body.getReader();
  const chunks = [];
  let loaded = 0;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    loaded += value.length;
    onProgress?.((loaded / total) * 100);
  }

  return new Blob(chunks, {
    type: response.headers.get('content-type') || 'image/png',
  });
}

async function downloadImage(item, index, onProgress) {
  const src = buildImageUrl(item);
  if (!src) return;
  const filename = getImageFilename(index);
  if (item?._record_id) {
    const blob = await fetchImageRecordBlob(item, onProgress);
    onProgress?.(100);
    triggerBrowserDownload(blob, filename);
    return;
  }

  if (src.startsWith('data:')) {
    onProgress?.(100);
    triggerBrowserDownload(src, filename);
    return;
  }

  const blob = await fetchImageBlobWithProgress(src, onProgress);
  onProgress?.(100);
  triggerBrowserDownload(blob, filename);
}

function attachRecordToImages(imageData, recordId) {
  return imageData.map((item, index) => ({
    ...item,
    _record_id: recordId,
    _record_index: index,
  }));
}

function parseMaybeJson(value) {
  if (typeof value !== 'string') return value;
  const text = value.trim();
  if (!text) return value;
  try {
    return JSON.parse(text);
  } catch {
    return value;
  }
}

function getResponseHeader(headers, name) {
  if (!headers || !name) return '';
  if (typeof headers.get === 'function') {
    return headers.get(name) || headers.get(name.toLowerCase()) || '';
  }
  return headers[name] || headers[name.toLowerCase()] || '';
}

function extractRequestIdFromMessage(message) {
  const match = String(message || '').match(/\(request id:\s*([^)]+)\)/i);
  return match?.[1] || '';
}

function getRequestIdFromError(error, message) {
  const data = parseMaybeJson(error?.response?.data);
  const headers = error?.response?.headers;
  const fromMessage = extractRequestIdFromMessage(message);
  if (fromMessage) return fromMessage;
  const fromBody =
    data?.request_id ||
    data?.requestId ||
    data?.error?.request_id ||
    data?.error?.requestId ||
    '';
  if (fromBody) return fromBody;
  for (const headerName of REQUEST_ID_HEADER_NAMES) {
    const value = getResponseHeader(headers, headerName);
    if (value) return value;
  }
  return '';
}

function appendRequestId(message, requestId) {
  if (!message || !requestId || extractRequestIdFromMessage(message)) {
    return message;
  }
  return `${message} (request id: ${requestId})`;
}

function getBackendErrorMessage(error) {
  const data = parseMaybeJson(error?.response?.data);
  return (
    data?.error?.message ||
    data?.message ||
    data?.description ||
    (typeof data?.error === 'string' ? data.error : '') ||
    (typeof data === 'string' ? data : '') ||
    ''
  );
}

function getReadableError(error, fallback) {
  const rawMessage = getBackendErrorMessage(error);
  const axiosMessage = error?.message || '';
  const message =
    rawMessage ||
    (axiosMessage.startsWith('Request failed with status code')
      ? ''
      : axiosMessage) ||
    fallback;
  return appendRequestId(message, getRequestIdFromError(error, message));
}

function shouldSuggestImageRecordRefresh(error) {
  const status = error?.response?.status;
  if (getBackendErrorMessage(error) && status !== 524) return false;
  if (!status) return true;
  return status === 408 || status === 499 || status === 524;
}

const ImageGeneration = () => {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState([]);
  const [tokensLoading, setTokensLoading] = useState(false);
  const [selectedTokenId, setSelectedTokenId] = useState('');
  const [prompt, setPrompt] = useState(DEFAULT_PROMPT);
  const [size, setSize] = useState('auto');
  const [quality, setQuality] = useState('auto');
  const [autoCompressReferenceImages, setAutoCompressReferenceImages] =
    useState(true);
  const [directCompressionConfig, setDirectCompressionConfig] = useState(null);
  const [directImageEditsBaseUrl, setDirectImageEditsBaseUrl] = useState(null);
  const [referenceImages, setReferenceImages] = useState([]);
  const [results, setResults] = useState([]);
  const [responseMeta, setResponseMeta] = useState(null);
  const [activeRecordId, setActiveRecordId] = useState('');
  const [recordStatus, setRecordStatus] = useState('');
  const [recordLoading, setRecordLoading] = useState(false);
  const [recordDeleting, setRecordDeleting] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [downloadProgressMap, setDownloadProgressMap] = useState({});
  const [docVisible, setDocVisible] = useState(false);
  const [previewVisible, setPreviewVisible] = useState(false);
  const [previewSrc, setPreviewSrc] = useState('');
  const [previewIndex, setPreviewIndex] = useState(0);
  const fileInputRef = useRef(null);
  const referenceImagesRef = useRef([]);
  const previewObjectUrlRef = useRef('');
  const previewRequestRef = useRef(0);
  const activeDownloadsRef = useRef(new Set());

  const serverAddress = useMemo(() => getServerAddress(), []);
  const referenceImageCompressionConfig = useMemo(() => {
    const status =
      directCompressionConfig !== null
        ? {
            _imageReferenceCompressionSourceName: 'api-image-config',
            image_reference_compression: directCompressionConfig,
          }
        : undefined;
    return getReferenceImageCompressionConfig(status);
  }, [directCompressionConfig]);
  const imageEditsBaseUrl = useMemo(() => {
    if (directImageEditsBaseUrl !== null) return directImageEditsBaseUrl;
    return '';
  }, [directImageEditsBaseUrl]);
  const imageEditsEndpoint = useMemo(
    () => buildImageEditsEndpoint(imageEditsBaseUrl),
    [imageEditsBaseUrl],
  );
  const imageGenerationsEndpoint = useMemo(
    () => buildImageGenerationsEndpoint(imageEditsBaseUrl),
    [imageEditsBaseUrl],
  );
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
    let canceled = false;
    const loadCompressionConfig = async () => {
      try {
        const res = await API.get('/api/image/config', {
          disableDuplicate: true,
          skipErrorHandler: true,
        });
        const { success, data } = res.data || {};
        if (canceled) return;
        if (success) {
          setDirectImageEditsBaseUrl(
            typeof data.image_edits_base_url === 'string'
              ? data.image_edits_base_url.trim()
              : '',
          );
        }
        if (success && data?.image_reference_compression) {
          setDirectCompressionConfig(data.image_reference_compression);
          console.info(
            '[image compression] loaded config from /api/image/config',
            {
              raw: data.image_reference_compression,
              imageEditsBaseUrl: data.image_edits_base_url || '',
            },
          );
        } else {
          console.warn(
            '[image compression] /api/image/config has no compression config',
            {
              success,
              data,
            },
          );
        }
      } catch (error) {
        if (!canceled) {
          console.error('[image compression] load /api/image/config failed', {
            error,
            message: error?.message,
            response: error?.response?.data,
          });
        }
      }
    };
    loadCompressionConfig();
    return () => {
      canceled = true;
    };
  }, []);

  useEffect(() => {
    console.info('[image compression] effective config', {
      config: referenceImageCompressionConfig,
    });
  }, [referenceImageCompressionConfig]);

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

  useEffect(() => {
    return () => {
      if (previewObjectUrlRef.current) {
        URL.revokeObjectURL(previewObjectUrlRef.current);
        previewObjectUrlRef.current = '';
      }
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

  const buildFormPayload = async () => {
    const formData = new FormData();
    const compressionSummary = await prepareReferenceImageFiles(
      referenceImages,
      autoCompressReferenceImages,
      referenceImageCompressionConfig,
    );
    formData.append('model', IMAGE_MODEL);
    formData.append('prompt', prompt.trim());
    formData.append('size', size);
    formData.append('n', String(FIXED_IMAGE_COUNT));
    formData.append('quality', quality);
    formData.append('response_format', 'url');
    formData.append(
      'auto_compress_reference_image',
      String(autoCompressReferenceImages),
    );
    compressionSummary.files.forEach((file) => {
      formData.append('image[]', file);
    });
    return { formData, compressionSummary };
  };

  const handleGenerate = async () => {
    if (!validateBeforeSubmit()) return;

    setGenerating(true);
    const recordId = createImageRecordId();
    setActiveRecordId(recordId);
    setRecordStatus('IN_PROGRESS');
    try {
      const rawKey = await fetchTokenKey(selectedTokenId);
      const endpoint = hasReferenceImages
        ? imageEditsEndpoint
        : imageGenerationsEndpoint;
      const payload = hasReferenceImages
        ? await buildFormPayload()
        : buildJsonPayload();
      if (
        hasReferenceImages &&
        payload.compressionSummary?.compressedCount > 0
      ) {
        Toast.info(
          t('已压缩 {{count}} 张参考图：{{before}} -> {{after}}', {
            count: payload.compressionSummary.compressedCount,
            before: formatFileSize(payload.compressionSummary.originalBytes),
            after: formatFileSize(payload.compressionSummary.finalBytes),
          }),
        );
      }
      console.info('[image generation] request endpoint', {
        endpoint,
        mode: hasReferenceImages ? 'edits' : 'generations',
        configuredBaseUrl: imageEditsBaseUrl || '',
        useSameOrigin: !imageEditsBaseUrl,
      });

      const res = await API.post(
        endpoint,
        hasReferenceImages ? payload.formData : payload,
        {
          headers: {
            Authorization: `Bearer sk-${rawKey}`,
            'X-Newapi-Image-Record-Id': recordId,
          },
          //timeout: 120000,
          skipErrorHandler: true,
        },
      );

      const data = res.data || {};
      const imageData = Array.isArray(data.data) ? data.data : [];
      if (imageData.length === 0) {
        Toast.warning(t('接口未返回图片数据'));
      }
      setResults((current) => [
        ...attachRecordToImages(imageData, recordId),
        ...current,
      ]);
      setResponseMeta({
        created: data.created,
        model: data.model || IMAGE_MODEL,
        size: data.size || size,
        quality: data.quality,
        background: data.background,
        output_format: data.output_format,
        usage: data.usage,
      });
      setRecordStatus('SUCCESS');
    } catch (error) {
      const message = getReadableError(error, t('生成失败'));
      setRecordStatus('IN_PROGRESS');
      const refreshTip = shouldSuggestImageRecordRefresh(error)
        ? `。${t('如果图片实际已生成，请稍后点击刷新结果查询生图记录。')}`
        : '';
      Toast.error({
        content: `${message}${refreshTip}`,
        duration: 0,
      });
    } finally {
      setGenerating(false);
    }
  };

  const applyRecordResult = (record) => {
    if (!record) return;
    setRecordStatus(record.status || '');
    const result = record.result || {};
    const imageData = Array.isArray(result.data) ? result.data : [];
    if (imageData.length > 0) {
      const recordImages = attachRecordToImages(imageData, record.record_id);
      setResults((current) => {
        const withoutSameRecord = current.filter(
          (item) => item._record_id !== record.record_id,
        );
        return [...recordImages, ...withoutSameRecord];
      });
      setResponseMeta({
        created: result.created,
        model: result.model || record.model || IMAGE_MODEL,
        size: result.size || record.size || size,
        quality: result.quality || record.quality,
        background: result.background,
        output_format: result.output_format,
        usage: result.usage,
      });
    }
  };

  const refreshActiveRecord = async () => {
    if (!activeRecordId) {
      Toast.warning(t('暂无可刷新的生图记录'));
      return;
    }
    setRecordLoading(true);
    try {
      const res = await API.get(`/api/image_records/${activeRecordId}`, {
        skipErrorHandler: true,
        disableDuplicate: true,
      });
      const { success, data, message } = res.data || {};
      if (!success) {
        Toast.error({
          content: message || t('刷新失败'),
          duration: 0,
        });
        return;
      }
      applyRecordResult(data);
      if (data?.status === 'SUCCESS') {
        Toast.success(t('生图结果已刷新'));
      } else if (data?.status === 'FAILURE') {
        if (data?.fail_reason) {
          console.warn('[image record] generation failed:', data.fail_reason);
        }
        Toast.error({
          content: t('这条生图记录最终失败，不会再产生图片，请重新生成。'),
          duration: 0,
        });
      } else {
        Toast.info(t('生图仍在处理中，请稍后再刷新'));
      }
    } catch (error) {
      Toast.error({
        content: getReadableError(error, t('刷新失败')),
        duration: 0,
      });
    } finally {
      setRecordLoading(false);
    }
  };

  const deleteImageRecord = (recordId) => {
    if (!recordId) {
      Toast.warning(t('暂无可删除的生图记录'));
      return;
    }
    Modal.confirm({
      title: t('确认删除'),
      content: t(
        '将永久删除这张图片、对应生图记录及其 R2 存储，删除后不可恢复。',
      ),
      okText: t('确认删除'),
      cancelText: t('取消'),
      okButtonProps: { type: 'danger' },
      onOk: async () => {
        setRecordDeleting(true);
        try {
          const res = await API.delete(`/api/image_records/${recordId}`, {
            skipErrorHandler: true,
          });
          const { success, message } = res.data || {};
          if (!success) {
            Toast.error({ content: message || t('删除失败'), duration: 0 });
            return;
          }
          setResults((current) =>
            current.filter((item) => item._record_id !== recordId),
          );
          if (activeRecordId === recordId) {
            setActiveRecordId('');
            setRecordStatus('');
            setResponseMeta(null);
          }
          Toast.success(t('已删除生图记录'));
        } catch (error) {
          const message = getReadableError(error, t('删除失败'));
          Toast.error({ content: message, duration: 0 });
        } finally {
          setRecordDeleting(false);
        }
      },
    });
  };

  const revokePreviewObjectUrl = () => {
    if (previewObjectUrlRef.current) {
      URL.revokeObjectURL(previewObjectUrlRef.current);
      previewObjectUrlRef.current = '';
    }
  };

  const openPreview = async (item, index) => {
    const src = buildImageUrl(item);
    if (!src) return;
    const requestId = previewRequestRef.current + 1;
    previewRequestRef.current = requestId;
    revokePreviewObjectUrl();
    setPreviewSrc(src);
    setPreviewIndex(index);
    setPreviewVisible(true);

    if (!item?._record_id) return;
    try {
      const blob = await fetchImageRecordBlob(item);
      const objectUrl = URL.createObjectURL(blob);
      if (previewRequestRef.current !== requestId) {
        URL.revokeObjectURL(objectUrl);
        return;
      }
      revokePreviewObjectUrl();
      previewObjectUrlRef.current = objectUrl;
      setPreviewSrc(objectUrl);
    } catch (error) {
      console.error('[image preview] prepare download blob failed', error);
    }
  };

  const handlePreviewVisibleChange = (visible) => {
    setPreviewVisible(visible);
    if (!visible) {
      previewRequestRef.current += 1;
      revokePreviewObjectUrl();
    }
  };

  const handlePreviewDownloadError = async () => {
    const item = results[previewIndex];
    if (!item) return;
    try {
      await downloadImage(item, previewIndex);
    } catch (error) {
      console.error('[image preview download fallback] failed', error);
      Toast.error({
        content: getReadableError(error, t('下载失败')),
        duration: 0,
      });
    }
  };

  const handleThumbnailDownload = async (item, index) => {
    const downloadKey = getImageDownloadKey(item, index);
    if (
      activeDownloadsRef.current.has(downloadKey) ||
      downloadProgressMap[downloadKey] !== undefined
    ) {
      return;
    }

    const updateDownloadProgress = (percent) => {
      setDownloadProgressMap((current) => ({
        ...current,
        [downloadKey]: normalizeDownloadPercent(percent),
      }));
    };

    activeDownloadsRef.current.add(downloadKey);
    updateDownloadProgress(0);
    try {
      await downloadImage(item, index, updateDownloadProgress);
      updateDownloadProgress(100);
      window.setTimeout(() => {
        setDownloadProgressMap((current) => {
          const next = { ...current };
          delete next[downloadKey];
          return next;
        });
        activeDownloadsRef.current.delete(downloadKey);
      }, 500);
    } catch (error) {
      activeDownloadsRef.current.delete(downloadKey);
      setDownloadProgressMap((current) => {
        const next = { ...current };
        delete next[downloadKey];
        return next;
      });
      console.error('[image download] failed', error);
      Toast.error({
        content: getReadableError(error, t('下载失败')),
        duration: 0,
      });
    }
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
          {`curl -X POST '${
            imageEditsBaseUrl
              ? buildImageGenerationsEndpoint(imageEditsBaseUrl)
              : `${serverAddress}/v1/images/generations`
          }' \\
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
          {`curl -X POST '${
            imageEditsBaseUrl
              ? buildImageEditsEndpoint(imageEditsBaseUrl)
              : `${serverAddress}/v1/images/edits`
          }' \\
  -H 'Authorization: Bearer YOUR_API_KEY' \\
  -F 'model=${IMAGE_MODEL}' \\
  -F 'prompt=a cinematic neon city in heavy rain' \\
  -F 'size=auto' \\
  -F 'n=1' \\
  -F 'quality=auto' \\
  -F 'response_format=url' \\
  -F 'auto_compress_reference_image=true' \\
  -F 'image[]=@/path/to/image1.png' \\
  -F 'image[]=@/path/to/image2.png'`}
        </pre>
      </section>
      <section>
        <Title heading={6} className='!mb-2'>
          {t('注意事项')}
        </Title>
        <ul className='text-sm text-[var(--semi-color-text-2)] list-disc pl-4 flex flex-col gap-1'>
          <li>
            {t('页面默认请求 URL 返回，后端可将 b64_json 结果转存为临时 URL。')}
          </li>
          <li>{t('品质默认为 auto，由模型自行决定合适的生成质量。')}</li>
          <li>
            {t(
              'edits 支持 auto_compress_reference_image 参数，默认为 true；页面会先按系统配置在浏览器内压缩参考图，后端也会兜底压缩，该参数不会转发给上游。',
            )}
          </li>
          <li>{t('同步生成可能耗时 10-60 秒，请耐心等待。')}</li>
        </ul>
      </section>
    </div>
  );

  return (
    <div className='mt-[60px] min-h-[calc(100vh-84px)] pb-4 lg:h-[calc(100vh-84px)] lg:min-h-[620px] lg:overflow-hidden'>
      <div className='flex flex-col gap-4 lg:h-full lg:flex-row'>
        <aside className='shrink-0 overflow-y-auto rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)] lg:h-full lg:w-[360px] xl:w-[400px]'>
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

            <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
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
              <div className='flex items-center justify-between gap-3 mb-3'>
                <div className='min-w-0'>
                  <Text type='tertiary' size='small'>
                    {t('自动压缩过大的参考图')}
                  </Text>
                  <div className='text-xs text-[var(--semi-color-text-2)] mt-1'>
                    {referenceImageCompressionConfig.enabled
                      ? t(
                          '超过 {{threshold}}MB 时压缩到约 {{target}}MB，最长边 {{maxSide}}px',
                          {
                            threshold:
                              referenceImageCompressionConfig.thresholdMb,
                            target:
                              referenceImageCompressionConfig.targetSizeMb,
                            maxSide: referenceImageCompressionConfig.maxSide,
                          },
                        )
                      : t('系统已关闭参考图自动压缩')}
                  </div>
                </div>
                <Switch
                  size='small'
                  checked={
                    referenceImageCompressionConfig.enabled &&
                    autoCompressReferenceImages
                  }
                  disabled={!referenceImageCompressionConfig.enabled}
                  onChange={setAutoCompressReferenceImages}
                />
              </div>

              <div className='grid grid-cols-3 gap-2 sm:grid-cols-4'>
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

        <main className='flex min-h-[520px] min-w-0 flex-1 flex-col overflow-hidden rounded-xl border border-[var(--semi-color-border)] bg-[var(--semi-color-bg-1)] lg:h-full'>
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
              {activeRecordId && (
                <Button
                  size='small'
                  icon={<RefreshCcw size={14} />}
                  loading={recordLoading}
                  onClick={refreshActiveRecord}
                >
                  {t('刷新结果')}
                </Button>
              )}
              {recordStatus && <Tag color='grey'>{recordStatus}</Tag>}
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
                      '如遇网络超时或 524，后端会继续处理已创建的生图记录，可稍后点击刷新结果查询。',
                    )}
                  </div>
                  <div>
                    {t(
                      '请勿生成违反法律法规及社会公序良俗的的内容，生图成功请即时保存，所有图片24小时后永久删除。',
                    )}
                  </div>
                </div>
              }
            />
            {activeRecordId && (
              <div className='mt-2 flex md:hidden items-center gap-2'>
                <Button
                  size='small'
                  icon={<RefreshCcw size={14} />}
                  loading={recordLoading}
                  onClick={refreshActiveRecord}
                >
                  {t('刷新结果')}
                </Button>
                {recordStatus && <Tag color='grey'>{recordStatus}</Tag>}
              </div>
            )}
          </div>

          <Spin
            spinning={generating}
            tip={t('正在等待图像生成结果...')}
            wrapperClassName='flex-1 min-h-[420px] lg:min-h-0'
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
                    const downloadKey = getImageDownloadKey(item, index);
                    const downloadProgress = downloadProgressMap[downloadKey];
                    const isDownloading = downloadProgress !== undefined;
                    return (
                      <div
                        key={`${src.slice(0, 48)}-${index}`}
                        className='break-inside-avoid mb-3 group relative rounded-xl overflow-hidden bg-[var(--semi-color-bg-2)] border border-[var(--semi-color-border)] cursor-zoom-in'
                        onClick={() => openPreview(item, index)}
                      >
                        <img
                          src={src}
                          alt={item.revised_prompt || prompt}
                          className='w-full block transition-transform duration-300 group-hover:scale-[1.03]'
                        />
                        <div className='absolute right-2 top-2 flex gap-1 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100'>
                          <Tooltip content={t('放大')}>
                            <Button
                              size='small'
                              type='primary'
                              className='!bg-black/60 !border-0 hover:!bg-black/80 !rounded-full'
                              icon={<ZoomInIcon />}
                              onClick={(event) => {
                                event.stopPropagation();
                                openPreview(item, index);
                              }}
                            />
                          </Tooltip>
                          <Tooltip content={t('下载')}>
                            <Button
                              size='small'
                              type='primary'
                              className='!bg-black/60 !border-0 hover:!bg-black/80 !rounded-full'
                              icon={<DownloadIcon />}
                              onClick={async (event) => {
                                event.stopPropagation();
                                await handleThumbnailDownload(item, index);
                              }}
                            />
                          </Tooltip>
                          {item._record_id && (
                            <Tooltip content={t('删除')}>
                              <Button
                                size='small'
                                type='primary'
                                className='!bg-black/60 !border-0 hover:!bg-black/80 !rounded-full'
                                icon={<Trash2 size={14} />}
                                loading={recordDeleting}
                                onClick={(event) => {
                                  event.stopPropagation();
                                  deleteImageRecord(item._record_id);
                                }}
                              />
                            </Tooltip>
                          )}
                        </div>
                        {item.revised_prompt && (
                          <div className='absolute bottom-0 left-0 right-0 bg-gradient-to-t from-black/75 to-transparent p-3 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100'>
                            <p className='text-xs text-white/90 leading-relaxed line-clamp-4'>
                              {item.revised_prompt}
                            </p>
                          </div>
                        )}
                        {isDownloading && (
                          <div
                            className='absolute inset-0 z-[9999] flex items-center justify-center bg-black/55 px-6 cursor-progress'
                            onClick={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                            }}
                            onMouseDown={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                            }}
                          >
                            <div className='w-full max-w-[220px] rounded-lg bg-black/45 p-3 shadow-lg'>
                              <div className='mb-2 text-center text-sm font-medium text-white'>
                                {t('下载')} {downloadProgress}%
                              </div>
                              <div className='h-2 overflow-hidden rounded-full bg-white/25'>
                                <div
                                  className='h-full rounded-full bg-emerald-500 transition-[width] duration-150 ease-out'
                                  style={{ width: `${downloadProgress}%` }}
                                />
                              </div>
                            </div>
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
        onVisibleChange={handlePreviewVisibleChange}
        onDownloadError={handlePreviewDownloadError}
        setDownloadName={() => getImageFilename(previewIndex)}
      />
    </div>
  );
};

export default ImageGeneration;
