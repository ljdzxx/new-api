import React from 'react';
import { Button, Input, Select, Space, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import {
  defaultAdvancedCustomConfig,
  defaultAdvancedCustomRoute,
} from '../../../../helpers/advancedCustomSettings';

export default function AdvancedCustomEditor({ value, onChange }) {
  const { t } = useTranslation();
  const config = value || defaultAdvancedCustomConfig();
  const routes = config.advanced_routes || [];
  const update = (index, patch) =>
    onChange({
      ...config,
      advanced_routes: routes.map((route, i) =>
        i === index ? { ...route, ...patch } : route,
      ),
    });
  return (
    <Space vertical align='start' style={{ width: '100%' }}>
      <Typography.Text type='secondary'>
        {t(
          '将 Gemini 请求转换为 Responses，并将结果转换回 Gemini；流式入口自动匹配。',
        )}
      </Typography.Text>
      {routes.map((route, index) => {
        const auth = route.auth || defaultAdvancedCustomRoute().auth;
        return (
          <Space vertical align='start' key={index} style={{ width: '100%' }}>
            <Typography.Text strong>{t('入口版本')}</Typography.Text>
            <Select
              style={{ width: '100%' }}
              value={route.incoming_path}
              onChange={(incoming_path) => update(index, { incoming_path })}
              optionList={['v1beta', 'v1'].map((version) => ({
                value: `/${version}/models/{model}:generateContent`,
                label: version,
              }))}
            />
            <Typography.Text strong>{t('上游协议')}</Typography.Text>
            <Select
              style={{ width: '100%' }}
              value={route.converter}
              optionList={[
                {
                  value: defaultAdvancedCustomRoute().converter,
                  label: 'OpenAI Responses',
                },
              ]}
              onChange={(converter) => update(index, { converter })}
            />
            <Typography.Text strong>{t('上游请求路径')}</Typography.Text>
            <Input
              value={route.upstream_path}
              placeholder={t('例如：{{example}}', { example: '/v1/responses' })}
              onChange={(upstream_path) => update(index, { upstream_path })}
            />
            <Typography.Text strong>{t('上游鉴权')}</Typography.Text>
            <Select
              style={{ width: '100%' }}
              value={auth.type}
              optionList={[
                { value: 'header', label: t('请求头') },
                { value: 'query', label: t('查询参数') },
                { value: 'none', label: t('无需鉴权') },
              ]}
              onChange={(type) => update(index, { auth: { ...auth, type } })}
            />
            {auth.type !== 'none' && (
              <>
                <Input
                  value={auth.name}
                  placeholder={t('鉴权字段名称')}
                  onChange={(name) =>
                    update(index, { auth: { ...auth, name } })
                  }
                />
                <Input
                  value={auth.value}
                  placeholder={t('鉴权字段值，使用 {key} 引用渠道密钥')}
                  onChange={(value) =>
                    update(index, { auth: { ...auth, value } })
                  }
                />
              </>
            )}
            <Button
              type='danger'
              disabled={routes.length <= 1}
              onClick={() =>
                onChange({
                  ...config,
                  advanced_routes: routes.filter((_, i) => i !== index),
                })
              }
            >
              {t('删除路由')}
            </Button>
          </Space>
        );
      })}
      <Button
        disabled={routes.length >= 2}
        onClick={() =>
          onChange({
            ...config,
            advanced_routes: [
              ...routes,
              defaultAdvancedCustomRoute(
                routes.some((r) => r.incoming_path.startsWith('/v1beta/'))
                  ? 'v1'
                  : 'v1beta',
              ),
            ],
          })
        }
      >
        {t('添加路由')}
      </Button>
    </Space>
  );
}
