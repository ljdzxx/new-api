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
  Modal,
  Select,
  Skeleton,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconCopy, IconSearch } from '@douyinfe/semi-icons';
import { API, copy, showError, showSuccess } from '../../helpers';

const { Title, Text, Paragraph } = Typography;

const PRICE_FIELDS = [
  ['input', '输入'],
  ['output', '输出'],
  ['cache_read', '缓存读取'],
  ['cache_write_5m', '缓存写入 5m'],
  ['cache_write_1h', '缓存写入 1h'],
  ['image_input', '图片输入'],
  ['audio_input', '音频输入'],
  ['audio_output', '音频输出'],
];

function legacyPolicy(model) {
  if (model.quota_type === 1) {
    return {
      mode: 'per_request',
      price: String(model.model_price ?? 0),
    };
  }
  const input = Number(model.model_ratio || 0) * 2;
  const prices = {
    input: String(input),
    output: String(input * Number(model.completion_ratio || 1)),
  };
  const optional = [
    ['cache_read', model.cache_ratio],
    ['cache_write_5m', model.create_cache_ratio],
    ['image_input', model.image_ratio],
    ['audio_input', model.audio_ratio],
  ];
  optional.forEach(([key, ratio]) => {
    if (ratio !== undefined && ratio !== null) {
      prices[key] = String(input * Number(ratio));
    }
  });
  if (model.audio_ratio != null && model.audio_completion_ratio != null) {
    prices.audio_output = String(
      input * Number(model.audio_ratio) * Number(model.audio_completion_ratio),
    );
  }
  return { mode: 'per_token', prices };
}

function formatPrice(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return '-';
  return `$${Number(number.toFixed(8))}`;
}

function PriceRows({ prices }) {
  return (
    <div className='grid grid-cols-2 gap-x-4 gap-y-2'>
      {PRICE_FIELDS.filter(([key]) => prices?.[key] !== undefined).map(
        ([key, label]) => (
          <React.Fragment key={key}>
            <Text type='tertiary'>{label}</Text>
            <Text strong>{formatPrice(prices[key])} / 1M tokens</Text>
          </React.Fragment>
        ),
      )}
    </div>
  );
}

function PolicySummary({ policy }) {
  if (policy.mode === 'per_request') {
    return <Text strong>{formatPrice(policy.price)} / request</Text>;
  }
  if (policy.mode === 'tiered') {
    return (
      <Space wrap>
        {(policy.tiers || []).map((tier) => (
          <Tag key={tier.id} color={tier.fallback ? 'grey' : 'blue'}>
            {tier.id}: {formatPrice(tier.prices?.input)} in /{' '}
            {formatPrice(tier.prices?.output)} out
          </Tag>
        ))}
      </Space>
    );
  }
  return <PriceRows prices={policy.prices} />;
}

