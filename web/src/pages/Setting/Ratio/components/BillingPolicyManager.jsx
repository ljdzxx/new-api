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
  Modal,
  Space,
  Table,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../../helpers';

const { Text, Title } = Typography;

export default function BillingPolicyManager() {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [state, setState] = useState(null);
  const [preview, setPreview] = useState(null);
  const [query, setQuery] = useState('');
  const [editingModel, setEditingModel] = useState(null);
  const [draft, setDraft] = useState('');

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
  }, [refresh]);

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

  const savePolicy = async () => {
    try {
      const policy = JSON.parse(draft);
      const config = structuredClone(state.config);
      config.policies[editingModel] = policy;
      const response = await API.put('/api/option/billing_policy', config);
      if (!response.data?.success) throw new Error(response.data?.message);
      showSuccess(t('保存成功'));
      setEditingModel(null);
      await refresh();
    } catch (error) {
      showError(error.message);
    }
  };

  const config = state?.config;
  const shadow = state?.shadow;
  return (
    <div className='space-y-4'>
      <Card>
        <div className='flex flex-col justify-between gap-4 lg:flex-row lg:items-center'>
          <div>
            <Title heading={5}>{t('模型计费策略迁移')}</Title>
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
              disabled={
                config?.state !== 'shadow' ||
                !shadow?.observations ||
                shadow?.mismatches > 0
              }
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
            type={shadow?.mismatches ? 'danger' : 'info'}
            description={`${t('影子观测')} ${shadow?.observations || 0} · ${t('一致')} ${shadow?.matches || 0} · ${t('差异')} ${shadow?.mismatches || 0}`}
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

      <Card title={t('模型计费策略')}>
        <Input
          className='mb-3 max-w-md'
          value={query}
          onChange={setQuery}
          showClear
          placeholder={t('搜索模型')}
        />
        <Table
          loading={loading}
          dataSource={rows}
          pagination={{ pageSize: 20 }}
          columns={[
            { title: t('模型'), dataIndex: 'name' },
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
                  disabled={config?.state !== 'active'}
                  onClick={() => {
                    setEditingModel(row.name);
                    setDraft(JSON.stringify(row.policy, null, 2));
                  }}
                >
                  {t('编辑')}
                </Button>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        title={`${t('编辑模型计费策略')} · ${editingModel || ''}`}
        visible={Boolean(editingModel)}
        onCancel={() => setEditingModel(null)}
        onOk={savePolicy}
        width={800}
      >
        <TextArea
          value={draft}
          onChange={setDraft}
          autosize={{ minRows: 18, maxRows: 32 }}
          style={{ fontFamily: 'monospace' }}
        />
      </Modal>
    </div>
  );
}
