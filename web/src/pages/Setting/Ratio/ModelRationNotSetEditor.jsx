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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Empty,
  Input,
  Modal,
  Radio,
  Table,
  Space,
  Tooltip,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconSearch } from '@douyinfe/semi-icons';
import { API, showError, showSuccess } from '../../../helpers';
import BillingPolicyVisualEditor from './components/BillingPolicyVisualEditor';

const { Text, Paragraph } = Typography;

function newPolicy() {
  return {
    version: 1,
    mode: 'per_token',
    currency: 'USD',
    unit: 'per_million_tokens',
    prices: { input: '', output: '' },
    adjustments: [],
  };
}

export default function ModelRatioNotSetEditor({
  billingPolicyVersion,
  onBillingPolicyChanged,
}) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const refreshSequence = useRef(0);
  const [saving, setSaving] = useState(false);
  const [syncingModel, setSyncingModel] = useState(null);
  const [legacyModels, setLegacyModels] = useState({});
  const [models, setModels] = useState([]);
  const [config, setConfig] = useState(null);
  const [query, setQuery] = useState('');
  const [editingModel, setEditingModel] = useState(null);
  const [syncTarget, setSyncTarget] = useState(null);
  const [sourceQuery, setSourceQuery] = useState('');
  const [searchedQuery, setSearchedQuery] = useState('');
  const [selectedSource, setSelectedSource] = useState(null);

  const refresh = useCallback(async () => {
    const sequence = ++refreshSequence.current;
    setLoading(true);
    try {
      const modelsResponse = await API.get('/api/channel/models_enabled');
      if (!modelsResponse.data?.success)
        throw new Error(modelsResponse.data?.message);
      const enabledModels = modelsResponse.data.data || [];
      const previewResponse = await API.post(
        '/api/option/billing_policy/legacy/preview',
        {
          models: enabledModels,
        },
      );
      if (!previewResponse.data?.success)
        throw new Error(previewResponse.data?.message);
      if (sequence !== refreshSequence.current) return;
      setModels(enabledModels);
      setConfig(previewResponse.data.data?.config || null);
      setLegacyModels(previewResponse.data.data?.models || {});
    } catch (error) {
      if (sequence === refreshSequence.current)
        showError(error.message || t('加载未设置价格模型失败'));
    } finally {
      if (sequence === refreshSequence.current) setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    refresh();
  }, [refresh, billingPolicyVersion]);

  const unpricedModels = useMemo(() => {
    const keyword = query.trim().toLowerCase();
    return models
      .filter((name) => legacyModels[name]?.configured === false)
      .filter((name) => !keyword || name.toLowerCase().includes(keyword));
  }, [legacyModels, models, query]);

  const sourceModels = useMemo(() => {
    if (!searchedQuery) return [];
    return Object.keys(config?.policies || {})
      .filter(
        (name) =>
          name !== syncTarget && name.toLowerCase().includes(searchedQuery),
      )
      .sort((a, b) => a.localeCompare(b))
      .map((name) => ({ name }));
  }, [config, searchedQuery, syncTarget]);

  const openModelSync = (name) => {
    setSourceQuery('');
    setSearchedQuery('');
    setSelectedSource(null);
    setSyncTarget(name);
  };

  const syncSelectedPrice = async () => {
    if (
      !syncTarget ||
      !selectedSource ||
      syncingModel ||
      saving ||
      loading ||
      config?.state !== 'active' ||
      !sourceModels.some(({ name }) => name === selectedSource)
    )
      return;
    setSaving(true);
    try {
      // Preserve the full policy, including tiers and tool prices. The revision
      // check prevents this snapshot from overwriting concurrent pricing edits.
      const response = await API.put('/api/option/billing_policy', {
        ...config,
        policies: {
          ...config.policies,
          [syncTarget]: config.policies[selectedSource],
        },
      });
      if (!response.data?.success) throw new Error(response.data?.message);
      showSuccess(t('模型价格同步成功'));
      setSyncTarget(null);
      onBillingPolicyChanged?.();
      await refresh();
    } catch (error) {
      showError(
        error.response?.data?.message || error.message || t('同步模型价格失败'),
      );
      setSelectedSource(null);
      await refresh();
    } finally {
      setSaving(false);
    }
  };

  const syncLegacyPrice = async (name) => {
    if (syncingModel || saving || config?.state !== 'active') return;
    setSyncingModel(name);
    try {
      const response = await API.post(
        '/api/option/billing_policy/sync_legacy',
        {
          model: name,
          revision: config.revision,
        },
      );
      if (!response.data?.success) throw new Error(response.data?.message);
      showSuccess(t('已同步旧版价格'));
      onBillingPolicyChanged?.();
      await refresh();
    } catch (error) {
      showError(
        error.response?.data?.message || error.message || t('同步旧版价格失败'),
      );
      await refresh();
    } finally {
      setSyncingModel(null);
    }
  };

  const savePolicy = async (policy) => {
    if (!editingModel) return;
    // The backend deliberately rejects policy writes until the migration has
    // been activated.  Guard here as well so this page does not issue a
    // request that can only result in a 409 on fresh/legacy installations.
    const canBootstrap =
      config?.state === 'legacy' &&
      config?.migration?.version === 0 &&
      Object.keys(config?.policies || {}).length === 0;
    if (config?.state !== 'active' && !canBootstrap) {
      showError(
        t('当前计费策略状态为 {{state}}，请先完成迁移并激活后再设置价格', {
          state: config?.state || 'legacy',
        }),
      );
      return;
    }
    setSaving(true);
    try {
      const response = await API.put('/api/option/billing_policy/policy', {
        model: editingModel,
        policy,
      });
      if (!response.data?.success) throw new Error(response.data?.message);
      showSuccess(t('模型价格策略已保存'));
      setEditingModel(null);
      onBillingPolicyChanged?.();
      await refresh();
    } catch (error) {
      showError(error.message || t('保存失败，请重试'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className='space-y-4'>
      <div className='rounded-xl border bg-semi-color-fill-0 p-4'>
        <Text strong>{t('未设置价格模型')}</Text>
        <Paragraph type='tertiary' className='mb-0 mt-1'>
          {t(
            '这里列出已启用但尚未配置新版模型计费策略的模型。设置后会自动从列表移出。',
          )}
        </Paragraph>
      </div>
      <Input
        prefix={<IconSearch />}
        value={query}
        onChange={setQuery}
        showClear
        placeholder={t('搜索模型名称')}
        className='max-w-md'
      />
      <Table
        loading={loading}
        dataSource={unpricedModels.map((name) => ({ name }))}
        rowKey='name'
        pagination={{ pageSize: 20 }}
        empty={<Empty description={t('当前没有未设置定价的模型')} />}
        columns={[
          {
            title: t('模型名称'),
            dataIndex: 'name',
            render: (name) => (
              <Text strong className='font-mono'>
                {name}
              </Text>
            ),
          },
          {
            title: t('状态'),
            render: () => <Tag color='orange'>{t('未设置价格')}</Tag>,
          },
          {
            title: t('操作'),
            render: (_, record) => {
              const unavailable = !legacyModels[record.name]?.available;
              const reason =
                config?.state !== 'active'
                  ? t('请先完成迁移并激活新版计费后再同步')
                  : unavailable
                    ? t('旧版价格缺失或无效，请手动设置价格')
                    : t('将旧版价格转换并保存为新版计费策略');
              return (
                <Space>
                  <Button
                    theme='solid'
                    disabled={Boolean(syncingModel) || saving || loading}
                    onClick={() => setEditingModel(record.name)}
                  >
                    {t('设置价格')}
                  </Button>
                  <Tooltip content={reason}>
                    <span>
                      <Button
                        loading={syncingModel === record.name}
                        disabled={
                          config?.state !== 'active' ||
                          unavailable ||
                          Boolean(syncingModel) ||
                          saving ||
                          loading ||
                          Boolean(editingModel)
                        }
                        onClick={() => syncLegacyPrice(record.name)}
                      >
                        {t('同步旧版')}
                      </Button>
                    </span>
                  </Tooltip>
                  <Tooltip
                    content={
                      config?.state !== 'active'
                        ? t('请先完成迁移并激活新版计费后再同步')
                        : t('将所选模型的完整定价配置复制至本模型')
                    }
                  >
                    <span>
                      <Button
                        disabled={
                          config?.state !== 'active' ||
                          Boolean(syncingModel) ||
                          saving ||
                          loading ||
                          Boolean(editingModel)
                        }
                        onClick={() => openModelSync(record.name)}
                      >
                        {t('同步指定模型')}
                      </Button>
                    </span>
                  </Tooltip>
                </Space>
              );
            },
          },
        ]}
      />
      <Modal
        title={t('同步指定模型')}
        visible={Boolean(syncTarget)}
        onCancel={() => {
          if (!saving) setSyncTarget(null);
        }}
        onOk={syncSelectedPrice}
        okText={t('确认同步')}
        cancelText={t('取消')}
        confirmLoading={saving}
        okButtonProps={{
          disabled:
            !selectedSource ||
            !sourceModels.some(({ name }) => name === selectedSource) ||
            loading ||
            saving ||
            config?.state !== 'active',
        }}
        cancelButtonProps={{ disabled: saving }}
        closable={!saving}
        maskClosable={false}
        width={640}
        style={{ maxWidth: 'calc(100vw - 32px)' }}
      >
        <Paragraph>
          {t('将所选模型的完整定价配置复制至本模型')}：
          <Text strong className='font-mono'>
            {syncTarget}
          </Text>
        </Paragraph>
        <div className='mb-4 flex gap-2'>
          <Input
            prefix={<IconSearch />}
            value={sourceQuery}
            disabled={saving}
            onChange={(value) => {
              setSourceQuery(value);
              setSearchedQuery('');
              setSelectedSource(null);
            }}
            showClear
            placeholder={t('请输入模型名称后点击查询')}
          />
          <Button
            disabled={!sourceQuery.trim() || saving || loading}
            onClick={() => {
              setSearchedQuery(sourceQuery.trim().toLowerCase());
              setSelectedSource(null);
            }}
          >
            {t('查询')}
          </Button>
        </div>
        <Table
          key={searchedQuery}
          dataSource={sourceModels}
          rowKey='name'
          pagination={{ pageSize: 10 }}
          empty={
            <Empty
              description={
                searchedQuery
                  ? t('未找到匹配的已定价模型')
                  : t('请输入模型名称后点击查询')
              }
            />
          }
          columns={[
            {
              title: t('选择'),
              width: 64,
              render: (_, record) => (
                <Radio
                  aria-label={record.name}
                  checked={selectedSource === record.name}
                  disabled={saving || loading}
                  onChange={() => setSelectedSource(record.name)}
                />
              ),
            },
            {
              title: t('模型名称'),
              dataIndex: 'name',
              render: (name) => <Text className='font-mono'>{name}</Text>,
            },
          ]}
        />
      </Modal>
      <BillingPolicyVisualEditor
        visible={Boolean(editingModel)}
        model={editingModel}
        policy={newPolicy()}
        onCancel={() => setEditingModel(null)}
        onSave={savePolicy}
        saving={saving}
      />
    </div>
  );
}