export default function OriginalPricingPage() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [models, setModels] = useState([]);
  const [vendors, setVendors] = useState({});
  const [query, setQuery] = useState('');
  const [vendor, setVendor] = useState('all');
  const [selected, setSelected] = useState(null);

  useEffect(() => {
    const load = async () => {
      setLoading(true);
      try {
        const response = await API.get('/api/pricing');
        if (!response.data?.success) {
          showError(response.data?.message || t('加载模型价格失败'));
          return;
        }
        const vendorMap = {};
        (response.data.vendors || []).forEach((item) => {
          vendorMap[item.id] = item;
        });
        setVendors(vendorMap);
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
    };
    load();
  }, [t]);

  const vendorOptions = useMemo(() => {
    return [
      { label: t('全部供应商'), value: 'all' },
      ...Object.values(vendors).map((item) => ({
        label: item.name,
        value: String(item.id),
      })),
    ];
  }, [vendors, t]);

  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return models.filter((model) => {
      if (vendor !== 'all' && String(model.vendor_id) !== vendor) return false;
      if (!keyword) return true;
      return [
        model.model_name,
        model.description,
        model.tags,
        vendors[model.vendor_id]?.name,
      ]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword));
    });
  }, [models, query, vendor, vendors]);

  return (
    <div className='mx-auto w-full max-w-[1500px] px-4 py-6 md:px-8'>
      <div className='mb-6 flex flex-col justify-between gap-4 md:flex-row md:items-end'>
        <div>
          <Title heading={2}>{t('模型广场')}</Title>
          <Paragraph type='tertiary' className='mb-0'>
            {t(
              '仅展示模型原始官方定价，所有全局、用户、渠道、分组和等级倍率均按 1 倍计算。',
            )}
          </Paragraph>
        </div>
        <Space wrap>
          <Input
            prefix={<IconSearch />}
            value={query}
            onChange={setQuery}
            placeholder={t('搜索模型、标签或供应商')}
            showClear
          />
          <Select
            value={vendor}
            optionList={vendorOptions}
            onChange={setVendor}
            style={{ width: 180 }}
          />
        </Space>
      </div>

      {loading ? (
        <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
          {Array.from({ length: 9 }).map((_, index) => (
            <Card key={index}>
              <Skeleton loading active />
            </Card>
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <Empty description={t('没有符合条件的模型')} />
      ) : (
        <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-3'>
          {filtered.map((model) => (
            <Card
              key={model.model_name}
              shadows='hover'
              className='h-full cursor-pointer'
              onClick={() => setSelected(model)}
              headerLine={false}
              title={
                <div className='flex min-w-0 items-center justify-between gap-2'>
                  <Text strong ellipsis={{ showTooltip: true }}>
                    {model.model_name}
                  </Text>
                  <Tag
                    color={model.policy.mode === 'tiered' ? 'violet' : 'blue'}
                  >
                    {model.policy.mode === 'tiered'
                      ? t('阶梯')
                      : model.policy.mode === 'per_request'
                        ? t('按次')
                        : t('按 Token')}
                  </Tag>
                </div>
              }
            >
              <div className='space-y-3'>
                <Text type='tertiary'>
                  {vendors[model.vendor_id]?.name || t('未知供应商')}
                </Text>
                <PolicySummary policy={model.policy} />
                {model.description && (
                  <Paragraph
                    ellipsis={{ rows: 2, showTooltip: true }}
                    className='mb-0'
                  >
                    {model.description}
                  </Paragraph>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal
        title={selected?.model_name}
        visible={Boolean(selected)}
        onCancel={() => setSelected(null)}
        footer={null}
        width={720}
      >
        {selected && (
          <div className='space-y-5'>
            <div className='flex items-center justify-between rounded-lg border p-3'>
              <div>
                <Text type='tertiary'>{t('供应商')}</Text>
                <div>
                  <Text strong>
                    {vendors[selected.vendor_id]?.name || t('未知供应商')}
                  </Text>
                </div>
              </div>
              <Button
                icon={<IconCopy />}
                onClick={async () => {
                  if (await copy(selected.model_name))
                    showSuccess(t('模型名称已复制'));
                }}
              >
                {t('复制模型名')}
              </Button>
            </div>
            {selected.policy.mode === 'tiered' ? (
              (selected.policy.tiers || [])
                .sort((a, b) => a.priority - b.priority)
                .map((tier) => (
                  <Card
                    key={tier.id}
                    title={`${tier.id}${tier.fallback ? ` · ${t('兜底')}` : ''}`}
                    headerExtraContent={
                      <Tag>
                        {t('优先级')} {tier.priority}
                      </Tag>
                    }
                  >
                    {tier.conditions?.length > 0 && (
                      <Paragraph type='tertiary'>
                        {tier.conditions
                          .map(
                            (item) =>
                              `${item.metric} ${item.operator} ${item.value}`,
                          )
                          .join(' AND ')}
                      </Paragraph>
                    )}
                    <PriceRows prices={tier.prices} />
                  </Card>
                ))
            ) : selected.policy.mode === 'per_request' ? (
              <Title heading={4}>
                {formatPrice(selected.policy.price)} / request
              </Title>
            ) : (
              <PriceRows prices={selected.policy.prices} />
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}
