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
import React, { useState, useEffect, useMemo, useRef, useContext } from 'react';
import {
  Modal,
  RadioGroup,
  Radio,
  Select,
  Input,
  Toast,
  Typography,
  Button,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, selectFilter, getModelCategories } from '../../../../helpers';
import {
  buildCCSwitchURL,
  getCCSwitchName,
} from '../../../../helpers/ccSwitch';
import { StatusContext } from '../../../../context/Status';

const APP_CONFIGS = {
  claude: {
    label: 'Claude',
    modelFields: [
      { key: 'model', label: '主模型' },
      { key: 'haikuModel', label: 'Haiku 模型' },
      { key: 'sonnetModel', label: 'Sonnet 模型' },
      { key: 'opusModel', label: 'Opus 模型' },
    ],
  },
  codex: {
    label: 'Codex',
    modelFields: [{ key: 'model', label: '主模型' }],
  },
  gemini: {
    label: 'Gemini',
    modelFields: [{ key: 'model', label: '主模型' }],
  },
};

function getCachedStatus() {
  try {
    return JSON.parse(localStorage.getItem('status')) || {};
  } catch (_) {}
  return {};
}

export default function CCSwitchModal({ onClose, token, fetchTokenKey }) {
  const { t } = useTranslation();
  const [statusState] = useContext(StatusContext);
  const status = statusState?.status || getCachedStatus();
  const defaultName = getCCSwitchName(
    status.system_name ?? localStorage.getItem('system_name'),
    token.name,
  );
  const [app, setApp] = useState('claude');
  const [name, setName] = useState(null);
  const [models, setModels] = useState({});
  const [modelNames, setModelNames] = useState([]);
  const [tokenKey, setTokenKey] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [reload, setReload] = useState(0);
  const fetchTokenKeyRef = useRef(fetchTokenKey);
  fetchTokenKeyRef.current = fetchTokenKey;

  const currentConfig = APP_CONFIGS[app];

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError('');
    setModelNames([]);
    setModels({});
    setTokenKey('');
    const load = async () => {
      try {
        const [res, key] = await Promise.all([
          API.get(`/api/token/${token.id}/models`, {
            skipErrorHandler: true,
            disableDuplicate: true,
          }),
          fetchTokenKeyRef.current(token.id, { suppressError: true }),
        ]);
        if (!active) return;
        if (!res.data?.success || !Array.isArray(res.data.data)) {
          throw new Error(res.data?.message || t('加载模型列表失败'));
        }
        if (!key) throw new Error(t('获取令牌密钥失败'));
        setModelNames(res.data.data);
        setTokenKey(key);
      } catch (error) {
        if (active) setLoadError(error.message || t('加载模型列表失败'));
      } finally {
        if (active) setLoading(false);
      }
    };
    load();
    return () => {
      active = false;
    };
  }, [token.id, reload, t]);

  const modelOptions = useMemo(() => {
    const categories = getModelCategories(t);
    return modelNames.map((model) => {
      const category = Object.entries(categories).find(
        ([key, category]) =>
          key !== 'all' && category.filter({ model_name: model }),
      )?.[1];
      return {
        value: model,
        label: (
          <span className='flex items-center gap-1'>
            {category?.icon}
            {model}
          </span>
        ),
      };
    });
  }, [modelNames, t]);

  const handleAppChange = (val) => {
    setApp(val);
    setModels({});
  };

  const handleModelChange = (field, value) => {
    setModels((prev) => ({ ...prev, [field]: value }));
  };

  const handleSubmit = () => {
    if (loading || loadError || !tokenKey || modelNames.length === 0) return;
    if (
      !models.model ||
      Object.values(models).some(
        (value) => value && !modelNames.includes(value),
      )
    ) {
      Toast.warning(t('请选择主模型'));
      return;
    }
    const url = buildCCSwitchURL({
      app,
      name: name?.trim() || defaultName,
      models,
      apiKey: 'sk-' + tokenKey,
      serverAddress: status.server_address?.trim() || window.location.origin,
    });
    window.open(url, '_blank');
    onClose();
  };

  const fieldLabelStyle = useMemo(
    () => ({
      marginBottom: 4,
      fontSize: 13,
      color: 'var(--semi-color-text-1)',
    }),
    [],
  );

  return (
    <Modal
      title={t('填入 CC Switch')}
      visible
      onCancel={onClose}
      onOk={handleSubmit}
      okText={t('打开 CC Switch')}
      cancelText={t('取消')}
      okButtonProps={{
        disabled:
          loading || !!loadError || !tokenKey || modelNames.length === 0,
      }}
      maskClosable={false}
      width={480}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <div>
          <div style={fieldLabelStyle}>{t('应用')}</div>
          <RadioGroup
            type='button'
            value={app}
            onChange={(e) => handleAppChange(e.target.value)}
            style={{ width: '100%' }}
          >
            {Object.entries(APP_CONFIGS).map(([key, cfg]) => (
              <Radio key={key} value={key}>
                {cfg.label}
              </Radio>
            ))}
          </RadioGroup>
        </div>

        <div>
          <div style={fieldLabelStyle}>{t('名称')}</div>
          <Input
            value={name ?? defaultName}
            onChange={setName}
            placeholder={defaultName}
          />
        </div>

        {loadError ? (
          <div>
            <Typography.Text type='danger'>{loadError}</Typography.Text>
            <Button onClick={() => setReload((value) => value + 1)}>
              {t('重试')}
            </Button>
          </div>
        ) : (
          !loading &&
          modelNames.length === 0 && (
            <Typography.Text type='warning'>
              {t('暂无可用模型')}
            </Typography.Text>
          )
        )}

        {currentConfig.modelFields.map((field) => (
          <div key={field.key}>
            <div style={fieldLabelStyle}>
              {t(field.label)}
              {field.key === 'model' && (
                <Typography.Text type='danger'> *</Typography.Text>
              )}
            </div>
            <Select
              placeholder={t('请选择模型')}
              optionList={modelOptions}
              loading={loading}
              disabled={loading || !!loadError || modelNames.length === 0}
              value={models[field.key] || undefined}
              onChange={(val) => handleModelChange(field.key, val)}
              filter={selectFilter}
              style={{ width: '100%' }}
              showClear
              searchable
              emptyContent={t('暂无可用模型')}
            />
          </div>
        ))}
      </div>
    </Modal>
  );
}
