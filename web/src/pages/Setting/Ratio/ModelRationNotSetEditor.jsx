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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Empty, Input, Table, Tag, Typography } from '@douyinfe/semi-ui';
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

export default function ModelRatioNotSetEditor({ onBillingPolicyChanged }) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [models, setModels] = useState([]);
  const [config, setConfig] = useState(null);
  const [query, setQuery] = useState('');
  const [editingModel, setEditingModel] = useState(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [modelsResponse, policyResponse] = await Promise.all([
        API.get('/api/channel/models_enabled'),
        API.get('/api/option/billing_policy'),
      ]);
      if (!modelsResponse.data?.success) throw new Error(modelsResponse.data?.message);
      if (!policyResponse.data?.success) throw new Error(policyResponse.data?.message);
      setModels(modelsResponse.data.data || []);
      setConfig(policyResponse.data.data?.config || null);
    } catch (error) {
      showError(error.message || t('加载未设置价格模型失败'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const unpricedModels = useMemo(() => {
    const policies = config?.policies || {};
    const keyword = query.trim().toLowerCase();
    return models
      .filter((name) => !policies[name])
      .filter((name) => !keyword || name.toLowerCase().includes(keyword));
  }, [config, models, query]);

  const savePolicy = async (policy) => {
    if (!editingModel) return;
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
          {t('这里列出已启用但尚未配置新版模型计费策略的模型。设置后会自动从列表移出。')}
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
          { title: t('模型名称'), dataIndex: 'name', render: (name) => <Text strong className='font-mono'>{name}</Text> },
          { title: t('状态'), render: () => <Tag color='orange'>{t('未设置价格')}</Tag> },
          { title: t('操作'), render: (_, record) => <Button theme='solid' onClick={() => setEditingModel(record.name)}>{t('设置价格')}</Button> },
        ]}
      />
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
