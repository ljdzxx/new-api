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
import {
  Button,
  Card,
  Empty,
  Input,
  Select,
  SideSheet,
  Skeleton,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconApps,
  IconCopy,
  IconFilter,
  IconList,
  IconRefresh,
  IconSearch,
} from '@douyinfe/semi-icons';
import {
  API,
  copy,
  getLobeHubIcon,
  showError,
  showSuccess,
  stringToColor,
} from '../../helpers';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import './OriginalPricingPage.css';

const { Title, Text, Paragraph } = Typography;
const PRICE_FIELDS = [
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
const CACHE_WRITE_FIELDS = ['cache_write', 'cache_write_5m', 'cache_write_1h'];
const METRIC_LABELS = {
  input_total_tokens: '总输入 Tokens',
  output_total_tokens: '总输出 Tokens',
};
const OP_SYMBOLS = { lt: '<', lte: '≤', gt: '>', gte: '≥', eq: '=' };
const DEFAULT_FILTERS = {
  vendor: 'all',
  billing: 'all',
  group: 'all',
  tag: 'all',
  endpoint: 'all',
};

function isAnthropicModel(modelName) {
  const normalized = String(modelName || '').toLowerCase();
  return normalized.includes('claude') || normalized.includes('anthropic/');
}

function isVisibleDetailPriceField(model, key) {
  if (key === 'cache_write' && isAnthropicModel(model?.model_name)) {
    return false;
  }
  return policyPriceValues(model?.policy, key).length > 0;
}

function legacyPolicy(model) {
  if (model.quota_type === 1) {
    return {
      mode: 'per_request',
      price: String(model.model_price ?? 0),
      adjustments: [],
    };
  }
  const input = Number(model.model_ratio || 0) * 2;
  const prices = {
    input: String(input),
    output: String(input * Number(model.completion_ratio || 1)),
  };
  [
    ['cache_read', model.cache_ratio],
    ['cache_write', model.create_cache_ratio],
    ['image_input', model.image_ratio],
    ['audio_input', model.audio_ratio],
  ].forEach(([key, ratio]) => {
    if (ratio != null) prices[key] = String(input * Number(ratio));
  });
  if (model.audio_ratio != null && model.audio_completion_ratio != null) {
    prices.audio_output = String(
      input * Number(model.audio_ratio) * Number(model.audio_completion_ratio),
    );
  }
  return { mode: 'per_token', prices, adjustments: [] };
}

function formatPrice(value) {
  const number = Number(value);
  return Number.isFinite(number) ? `$${Number(number.toFixed(8))}` : '-';
}

function compactTokens(value) {
  const number = Number(value);
  if (number >= 1000000) return `${Number((number / 1000000).toFixed(2))}M`;
  if (number >= 1000) return `${Number((number / 1000).toFixed(2))}K`;
  return String(number);
}

function parseTags(tags) {
  return String(tags || '')
    .split(/[,;|]+/)
    .map((tag) => tag.trim())
    .filter(Boolean);
}

function publicTimeAdjustments(policy) {
  return (policy.adjustments || []).filter(
    (rule) =>
      rule.conditions?.length > 0 &&
      rule.conditions.every(
        (item) => item.source === 'hour' || item.source === 'weekday',
      ),
  );
}

function parsePriceValue(value) {
  if (value === null || value === undefined || String(value).trim() === '') {
    return null;
  }
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function policyPriceValues(policy, fields) {
  const fieldList = Array.isArray(fields) ? fields : [fields];
  const pricesList =
    policy.mode === 'tiered'
      ? (policy.tiers || []).map((tier) => tier?.prices)
      : [policy.prices];
  return pricesList.flatMap((prices) =>
    fieldList
      .map((field) => parsePriceValue(prices?.[field]))
      .filter((value) => value !== null),
  );
}

function numericPrice(policy, field) {
  const values = policyPriceValues(policy, field);
  return values.length ? Math.min(...values) : Number.POSITIVE_INFINITY;
}

function priceRange(policy, fields) {
  const values = policyPriceValues(policy, fields);
  if (!values.length) return null;
  const min = Math.min(...values);
  const max = Math.max(...values);
  return min === max
    ? formatPrice(min)
    : `${formatPrice(min)} - ${formatPrice(max)}`;
}

function billingLabel(policy, t) {
  if (policy.mode === 'tiered') return t('阶梯定价');
  if (policy.mode === 'per_request') return t('按次');
  return t('按 Token');
}

function ModelLogo({ model, vendor, size = 24 }) {
  const icon = model?.icon || vendor?.icon;
  return (
    <div
      className='plaza-model-logo'
      style={{ '--plaza-logo-size': `${size + 18}px` }}
    >
      {icon ? (
        getLobeHubIcon(icon, size)
      ) : (
        <span>{model?.model_name?.slice(0, 1).toUpperCase() || '?'}</span>
      )}
    </div>
  );
}

function ModelIdentity({ model, vendor, compact = false }) {
  return (
    <div className='plaza-model-identity'>
      <ModelLogo model={model} vendor={vendor} size={compact ? 20 : 28} />
      <div className='min-w-0'>
        <div className='plaza-model-name'>{model.model_name}</div>
        <Text type='tertiary' size='small'>
          {vendor?.name || '-'}
        </Text>
      </div>
    </div>
  );
}

function PriceSummary({ policy, t, compact = false }) {
  if (policy.mode === 'per_request') {
    return (
      <div className='plaza-request-price'>
        {formatPrice(policy.price)} <span>/ {t('次')}</span>
      </div>
    );
  }
  const entries = [
    [t('输入'), priceRange(policy, 'input')],
    [t('输出'), priceRange(policy, 'output')],
    [t('缓存读'), priceRange(policy, 'cache_read')],
    [t('缓存写'), priceRange(policy, CACHE_WRITE_FIELDS)],
  ].filter(([, value]) => value);
  return (
    <div className={compact ? 'plaza-summary compact' : 'plaza-summary'}>
      {entries.map(([label, value]) => (
        <div key={label} className='plaza-summary-item'>
          <Text type='tertiary' size='small'>
            {label}
          </Text>
          <div>
            <Text strong className='font-mono'>
              {value}
            </Text>
            <Text type='tertiary' size='small'>
              {' '}
              / 1M
            </Text>
          </div>
        </div>
      ))}
    </div>
  );
}

function conditionText(condition, t) {
  return `${t(METRIC_LABELS[condition.metric] || condition.metric)} ${
    OP_SYMBOLS[condition.operator] || condition.operator
  } ${compactTokens(condition.value)}`;
}

function timeConditionText(condition, t) {
  const zone = condition.timezone || 'UTC';
  if (condition.source === 'hour') {
    return `${t('每天')} ${OP_SYMBOLS[condition.operator] || '='} ${condition.value}:00 (${zone})`;
  }
  const weekdays = [
    t('周日'),
    t('周一'),
    t('周二'),
    t('周三'),
    t('周四'),
    t('周五'),
    t('周六'),
  ];
  return `${weekdays[Number(condition.value)] || condition.value} (${zone})`;
}

function FilterSection({ title, value, options, onChange }) {
  return (
    <section className='plaza-filter-section'>
      <Text strong size='small'>
        {title}
      </Text>
      <div className='plaza-filter-options'>
        {options.map((option) => (
          <button
            key={option.value}
            type='button'
            className={`plaza-filter-chip ${value === option.value ? 'active' : ''}`}
            onClick={() => onChange(option.value)}
            title={option.label}
          >
            {option.icon && (
              <span className='plaza-filter-icon'>{option.icon}</span>
            )}
            <span className='plaza-filter-label'>{option.label}</span>
            <span className='plaza-filter-count'>{option.count}</span>
          </button>
        ))}
      </div>
    </section>
  );
}

function PricingFilters({ models, vendors, filters, setFilters, t }) {
  const count = (predicate) => models.filter(predicate).length;
  const groups = [
    ...new Set(models.flatMap((model) => model.enable_groups || [])),
  ].sort();
  const tags = [
    ...new Set(models.flatMap((model) => parseTags(model.tags))),
  ].sort();
  const endpoints = [
    ...new Set(models.flatMap((model) => model.supported_endpoint_types || [])),
  ].sort();
  const vendorOptions = [
    { value: 'all', label: t('全部供应商'), count: models.length },
    ...Object.values(vendors)
      .map((item) => ({
        value: String(item.id),
        label: item.name,
        count: count((model) => model.vendor_id === item.id),
        icon: item.icon ? getLobeHubIcon(item.icon, 14) : null,
      }))
      .filter((item) => item.count > 0),
  ];
  const billingOptions = [
    { value: 'all', label: t('全部类型'), count: models.length },
    {
      value: 'per_token',
      label: t('按 Token'),
      count: count((model) => model.policy.mode === 'per_token'),
    },
    {
      value: 'per_request',
      label: t('按次'),
      count: count((model) => model.policy.mode === 'per_request'),
    },
    {
      value: 'tiered',
      label: t('阶梯定价'),
      count: count((model) => model.policy.mode === 'tiered'),
    },
    {
      value: 'dynamic',
      label: t('闲忙时价格'),
      count: count((model) => publicTimeAdjustments(model.policy).length > 0),
    },
  ].filter((item) => item.value === 'all' || item.count > 0);
  const makeOptions = (allLabel, values, matcher) => [
    { value: 'all', label: allLabel, count: models.length },
    ...values.map((item) => ({
      value: item,
      label: item,
      count: count((model) => matcher(model, item)),
    })),
  ];
  const update = (key) => (value) =>
    setFilters((current) => ({ ...current, [key]: value }));

  return (
    <Card
      className='plaza-filter-panel'
      headerLine={false}
      title={<Text strong>{t('筛选')}</Text>}
      headerExtraContent={
        <Button
          type='tertiary'
          theme='borderless'
          size='small'
          icon={<IconRefresh />}
          onClick={() => setFilters(DEFAULT_FILTERS)}
        >
          {t('重置')}
        </Button>
      }
    >
      <FilterSection
        title={t('供应商')}
        value={filters.vendor}
        options={vendorOptions}
        onChange={update('vendor')}
      />
      <FilterSection
        title={t('计费类型')}
        value={filters.billing}
        options={billingOptions}
        onChange={update('billing')}
      />
      {groups.length > 0 && (
        <FilterSection
          title={t('分组')}
          value={filters.group}
          options={makeOptions(t('全部分组'), groups, (model, item) =>
            model.enable_groups?.includes(item),
          )}
          onChange={update('group')}
        />
      )}
      {tags.length > 0 && (
        <FilterSection
          title={t('标签')}
          value={filters.tag}
          options={makeOptions(t('全部标签'), tags, (model, item) =>
            parseTags(model.tags).includes(item),
          )}
          onChange={update('tag')}
        />
      )}
      {endpoints.length > 0 && (
        <FilterSection
          title={t('端点类型')}
          value={filters.endpoint}
          options={makeOptions(t('全部端点'), endpoints, (model, item) =>
            model.supported_endpoint_types?.includes(item),
          )}
          onChange={update('endpoint')}
        />
      )}
    </Card>
  );
}

function PricingCard({ model, vendor, onOpen, t }) {
  const policy = model.policy;
  const timeRules = publicTimeAdjustments(policy);
  const labels = [
    ...(model.supported_endpoint_types || []).slice(0, 2),
    ...parseTags(model.tags).slice(0, 2),
  ];
  return (
    <Card
      shadows='hover'
      headerLine={false}
      className='plaza-model-card h-full cursor-pointer'
      onClick={onOpen}
      title={
        <div className='flex min-w-0 items-start justify-between gap-2'>
          <ModelIdentity model={model} vendor={vendor} />
          <div className='flex flex-col items-end gap-1 flex-shrink-0'>
            <Tag color={policy.mode === 'tiered' ? 'violet' : 'blue'}>
              {billingLabel(policy, t)}
            </Tag>
            {timeRules.length > 0 && <Tag color='amber'>{t('闲忙时价格')}</Tag>}
          </div>
        </div>
      }
    >
      <div className='space-y-3'>
        <PriceSummary policy={policy} t={t} />
        {model.description && (
          <Paragraph
            type='tertiary'
            ellipsis={{ rows: 2, showTooltip: true }}
            className='mb-0'
          >
            {model.description}
          </Paragraph>
        )}
        {labels.length > 0 && (
          <Space wrap>
            {labels.map((item) => (
              <Tag
                key={item}
                shape='circle'
                color={stringToColor(item)}
                size='small'
              >
                {item}
              </Tag>
            ))}
          </Space>
        )}
      </div>
    </Card>
  );
}

const SHEET_MIN_WIDTH = 560;
const SHEET_CONTENT_PADDING = 56; /* .plaza-detail-content horizontal padding: 28px * 2 */

/* 阶梯表固有宽度 = 阶梯(160) + 优先级(90) + 命中条件(240) + 每个价格字段 130 */
function tierTableWidth(fieldCount) {
  return Math.max(720, 160 + 90 + 240 + fieldCount * 130);
}

/* 方案 A：根据模型定价数据确定性地推算抽屉的理想宽度（无需 DOM 测量）。 */
function computeIdealWidth(model) {
  const viewportMax = window.innerWidth - 24;
  if (!model) return Math.min(820, viewportMax);
  const policy = model.policy;
  const fieldCount = PRICE_FIELDS.filter(([key]) =>
    isVisibleDetailPriceField(model, key),
  ).length;
  let ideal;
  if (policy.mode === 'per_request') {
    /* 按次计费内容极窄，最小宽度即可 */
    ideal = SHEET_MIN_WIDTH;
  } else if (policy.mode === 'tiered') {
    /* 与阶梯表的固有宽度对齐（同下方 Table 的 scroll.x） */
    ideal = tierTableWidth(fieldCount) + SHEET_CONTENT_PADDING;
  } else {
    /* 按 Token：价格网格每项约 220px，单行最多 4 项 */
    ideal = Math.min(fieldCount, 4) * 220 + SHEET_CONTENT_PADDING;
  }
  return Math.max(SHEET_MIN_WIDTH, Math.min(ideal, viewportMax));
}

function ModelDetails({ model, vendor, visible, onClose, t }) {
  const isMobile = useIsMobile();
  const [width, setWidth] = useState(SHEET_MIN_WIDTH);
  const [resizing, setResizing] = useState(false);

  /* 每次打开新模型时按内容重算宽度；拖拽只在当前抽屉生命周期内生效 */
  useEffect(() => {
    if (model) setWidth(computeIdealWidth(model));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [model?.model_name]);

  useEffect(() => {
    const clampWidth = () =>
      setWidth((current) => Math.min(current, window.innerWidth - 24));
    window.addEventListener('resize', clampWidth);
    return () => window.removeEventListener('resize', clampWidth);
  }, []);

  useEffect(() => {
    if (!resizing) return undefined;
    /* 拖拽不设上限（自然受视口宽度约束），仅保留最小宽度 */
    const onMove = (event) => {
      setWidth(Math.max(SHEET_MIN_WIDTH, window.innerWidth - event.clientX));
    };
    const onEnd = () => setResizing(false);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onEnd);
    return () => {
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onEnd);
    };
  }, [resizing]);

  if (!model) {
    return (
      <SideSheet
        className='plaza-detail-sheet'
        title={null}
        visible={false}
        onCancel={onClose}
        width={isMobile ? '100%' : width}
        bodyStyle={{ padding: 0, overflow: 'hidden', position: 'relative' }}
      />
    );
  }
  const policy = model.policy;
  const timeRules = publicTimeAdjustments(policy);
  const visibleFields = PRICE_FIELDS.filter(([key]) =>
    isVisibleDetailPriceField(model, key),
  );
  const tiers = (policy.tiers || [])
    .filter(Boolean)
    .sort((a, b) => Number(a.priority || 0) - Number(b.priority || 0));
  const resizeByKeyboard = (event) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const delta = event.key === 'ArrowLeft' ? 24 : -24;
    /* 与拖拽一致：不设上限，仅保留最小宽度 */
    setWidth((current) => Math.max(SHEET_MIN_WIDTH, current + delta));
  };

  return (
    <SideSheet
      className={`plaza-detail-sheet ${resizing ? 'is-resizing' : ''}`}
      title={<ModelIdentity model={model} vendor={vendor} />}
      visible={visible}
      onCancel={onClose}
      width={isMobile ? '100%' : width}
      bodyStyle={{ padding: 0, overflow: 'hidden', position: 'relative' }}
    >
      {!isMobile && (
        <div
          className='plaza-resize-handle'
          role='separator'
          aria-orientation='vertical'
          aria-label={t('拖动调整详情宽度')}
          tabIndex={0}
          onPointerDown={(event) => {
            event.preventDefault();
            setResizing(true);
          }}
          onKeyDown={resizeByKeyboard}
        >
          <span />
        </div>
      )}
      <div className='plaza-detail-content'>
        <div className='plaza-detail-intro'>
          <div>
            <Text type='tertiary'>{vendor?.name || t('未知供应商')}</Text>
            <Paragraph className='plaza-detail-description'>
              {model.description || t('暂无模型描述')}
            </Paragraph>
          </div>
          <Button
            icon={<IconCopy />}
            onClick={async () => {
              if (await copy(model.model_name))
                showSuccess(t('模型名称已复制'));
            }}
          >
            {t('复制模型名')}
          </Button>
        </div>

        <section className='plaza-detail-section'>
          <Title heading={4}>{t('原始定价')}</Title>
          <Paragraph type='tertiary'>
            {t('这里展示的是倍率为x1的情况下的原始定价。')}
          </Paragraph>
          {policy.mode === 'per_request' ? (
            <div className='plaza-detail-request'>
              <PriceSummary policy={policy} t={t} />
            </div>
          ) : policy.mode === 'tiered' ? (
            <div className='plaza-tier-block'>
              <div className='plaza-detail-subheading'>
                <Title heading={5}>{t('阶梯价格表')}</Title>
                <Paragraph type='tertiary'>
                  {t(
                    '优先级数字越小越优先；第一个满足全部条件的阶梯生效，其他阶梯均不匹配时使用兜底阶梯。',
                  )}
                </Paragraph>
              </div>
              <Table
                className='plaza-tier-table'
                pagination={false}
                dataSource={tiers}
                rowKey='id'
                scroll={{ x: tierTableWidth(visibleFields.length) }}
                columns={[
                  {
                    title: t('阶梯'),
                    dataIndex: 'id',
                    width: 160,
                    fixed: 'left',
                    render: (value, row) => (
                      <div className='plaza-tier-name'>
                        <Text strong>{value}</Text>
                        {row.fallback && <Tag size='small'>{t('兜底')}</Tag>}
                      </div>
                    ),
                  },
                  { title: t('优先级'), dataIndex: 'priority', width: 90 },
                  {
                    title: t('命中条件'),
                    width: 240,
                    render: (_, row) => (
                      <div className='plaza-condition-text'>
                        {row.fallback
                          ? t('其他阶梯均未匹配')
                          : (row.conditions || [])
                              .filter(Boolean)
                              .map((item) => conditionText(item, t))
                              .join(` ${t('并且')} `)}
                      </div>
                    ),
                  },
                  ...visibleFields.map(([key, label]) => ({
                    title: t(label),
                    width: 130,
                    render: (_, row) => {
                      const value = parsePriceValue(row.prices?.[key]);
                      return value === null
                        ? '-'
                        : `${formatPrice(value)} / 1M`;
                    },
                  })),
                ]}
              />
            </div>
          ) : (
            <div className='plaza-detail-price-grid'>
              {visibleFields.map(([key, label]) => (
                <div key={key} className='plaza-detail-price-item'>
                  <Text type='tertiary' size='small'>
                    {t(label)}
                  </Text>
                  <div>
                    <Text strong className='font-mono'>
                      {formatPrice(policy.prices[key])}
                    </Text>
                    <Text type='tertiary' size='small'>
                      {' '}
                      / 1M tokens
                    </Text>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>

        {timeRules.length > 0 && (
          <section className='plaza-detail-section'>
            <Title heading={4}>{t('闲时与忙时')}</Title>
            <Paragraph type='tertiary'>
              {t('这里只展示按小时或星期生效的公开调价规则。')}
            </Paragraph>
            <div className='plaza-time-rules'>
              {timeRules.map((rule) => (
                <div key={rule.id} className='plaza-time-rule'>
                  <div className='plaza-time-rule-heading'>
                    <Text strong>{rule.id}</Text>
                    <Tag
                      color={Number(rule.multiplier) < 1 ? 'green' : 'orange'}
                    >
                      {rule.multiplier}x
                    </Tag>
                  </div>
                  <div className='plaza-time-rule-conditions'>
                    {rule.conditions.map((item, index) => (
                      <Text key={index}>{timeConditionText(item, t)}</Text>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    </SideSheet>
  );
}

export default function OriginalPricingPage() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [models, setModels] = useState([]);
  const [vendors, setVendors] = useState({});
  const [query, setQuery] = useState('');
  const [filters, setFilters] = useState(DEFAULT_FILTERS);
  const [view, setView] = useState('card');
  const [sort, setSort] = useState('name');
  const [selectedModelName, setSelectedModelName] = useState('');
  const [mobileFiltersVisible, setMobileFiltersVisible] = useState(false);

  useEffect(() => {
    (async () => {
      setLoading(true);
      try {
        const response = await API.get('/api/pricing');
        if (!response.data?.success) {
          throw new Error(response.data?.message || t('加载模型价格失败'));
        }
        const map = {};
        (response.data.vendors || []).forEach((item) => {
          map[item.id] = item;
        });
        setVendors(map);
        setModels(
          (response.data.data || []).map((model) => ({
            ...model,
            policy: model.billing_policy || legacyPolicy(model),
          })),
        );
      } catch (error) {
        showError(error.message);
      } finally {
        setLoading(false);
      }
    })();
  }, [t]);

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    const result = models.filter((model) => {
      if (
        filters.vendor !== 'all' &&
        String(model.vendor_id) !== filters.vendor
      )
        return false;
      if (filters.billing !== 'all') {
        const dynamic = publicTimeAdjustments(model.policy).length > 0;
        if (
          filters.billing === 'dynamic'
            ? !dynamic
            : filters.billing !== model.policy.mode
        ) {
          return false;
        }
      }
      if (
        filters.group !== 'all' &&
        !model.enable_groups?.includes(filters.group)
      )
        return false;
      if (filters.tag !== 'all' && !parseTags(model.tags).includes(filters.tag))
        return false;
      if (
        filters.endpoint !== 'all' &&
        !model.supported_endpoint_types?.includes(filters.endpoint)
      ) {
        return false;
      }
      return (
        !keyword ||
        [
          model.model_name,
          model.description,
          model.tags,
          vendors[model.vendor_id]?.name,
        ]
          .filter(Boolean)
          .some((value) => String(value).toLowerCase().includes(keyword))
      );
    });
    return result.sort((a, b) =>
      sort === 'input'
        ? numericPrice(a.policy, 'input') - numericPrice(b.policy, 'input')
        : a.model_name.localeCompare(b.model_name),
    );
  }, [models, query, filters, sort, vendors]);
  const selectedModel = useMemo(
    () =>
      models.find((model) => model.model_name === selectedModelName) || null,
    [models, selectedModelName],
  );

  const filterPanel = (
    <PricingFilters
      models={models}
      vendors={vendors}
      filters={filters}
      setFilters={setFilters}
      t={t}
    />
  );

  return (
    <div className='plaza-page'>
      <div className='plaza-page-inner'>
        <header className='plaza-page-header'>
          <div>
            <Title heading={2}>{t('模型广场')}</Title>
            <Paragraph type='tertiary'>
              {t('发现并比较可用模型的原始定价、能力与支持接口。')}
            </Paragraph>
          </div>
          <Input
            size='large'
            prefix={<IconSearch />}
            value={query}
            onChange={setQuery}
            showClear
            placeholder={t('搜索模型名称、供应商或标签')}
            className='plaza-search'
          />
        </header>

        <div className='plaza-page-grid'>
          <aside className='plaza-sidebar'>{filterPanel}</aside>
          <main className='plaza-main'>
            <div className='plaza-toolbar'>
              <Text type='tertiary'>
                {t('显示 {{filtered}} / {{total}} 个模型', {
                  filtered: filtered.length,
                  total: models.length,
                })}
              </Text>
              <Space wrap>
                <Button
                  className='plaza-mobile-filter-button'
                  icon={<IconFilter />}
                  onClick={() => setMobileFiltersVisible(true)}
                >
                  {t('筛选')}
                </Button>
                <Select
                  value={sort}
                  onChange={setSort}
                  optionList={[
                    { value: 'name', label: t('按名称排序') },
                    { value: 'input', label: t('按输入价格排序') },
                  ]}
                />
                <Button
                  aria-label={t('卡片视图')}
                  icon={<IconApps />}
                  theme={view === 'card' ? 'solid' : 'light'}
                  onClick={() => setView('card')}
                />
                <Button
                  aria-label={t('列表视图')}
                  icon={<IconList />}
                  theme={view === 'table' ? 'solid' : 'light'}
                  onClick={() => setView('table')}
                />
              </Space>
            </div>

            {loading ? (
              <div className='plaza-card-grid'>
                {Array.from({ length: 9 }).map((_, index) => (
                  <Card key={index} className='h-full'>
                    <Skeleton loading active />
                  </Card>
                ))}
              </div>
            ) : filtered.length === 0 ? (
              <div className='plaza-empty'>
                <Empty description={t('没有符合条件的模型')} />
              </div>
            ) : view === 'card' ? (
              <div className='plaza-card-grid'>
                {filtered.map((model) => (
                  <PricingCard
                    key={model.model_name}
                    model={model}
                    vendor={vendors[model.vendor_id]}
                    onOpen={() => setSelectedModelName(model.model_name)}
                    t={t}
                  />
                ))}
              </div>
            ) : (
              <Table
                className='plaza-model-table'
                rowKey='model_name'
                dataSource={filtered}
                pagination={{ pageSize: 20 }}
                columns={[
                  {
                    title: t('模型'),
                    dataIndex: 'model_name',
                    render: (_, row) => (
                      <ModelIdentity
                        model={row}
                        vendor={vendors[row.vendor_id]}
                        compact
                      />
                    ),
                  },
                  {
                    title: t('原始价格'),
                    render: (_, row) => (
                      <PriceSummary policy={row.policy} t={t} compact />
                    ),
                  },
                  {
                    title: t('类型'),
                    width: 190,
                    render: (_, row) => (
                      <Space wrap>
                        <Tag>{billingLabel(row.policy, t)}</Tag>
                        {publicTimeAdjustments(row.policy).length > 0 && (
                          <Tag color='amber'>{t('闲忙时')}</Tag>
                        )}
                      </Space>
                    ),
                  },
                  {
                    title: '',
                    width: 90,
                    render: (_, row) => (
                      <Button
                        theme='borderless'
                        onClick={() => setSelectedModelName(row.model_name)}
                      >
                        {t('详情')}
                      </Button>
                    ),
                  },
                ]}
              />
            )}
          </main>
        </div>
      </div>

      <SideSheet
        title={t('筛选')}
        visible={mobileFiltersVisible}
        onCancel={() => setMobileFiltersVisible(false)}
        width='min(380px, 100vw)'
        bodyStyle={{ padding: 12 }}
      >
        {filterPanel}
      </SideSheet>
      <ModelDetails
        model={selectedModel}
        vendor={selectedModel ? vendors[selectedModel.vendor_id] : null}
        visible={Boolean(selectedModel)}
        onClose={() => setSelectedModelName('')}
        t={t}
      />
    </div>
  );
}
