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

import React, { useEffect, useState } from 'react';
import {
  Button,
  Form,
  Modal,
  Table,
  Switch,
  Typography,
  Space,
  Spin,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../helpers';
import { useTranslation } from 'react-i18next';
import { GripVertical } from 'lucide-react';
import { EPAY_METHOD_NAMES, reorderEpayChannels } from '../../../helpers/epay';

const defaultMethodLimits = () =>
  Object.fromEntries(
    Object.keys(EPAY_METHOD_NAMES).map((method) => [
      method,
      { min_pay_cny: '1', max_pay_cny: '5000', daily_limit_cny: '0' },
    ]),
  );

const channelEditorValues = (channel) => ({
  ...channel,
  query_api: channel.query_api || 'api_pay_query',
  merchant_key: '',
  payment_method_limits: {
    ...defaultMethodLimits(),
    ...channel.payment_method_limits,
  },
});

const emptyChannel = {
  name: '',
  description: '',
  enabled: false,
  pay_address: '',
  query_api: 'api_pay_query',
  merchant_id: '',
  merchant_key: '',
  callback_address: '',
  payment_methods: ['wxpay', 'alipay'],
  topup_offer_type: 'none',
  topup_offer_percent: '100',
  topup_offer_threshold_cny: '0',
  subscription_discount_percent: '100',
  subscription_offer_threshold_cny: '0',
  revision_id: 0,
};

export default function SettingsPaymentGateway({ options, refresh }) {
  const { t: translate } = useTranslation();
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState(null);
  const [saving, setSaving] = useState(false);
  const [formApi, setFormApi] = useState(null);
  const [globalApi, setGlobalApi] = useState(null);
  const [ordering, setOrdering] = useState(false);
  const [togglingID, setTogglingID] = useState(null);
  const [draggedID, setDraggedID] = useState(null);
  const [dropTargetID, setDropTargetID] = useState(null);
  const channelBusy = loading || saving || ordering || togglingID !== null;

  const loadChannels = async () => {
    setLoading(true);
    try {
      const response = await API.get('/api/option/epay/channels');
      if (!response.data.success) throw new Error(response.data.message);
      setChannels(response.data.data || []);
    } catch (error) {
      showError(error.message || translate('加载失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadChannels();
  }, []);
  useEffect(() => {
    globalApi?.setValues({
      epayAmounts:
        options['epay_setting.amount_options'] ||
        '[10,20,50,100,200,500,1000,2000,5000]',
      amountOptions:
        options['payment_setting.amount_options'] || '[10,20,50,100,200,500]',
      mallLinks: options['payment_setting.mall_links'] || '{}',
    });
  }, [options, globalApi]);

  const saveGlobals = async () => {
    setSaving(true);
    try {
      const values = await globalApi.validate();
      for (const field of ['epayAmounts', 'amountOptions']) {
        const parsed = JSON.parse(values[field]);
        if (
          !Array.isArray(parsed) ||
          !parsed.length ||
          parsed.length > 50 ||
          new Set(parsed).size !== parsed.length ||
          parsed.some(
            (amount) =>
              !Number.isSafeInteger(amount) ||
              amount <= 0 ||
              amount > 100000000,
          )
        )
          throw new Error(translate('充值金额选项必须是有效的正整数数组'));
      }
      const links = JSON.parse(values.mallLinks);
      if (!links || Array.isArray(links) || typeof links !== 'object')
        throw new Error(translate('商品链接配置必须是 JSON 对象'));
      const updates = [
        ['epay_setting.amount_options', values.epayAmounts],
        ['payment_setting.amount_options', values.amountOptions],
        ['payment_setting.mall_links', values.mallLinks],
      ];
      for (const [key, value] of updates) {
        const response = await API.put('/api/option/', { key, value });
        if (!response.data.success) throw new Error(response.data.message);
      }
      showSuccess(translate('更新成功'));
      refresh?.();
    } catch (error) {
      showError(error.message || translate('更新失败'));
    } finally {
      setSaving(false);
    }
  };

  const saveChannel = async () => {
    setSaving(true);
    try {
      const values = await formApi.validate();
      const payload = { ...editing, ...values };
      for (const field of [
        'topup_offer_percent',
        'topup_offer_threshold_cny',
        'subscription_discount_percent',
        'subscription_offer_threshold_cny',
      ])
        payload[field] = String(payload[field] ?? '0');
      payload.payment_method_limits = Object.fromEntries(
        Object.entries(payload.payment_method_limits).map(
          ([method, limits]) => [
            method,
            Object.fromEntries(
              ['min_pay_cny', 'max_pay_cny', 'daily_limit_cny'].map((field) => [
                field,
                String(limits[field] ?? ''),
              ]),
            ),
          ],
        ),
      );
      const response = editing.id
        ? await API.put(`/api/option/epay/channels/${editing.id}`, payload)
        : await API.post('/api/option/epay/channels', payload);
      if (!response.data.success) throw new Error(response.data.message);
      setEditing(null);
      showSuccess(translate('更新成功'));
      await loadChannels();
    } catch (error) {
      showError(error.message || translate('更新失败'));
    } finally {
      setSaving(false);
    }
  };

  const moveChannel = async (sourceID, targetID) => {
    if (channelBusy) return;
    const previous = channels;
    const reordered = reorderEpayChannels(channels, sourceID, targetID);
    if (reordered === channels) return;
    setOrdering(true);
    setChannels(reordered);
    try {
      const response = await API.put('/api/option/epay/channels/order', {
        channel_ids: reordered.map((channel) => channel.id),
        expected_ids: previous.map((channel) => channel.id),
      });
      if (!response.data.success) throw new Error(response.data.message);
    } catch (error) {
      setChannels(previous);
      showError(error.message || translate('更新失败'));
      await loadChannels();
    } finally {
      setOrdering(false);
    }
  };

  const toggleChannel = async (channel, enabled) => {
    if (channelBusy) return;
    setTogglingID(channel.id);
    try {
      const response = await API.put(
        `/api/option/epay/channels/${channel.id}/enabled`,
        { enabled, revision_id: channel.revision_id },
      );
      if (!response.data.success) throw new Error(response.data.message);
      setChannels((current) =>
        current.map((item) =>
          item.id === channel.id ? { ...item, enabled } : item,
        ),
      );
    } catch (error) {
      showError(error.message || translate('更新失败'));
      await loadChannels();
    } finally {
      setTogglingID(null);
    }
  };

  return (
    <Spin spinning={loading}>
      <Typography.Title heading={5}>{translate('易支付设置')}</Typography.Title>
      <Typography.Paragraph type='tertiary'>
        {translate(
          '易支付共用系统美元汇率：余额基础到账 USD = 输入 CNY ÷ 汇率；订阅原价 CNY = 套餐 USD × 汇率，优惠由各渠道独立配置。',
        )}
      </Typography.Paragraph>
      <Form getFormApi={setGlobalApi} labelPosition='top'>
        <Form.TextArea
          field='epayAmounts'
          label={translate('易支付快捷充值金额（CNY）')}
          autosize
        />
        <Form.TextArea
          field='amountOptions'
          label={translate('Stripe / 商城充值数量选项')}
          autosize
        />
        <Form.TextArea
          field='mallLinks'
          label={translate('商城充值商品链接')}
          extraText={translate(
            '按充值数量映射商品链接，不再设置最低充值数量。',
          )}
          autosize
        />
        <Button onClick={saveGlobals} loading={saving}>
          {translate('保存公共设置')}
        </Button>
      </Form>
      <div className='my-4 flex items-center justify-between'>
        <Typography.Title heading={6}>
          {translate('易支付渠道')}
        </Typography.Title>
        <Button
          theme='solid'
          disabled={channelBusy}
          onClick={() => setEditing(channelEditorValues(emptyChannel))}
        >
          {translate('新增易支付渠道')}
        </Button>
      </div>
      <Typography.Paragraph type='tertiary'>
        {translate(
          '拖动六点手柄调整渠道顺序，松开后自动保存；也可聚焦手柄后使用上下方向键。',
        )}
      </Typography.Paragraph>
      <Table
        dataSource={channels}
        rowKey='id'
        pagination={false}
        loading={ordering}
        onRow={(channel) => ({
          onDragOver: (event) => {
            if (draggedID === null || channelBusy) return;
            event.preventDefault();
            event.dataTransfer.dropEffect = 'move';
            setDropTargetID(channel.id);
          },
          onDrop: (event) => {
            if (draggedID === null || channelBusy) return;
            event.preventDefault();
            moveChannel(draggedID, channel.id);
            setDraggedID(null);
            setDropTargetID(null);
          },
          style: {
            opacity: draggedID === channel.id ? 0.5 : 1,
            outline:
              dropTargetID === channel.id && draggedID !== channel.id
                ? '2px solid var(--semi-color-primary)'
                : undefined,
            outlineOffset: -2,
          },
        })}
        columns={[
          {
            title: '',
            key: 'drag',
            width: 44,
            render: (_, channel, index) => (
              <button
                type='button'
                draggable={!channelBusy}
                disabled={channelBusy}
                aria-label={translate('拖动排序')}
                title={translate('拖动排序')}
                className='flex items-center justify-center rounded p-1 text-[var(--semi-color-text-2)] disabled:opacity-40'
                style={{ cursor: channelBusy ? 'not-allowed' : 'grab' }}
                onDragStart={(event) => {
                  event.dataTransfer.effectAllowed = 'move';
                  event.dataTransfer.setData('text/plain', String(channel.id));
                  setDraggedID(channel.id);
                }}
                onDragEnd={() => {
                  setDraggedID(null);
                  setDropTargetID(null);
                }}
                onKeyDown={(event) => {
                  const direction =
                    event.key === 'ArrowUp'
                      ? -1
                      : event.key === 'ArrowDown'
                        ? 1
                        : 0;
                  if (!direction) return;
                  event.preventDefault();
                  const target = channels[index + direction];
                  if (target) moveChannel(channel.id, target.id);
                }}
              >
                <GripVertical size={18} />
              </button>
            ),
          },
          { title: 'ID', dataIndex: 'id', width: 60 },
          { title: translate('渠道名称'), dataIndex: 'name' },
          {
            title: translate('查单接口'),
            render: (_, channel) =>
              channel.query_api === 'api_php'
                ? 'GET /api.php?act=order'
                : 'POST /api/pay/query',
          },
          {
            title: translate('状态'),
            render: (_, channel) => (
              <Space vertical align='start' spacing={4}>
                <Typography.Text size='small' type='tertiary'>
                  {channel.enabled ? translate('已启用') : translate('已停用')}
                </Typography.Text>
                <Switch
                  checked={channel.enabled}
                  disabled={channelBusy}
                  loading={togglingID === channel.id}
                  aria-label={translate(channel.enabled ? '停用' : '启用')}
                  onChange={(enabled) => toggleChannel(channel, enabled)}
                />
              </Space>
            ),
          },
          {
            title: translate('支付方式'),
            render: (_, channel) => channel.payment_methods.join(' / '),
          },
          {
            title: translate('支付方式限额（实付 CNY）'),
            render: (_, channel) =>
              channel.payment_methods.map((method) => {
                const limits = channel.payment_method_limits?.[method];
                return (
                  <div key={method} className='mb-1 text-xs'>
                    {translate(EPAY_METHOD_NAMES[method])}：
                    {limits?.min_pay_cny} – {limits?.max_pay_cny}
                    <span className='ml-2'>
                      {translate('每日限额')}：
                      {Number(limits?.daily_limit_cny) > 0
                        ? limits.daily_limit_cny
                        : translate('不限')}
                    </span>
                  </div>
                );
              }),
          },
          {
            title: translate('余额优惠'),
            render: (_, channel) =>
              channel.topup_offer_type === 'none'
                ? translate('无优惠')
                : `${translate(channel.topup_offer_type === 'bonus' ? '赠金' : '折扣')} ${channel.topup_offer_percent}% / ${translate('起点')} ¥${channel.topup_offer_threshold_cny}`,
          },
          {
            title: translate('订阅折扣'),
            render: (_, channel) =>
              `${channel.subscription_discount_percent}% / ${translate('起点')} ¥${channel.subscription_offer_threshold_cny}`,
          },
          {
            title: translate('操作'),
            render: (_, channel) => (
              <Button
                disabled={channelBusy}
                onClick={() => setEditing(channelEditorValues(channel))}
              >
                {translate('编辑')}
              </Button>
            ),
          },
        ]}
      />
      <Modal
        title={translate(editing?.id ? '编辑易支付渠道' : '新增易支付渠道')}
        visible={!!editing}
        onCancel={() => setEditing(null)}
        onOk={saveChannel}
        confirmLoading={saving}
        centered
        width='min(720px, calc(100vw - 32px))'
        bodyStyle={{ maxHeight: 'calc(100dvh - 180px)', overflowY: 'auto' }}
        keepDOM={false}
      >
        {editing && (
          <Form
            key={`${editing.id || 'new'}-${editing.revision_id}`}
            initValues={editing}
            getFormApi={setFormApi}
            labelPosition='top'
          >
            <Form.Switch field='enabled' label={translate('启用渠道')} />
            <Form.Input
              field='name'
              label={translate('渠道名称')}
              rules={[{ required: true }]}
            />
            <Form.TextArea
              field='description'
              label={translate('渠道说明')}
              autosize
            />
            <Form.Input
              field='pay_address'
              label={translate('支付地址')}
              rules={[{ required: true }]}
            />
            <Form.Select
              field='query_api'
              label={translate('查单接口')}
              rules={[{ required: true }]}
              optionList={[
                {
                  label: 'POST /api/pay/query',
                  value: 'api_pay_query',
                },
                {
                  label: 'GET /api.php?act=order',
                  value: 'api_php',
                },
              ]}
              extraText={translate(
                '按支付平台文档选择，不会自动切换。接口路径追加在支付地址后，例如 https://pay.example.com/xpay/epay + /api.php。',
              )}
              style={{ width: '100%' }}
            />
            <Typography.Paragraph type='tertiary' size='small'>
              {translate(
                '查单接口随订单冻结；修改仅影响新订单，已有订单仍使用创建时的接口。',
              )}
            </Typography.Paragraph>
            <Form.Input
              field='merchant_id'
              label={translate('易支付商户ID')}
              rules={[{ required: true }]}
            />
            <Form.Input
              field='merchant_key'
              type='password'
              label={translate('易支付商户密钥')}
              extraText={translate(
                '已保存的密钥不会回显；编辑时留空保持不变。',
              )}
            />
            <Form.Input
              field='callback_address'
              label={translate('回调地址')}
              extraText={translate(
                '留空使用系统服务器地址，仅填写基础地址，系统自动追加回调路径。',
              )}
            />
            <Form.CheckboxGroup
              field='payment_methods'
              label={translate('支付方式')}
              options={[
                { label: translate('微信支付'), value: 'wxpay' },
                { label: translate('支付宝'), value: 'alipay' },
                { label: 'QQ', value: 'qqpay' },
              ]}
            />
            <Typography.Paragraph type='tertiary'>
              {translate(
                '各支付方式独立限额，余额充值与订阅共用；每日限额填 0 表示不限，按服务器本地时间 0 点至次日 0 点统计。待支付订单会占用当日额度。',
              )}
            </Typography.Paragraph>
            {Object.entries(EPAY_METHOD_NAMES).map(([method, name]) => (
              <div
                key={method}
                className='mb-3 rounded-lg border border-[var(--semi-color-border)] p-3'
              >
                <Typography.Text strong>{translate(name)}</Typography.Text>
                <div className='grid grid-cols-1 gap-x-3 sm:grid-cols-3'>
                  <Form.Input
                    field={`payment_method_limits.${method}.min_pay_cny`}
                    label={translate('单笔最低（实付 CNY）')}
                    rules={[{ required: true }]}
                  />
                  <Form.Input
                    field={`payment_method_limits.${method}.max_pay_cny`}
                    label={translate('单笔最高（实付 CNY）')}
                    rules={[{ required: true }]}
                  />
                  <Form.Input
                    field={`payment_method_limits.${method}.daily_limit_cny`}
                    label={translate('每日限额（实付 CNY）')}
                    rules={[{ required: true }]}
                  />
                </div>
              </div>
            ))}
            <Form.Select
              field='topup_offer_type'
              label={translate('余额充值优惠')}
              optionList={[
                { label: translate('无优惠'), value: 'none' },
                { label: translate('赠金'), value: 'bonus' },
                { label: translate('折扣'), value: 'discount' },
              ]}
            />
            <Form.Input
              field='topup_offer_percent'
              label={translate('余额优惠百分比')}
              extraText={translate(
                '赠金填大于 100 的百分比，如 102；折扣填 0 到 100 之间的百分比，如 98。',
              )}
            />
            <Form.Input
              field='topup_offer_threshold_cny'
              label={translate('余额优惠起点金额（优惠前 CNY）')}
            />
            <Form.Input
              field='subscription_discount_percent'
              label={translate('订阅折扣百分比')}
              extraText={translate(
                '订阅仅支持折扣；100 表示无折扣，98 表示按原价的 98% 实付。',
              )}
            />
            <Form.Input
              field='subscription_offer_threshold_cny'
              label={translate('订阅优惠起点金额（优惠前 CNY）')}
            />
          </Form>
        )}
      </Modal>
    </Spin>
  );
}
