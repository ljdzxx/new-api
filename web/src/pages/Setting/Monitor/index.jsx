import React, { useEffect, useState } from 'react';
import { Button, Banner, Spin, Switch, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../../helpers';
import './settings.css';

export default function MonitorSettings() {
  const { t } = useTranslation();
  const [config, setConfig] = useState(null);
  const [groups, setGroups] = useState('{}');
  const [saving, setSaving] = useState(false);
  const [redis, setRedis] = useState(true);
  useEffect(() => {
    API.get('/api/monitor/config')
      .then(({ data }) => {
        if (!data.success) throw new Error(data.message);
        setConfig(data.data.config);
        setGroups(JSON.stringify(data.data.config.groups, null, 2));
        setRedis(data.data.redis_enabled);
      })
      .catch((e) => showError(e.message));
  }, []);
  if (!config) return <Spin />;
  const update = (key, value) => setConfig((old) => ({ ...old, [key]: value }));
  const save = async () => {
    setSaving(true);
    try {
      const payload = { ...config, groups: JSON.parse(groups) };
      const { data } = await API.put('/api/monitor/config', payload);
      if (!data.success) throw new Error(data.message);
      const next = await API.get('/api/monitor/config');
      if (next.data.success) {
        setConfig(next.data.data.config);
        setGroups(JSON.stringify(next.data.data.config.groups, null, 2));
      }
      showSuccess(t('保存成功'));
    } catch (e) {
      showError(e.message);
    } finally {
      setSaving(false);
    }
  };
  const field = (key, label, type = 'text', min, max) => (
    <label key={key} className='flex flex-col gap-2'>
      <span>{label}</span>
      <input
        className='monitor-settings-field rounded border p-2'
        type={type}
        min={min}
        max={max}
        value={config[key] ?? ''}
        onChange={(e) =>
          update(
            key,
            type === 'number' ? Number(e.target.value) : e.target.value,
          )
        }
      />
    </label>
  );
  const area = (key, label) => (
    <label className='flex flex-col gap-2'>
      {label}
      <textarea
        className='monitor-settings-field rounded border p-2'
        rows={4}
        value={config[key] ?? ''}
        onChange={(e) => update(key, e.target.value)}
      />
    </label>
  );
  const toggle = (key, label) => (
    <label className='flex items-center gap-3'>
      <Switch checked={config[key]} onChange={(v) => update(key, v)} />
      {label}
    </label>
  );
  return (
    <div className='monitor-settings p-4 flex flex-col gap-6 max-w-5xl'>
      <Typography.Title heading={4}>{t('监控设置')}</Typography.Title>
      {!redis && (
        <Banner
          type='warning'
          description={t(
            '未配置 Redis，监控页面不展示数据，检测任务不会启动。',
          )}
        />
      )}
      <Banner
        type='info'
        description={t(
          '检测任务仅在 master 执行；所有节点采集请求统计。配置修改将在其他节点同步设置后生效。',
        )}
      />
      {toggle('enabled', t('启用分组监控'))}
      <label className='flex flex-col gap-2'>
        {t('监控分组 JSON')}
        <textarea
          spellCheck={false}
          className='monitor-settings-field rounded border p-3 font-mono'
          rows={10}
          value={groups}
          onChange={(e) => setGroups(e.target.value)}
        />
      </label>
      <Typography.Text type='tertiary'>
        {t('每组单独配置测试模型、协议、测试开关和分组启用状态。')}
      </Typography.Text>
      <pre className='overflow-x-auto text-xs'>
        {JSON.stringify(
          {
            'codex-pro': {
              model: ['gpt-5.5', 'gpt-5.6-sol'],
              svg_model: 'gpt-6-astra',
              logic_model: 'gpt-6-astra',
              logic_prompt: '2, 4, 8, 16, ?',
              logic_answer: '32',
              logic_match_mode: 'exact',
              key: '{your-key}',
              protocol: 'responses',
              svg_test: true,
              logic_test: false,
              active: true,
              order: 0,
            },
          },
          null,
          2,
        )}
      </pre>
      <Typography.Text type='tertiary'>
        {t(
          'logic_prompt、logic_answer、logic_match_mode 可逐字段覆盖全局逻辑题设置；省略、null、空字符串或仅空白时继承全局值。匹配方式为 exact 或 contains。',
        )}
      </Typography.Text>
      <Typography.Text type='tertiary'>
        {t(
          'protocol 填 responses 或 messages；active 为 false 时隐藏分组，并停止发起可用性探针、SVG 和逻辑检测。',
        )}
      </Typography.Text>
      <Typography.Text type='tertiary'>
        {t(
          'order 为展示顺序（整数），默认排序时越小越靠前；未填写时为 0，相同值按分组名称排序。',
        )}
      </Typography.Text>
      <div className='grid md:grid-cols-2 gap-4'>
        {field('base_url', t('本站探测地址'))}
        {field(
          'probe_minutes',
          t('可用性探测频率（分钟）'),
          'number',
          1,
          10080,
        )}
        {field('timeout_seconds', t('单次请求超时（秒）'), 'number', 5, 600)}
        {field('concurrency', t('检测并发数'), 'number', 1, 10)}
        {field(
          'history_keep',
          t('每组每类测试历史最多记录数'),
          'number',
          1,
          1000,
        )}
        {field('history_days', t('测试历史保留天数'), 'number', 1, 90)}
      </div>
      <Typography.Text type='tertiary'>
        {t(
          '逻辑题与 SVG 分别按数量和天数保留，任一超限即清理记录及详情；R2 文件数量单独设置，本地文件不自动删除。',
        )}
      </Typography.Text>
      <Typography.Title heading={5}>{t('SVG 绘图检测')}</Typography.Title>
      <div className='grid md:grid-cols-2 gap-4'>
        {field('svg_minutes', t('SVG 检测频率（分钟）'), 'number', 1, 10080)}
        {field('svg_keep', t('R2 每组保留 SVG 记录数量'), 'number', 1, 100)}
        {field('svg_output_dir', t('SVG 本地输出目录'))}
        {field('svg_public_base_url', t('SVG 公开访问地址（可选）'))}
        {field('svg_prefix', t('监控-SVG绘图-R2 对象前缀'))}
        {field(
          'svg_url_hours',
          t('监控-SVG绘图-URL 有效期（小时）'),
          'number',
          1,
          168,
        )}
      </div>
      <Typography.Text type='tertiary'>
        {t(
          '先保存题目、原始回复、结果和 HTML 到本地，再上传 R2。相对目录以 Go 服务工作目录为基准；本地文件保留用于排查，R2 按配置数量清理。',
        )}
      </Typography.Text>
      <Typography.Text type='tertiary'>
        {t(
          '公开访问地址填写已绑定 R2 或 Worker 的 HTTPS 域名；留空使用临时签名链接。不要修改用于上传的 S3 Endpoint。',
        )}
      </Typography.Text>
      {area('svg_prompt', t('SVG 绘图题目'))}
      <Typography.Title heading={5}>{t('逻辑题检测')}</Typography.Title>
      <Typography.Text type='tertiary'>
        {t(
          '以下题目、答案和匹配方式为全局默认值；分组优先使用自身配置。所有启用逻辑检测的分组都提供题目和答案时，全局题目和答案可留空。',
        )}
      </Typography.Text>
      {field('logic_minutes', t('逻辑检测频率（分钟）'), 'number', 1, 10080)}
      {area('logic_prompt', t('逻辑题目'))}
      {area('logic_answer', t('预期答案'))}
      <label className='flex flex-col gap-2'>
        {t('答案匹配方式')}
        <select
          className='monitor-settings-field rounded border p-2'
          value={config.logic_match_mode || 'exact'}
          onChange={(e) => update('logic_match_mode', e.target.value)}
        >
          <option value='exact'>{t('完全相等')}</option>
          <option value='contains'>{t('包含指定的字符')}</option>
        </select>
      </label>
      <Typography.Text type='tertiary'>
        {t(
          '逻辑测试仅调用该组的 logic_model；去除首尾空白后，按选定方式匹配，区分大小写。',
        )}
      </Typography.Text>
      <Button theme='solid' loading={saving} onClick={save}>
        {t('保存')}
      </Button>
    </div>
  );
}
