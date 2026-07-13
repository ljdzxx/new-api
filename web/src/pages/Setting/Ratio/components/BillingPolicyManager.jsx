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
import {
  Banner,
  Button,
  Card,
  Input,
  Space,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess, showWarning } from '../../../../helpers';
import BillingPolicyVisualEditor from './BillingPolicyVisualEditor';

const { Text } = Typography;

export default function BillingPolicyManager({
  billingPolicyVersion = 0,
  onBillingPolicyChanged,
}) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [state, setState] = useState(null);
  const [preview, setPreview] = useState(null);
  const [query, setQuery] = useState('');
  const [editingModel, setEditingModel] = useState(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const response = await API.get('/api/option/billing_policy');
      if (!response.data?.success) throw new Error(response.data?.message);
      setState(response.data.data);
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh, billingPolicyVersion]);

  const runAction = async (action) => {
    setLoading(true);
    try {
      if (action === 'preview') {
        const response = await API.get(
          '/api/option/billing_policy/migration/preview',
        );
        setPreview(response.data?.data);
        if (!response.data?.success) throw new Error(response.data?.message);
      } else {
        const checksum = state?.config?.migration?.source_checksum || '';
        const response = await API.post(
          `/api/option/billing_policy/${action}`,
          action === 'shadow' ? {} : { source_checksum: checksum },
        );
        if (!response.data?.success) throw new Error(response.data?.message);
        showSuccess(t('操作成功'));
      }
      await refresh();
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  const rows = useMemo(() => {
    const policies = state?.config?.policies || {};
    return Object.entries(policies)
      .filter(([name]) => name.toLowerCase().includes(query.toLowerCase()))
      .map(([name, policy]) => ({ key: name, name, policy }));
  }, [state, query]);

  const savePolicy = async (policy) => {
    if (!editingModel || saving) return;
    setSaving(true);
    try {
      const response = await API.put('/api/option/billing_policy/policy', {
        model: editingModel,
        policy,
      });
      if (!response.data?.success) throw new Error(response.data?.message);
      showSuccess(t('保存成功'));
      setEditingModel(null);
      onBillingPolicyChanged?.();
      await refresh();
    } catch (error) {
      showError(error.message);
    } finally {
      setSaving(false);
    }
  };

  const config = state?.config;
  const shadow = state?.shadow;
  const preConsumeShadow = shadow?.pre_consume;
  const settlementShadow = shadow?.settlement;
  const shadowHasBlockingIssues =
    (shadow?.errors || 0) > 0 || (settlementShadow?.mismatches || 0) > 0;
  const shadowReady =
    (settlementShadow?.observations || 0) > 0 && !shadowHasBlockingIssues;
  const openPolicyEditor = (modelName) => {
    if (!config) {
      showWarning(t('计费策略正在加载，请稍后重试'));
      return;
    }
    if (config.state !== 'active') {
      showWarning(
        t('当前计费策略状态为 {{state}}，请先完成迁移并激活后再编辑', {
          state: config.state || 'legacy',
        }),
      );
      return;
    }
    if (!config.policies?.[modelName]) {
      showWarning(t('未找到对应模型的计费策略'));
      return;
    }
    setEditingModel(modelName);
  };

  const columns = useMemo(
    () => [
      {
        title: t('模型'),
        dataIndex: 'name',
        render: (_, row) => (
          <Button
            theme='borderless'
            type='tertiary'
            style={{ padding: 0 }}
            onClick={(event) => {
              event.stopPropagation();
              openPolicyEditor(row.name);
            }}
          >
            {row.name}
          </Button>
        ),
      },
      {
        title: t('模式'),
        render: (_, row) => <Tag>{row.policy.mode}</Tag>,
      },
      {
        title: t('摘要'),
        render: (_, row) =>
          row.policy.mode === 'tiered'
            ? `${row.policy.tiers?.length || 0} ${t('个阶梯')}`
            : row.policy.mode === 'per_request'
              ? `$${row.policy.price} / request`
              : `$${row.policy.prices?.input || 0} / 1M input`,
      },
      {
        title: t('操作'),
        render: (_, row) => (
          <Button
            onClick={(event) => {
              event.stopPropagation();
              openPolicyEditor(row.name);
            }}
          >
            {t('编辑')}
          </Button>
        ),
      },
    ],
    [t, config],
  );

  return (
    <div className='space-y-4'>
      <Card>
        <div className='flex flex-col justify-between gap-4 lg:flex-row lg:items-center'>
          <div>
            <h3 className='m-0 text-base font-semibold leading-6 text-semi-color-text-0'>
              {t('模型计费策略迁移')}
            </h3>
            <Space>
              <Tag color='blue'>{config?.state || 'legacy'}</Tag>
              <Text type='tertiary'>revision {config?.revision || 0}</Text>
            </Space>
          </div>
          <Space wrap>
            <Button loading={loading} onClick={() => runAction('preview')}>
              {t('迁移预检')}
            </Button>
            <Button
              theme='solid'
              loading={loading}
              disabled={!['legacy', 'shadow'].includes(config?.state)}
              onClick={() => runAction('shadow')}
            >
              {t('启动影子运行')}
            </Button>
            <Button
              loading={loading}
              disabled={config?.state !== 'shadow' || !shadowReady}
              onClick={() => runAction('prepare')}
            >
              {t('冻结并准备切换')}
            </Button>
            <Button
              type='danger'
              theme='solid'
              loading={loading}
              disabled={config?.state !== 'prepared'}
              onClick={() => runAction('activate')}
            >
              {t('原子切换')}
            </Button>
            <Button
              type='tertiary'
              loading={loading}
              disabled={!['shadow', 'prepared'].includes(config?.state)}
              onClick={() => runAction('cancel')}
            >
              {t('取消迁移')}
            </Button>
          </Space>
        </div>
        {config?.state === 'shadow' && (
          <Banner
            className='mt-4'
            type={
              shadowHasBlockingIssues
                ? 'danger'
                : preConsumeShadow?.mismatches
                  ? 'warning'
                  : 'info'
            }
            description={`${t('结算观测')} ${settlementShadow?.observations || 0} · ${t('一致')} ${settlementShadow?.matches || 0} · ${t('差异')} ${settlementShadow?.mismatches || 0} | ${t('预扣观测')} ${preConsumeShadow?.observations || 0} · ${t('一致')} ${preConsumeShadow?.matches || 0} · ${t('差异')} ${preConsumeShadow?.mismatches || 0} | ${t('计算错误')} ${shadow?.errors || 0}`}
          />
        )}
        {preview && (
          <Banner
            className='mt-4'
            type={
              preview.issues?.some((item) => item.level === 'error')
                ? 'danger'
                : 'warning'
            }
            description={`${t('候选策略')} ${preview.total} · ${t('按 Token')} ${preview.per_token} · ${t('按次')} ${preview.per_request} · ${t('问题')} ${preview.issues?.length || 0}`}
          />
        )}
      </Card>

      <Card
        title={
          <span className='text-base font-semibold'>{t('模型计费策略')}</span>
        }
      >
        <Input
          className='mb-3 max-w-md'
          value={query}
          onChange={setQuery}
          showClear
          placeholder={t('搜索模型')}
        />
        <Table
          loading={loading}
          rowKey='key'
          dataSource={rows}
          pagination={{ pageSize: 20 }}
          columns={columns}
          onRow={(row) => ({
            onClick: () => openPolicyEditor(row.name),
            style: { cursor: 'pointer' },
          })}
        />
      </Card>

      <BillingPolicyVisualEditor
        key={editingModel || 'billing-policy-editor'}
        visible={Boolean(editingModel)}
        model={editingModel}
        policy={editingModel ? config?.policies?.[editingModel] : null}
        onCancel={() => setEditingModel(null)}
        onSave={savePolicy}
        saving={saving}
      />
    </div>
  );
}
