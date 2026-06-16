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
import {
  Button,
  Card,
  DatePicker,
  Empty,
  Input,
  InputNumber,
  Modal,
  Space,
  Switch,
  Table,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconEdit, IconPlus, IconUpload } from '@douyinfe/semi-icons';
import { Gift, PlayCircle, RefreshCw, Trophy } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, timestamp2string } from '../../helpers';

const { Text, Title } = Typography;

const emptyPeriodForm = {
  id: 0,
  issue: 0,
  title: '',
  start_time: 0,
  end_time: 0,
  display_enabled: false,
};

const emptyPrizeForm = {
  id: 0,
  level_name: '',
  quantity: 1,
  description: '',
  paid_only: false,
  sort_order: 0,
};

const parseDateToTimestamp = (value) => {
  if (!value) return 0;
  const date = value instanceof Date ? value : new Date(value);
  const timestamp = Math.floor(date.getTime() / 1000);
  return Number.isNaN(timestamp) ? 0 : timestamp;
};

const statusTag = (status, t) => {
  if (status === 'drawn') return <Tag color='green'>{t('已开奖')}</Tag>;
  if (status === 'drawing') return <Tag color='orange'>{t('开奖中')}</Tag>;
  return <Tag color='blue'>{t('未开奖')}</Tag>;
};

const LotteryAdmin = () => {
  const { t } = useTranslation();
  const [periods, setPeriods] = useState([]);
  const [selectedId, setSelectedId] = useState();
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(false);
  const [periodModalVisible, setPeriodModalVisible] = useState(false);
  const [periodForm, setPeriodForm] = useState(emptyPeriodForm);
  const [prizeModalVisible, setPrizeModalVisible] = useState(false);
  const [prizeForm, setPrizeForm] = useState(emptyPrizeForm);
  const [codeModalVisible, setCodeModalVisible] = useState(false);
  const [codePrize, setCodePrize] = useState(null);
  const [codeText, setCodeText] = useState('');

  const selectedPeriod = detail?.period;

  const loadPeriods = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/lottery/admin/periods');
      const { success, data, message } = res.data;
      if (success) {
        setPeriods(data || []);
        if (!selectedId && data?.length > 0) {
          setSelectedId(data[0].id);
        }
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } finally {
      setLoading(false);
    }
  };

  const loadDetail = async (id = selectedId) => {
    if (!id) {
      setDetail(null);
      return;
    }
    setLoading(true);
    try {
      const res = await API.get(`/api/lottery/admin/periods/${id}`, {
        disableDuplicate: true,
      });
      const { success, data, message } = res.data;
      if (success) {
        setDetail(data);
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadPeriods();
  }, []);

  useEffect(() => {
    loadDetail(selectedId);
  }, [selectedId]);

  const refreshAll = async () => {
    await loadPeriods();
    await loadDetail(selectedId);
  };

  const openCreatePeriod = () => {
    setPeriodForm(emptyPeriodForm);
    setPeriodModalVisible(true);
  };

  const openEditPeriod = (period) => {
    setPeriodForm({ ...emptyPeriodForm, ...period });
    setPeriodModalVisible(true);
  };

  const savePeriod = async () => {
    const payload = {
      issue: Number(periodForm.issue || 0),
      title: periodForm.title || '',
      start_time: Number(periodForm.start_time || 0),
      end_time: Number(periodForm.end_time || 0),
      display_enabled: Boolean(periodForm.display_enabled),
    };
    const res = periodForm.id
      ? await API.put(`/api/lottery/admin/periods/${periodForm.id}`, payload)
      : await API.post('/api/lottery/admin/periods', payload);
    const { success, message, data } = res.data;
    if (!success) {
      Toast.error({ content: message || t('保存失败') });
      return false;
    }
    Toast.success({ content: t('保存成功') });
    setPeriodModalVisible(false);
    if (!periodForm.id && data?.id) {
      setSelectedId(data.id);
    }
    await refreshAll();
    return true;
  };

  const deletePeriod = (period) => {
    Modal.confirm({
      title: t('删除抽奖期数'),
      content: t('删除后将同时移除该期奖品、兑换码和参与记录，是否继续？'),
      onOk: async () => {
        const res = await API.delete(`/api/lottery/admin/periods/${period.id}`);
        if (res.data.success) {
          Toast.success({ content: t('删除成功') });
          setSelectedId(undefined);
          setDetail(null);
          await loadPeriods();
        } else {
          Toast.error({ content: res.data.message || t('删除失败') });
        }
      },
    });
  };

  const drawPeriod = (period) => {
    Modal.confirm({
      title: t('手动开奖'),
      content: t('开奖后结果不可重开，请确认兑换码数量已与奖品数量一致。'),
      onOk: async () => {
        const res = await API.post(`/api/lottery/admin/periods/${period.id}/draw`);
        if (res.data.success) {
          Toast.success({ content: t('开奖完成') });
          await refreshAll();
        } else {
          Toast.error({ content: res.data.message || t('开奖失败') });
        }
      },
    });
  };

  const openCreatePrize = () => {
    setPrizeForm(emptyPrizeForm);
    setPrizeModalVisible(true);
  };

  const openEditPrize = (prize) => {
    setPrizeForm({ ...emptyPrizeForm, ...prize });
    setPrizeModalVisible(true);
  };

  const savePrize = async () => {
    const payload = {
      level_name: prizeForm.level_name || '',
      quantity: Number(prizeForm.quantity || 0),
      description: prizeForm.description || '',
      paid_only: Boolean(prizeForm.paid_only),
      sort_order: Number(prizeForm.sort_order || 0),
    };
    const path = `/api/lottery/admin/periods/${selectedId}/prizes`;
    const res = prizeForm.id
      ? await API.put(`${path}/${prizeForm.id}`, payload)
      : await API.post(path, payload);
    if (!res.data.success) {
      Toast.error({ content: res.data.message || t('保存失败') });
      return false;
    }
    Toast.success({ content: t('保存成功') });
    setPrizeModalVisible(false);
    await refreshAll();
    return true;
  };

  const deletePrize = (prize) => {
    Modal.confirm({
      title: t('删除奖品'),
      content: t('删除后将同时移除该奖品的兑换码，是否继续？'),
      onOk: async () => {
        const res = await API.delete(
          `/api/lottery/admin/periods/${selectedId}/prizes/${prize.id}`,
        );
        if (res.data.success) {
          Toast.success({ content: t('删除成功') });
          await refreshAll();
        } else {
          Toast.error({ content: res.data.message || t('删除失败') });
        }
      },
    });
  };

  const openImportCodes = (prize) => {
    const existingCodes = (detail?.codes || [])
      .filter((code) => code.prize_id === prize.id)
      .map((code) => code.code)
      .join('\n');
    setCodePrize(prize);
    setCodeText(existingCodes);
    setCodeModalVisible(true);
  };

  const importCodes = async () => {
    const res = await API.post(
      `/api/lottery/admin/periods/${selectedId}/prizes/${codePrize.id}/codes`,
      { codes: codeText },
    );
    if (!res.data.success) {
      Toast.error({ content: res.data.message || t('导入失败') });
      return false;
    }
    Toast.success({ content: t('导入成功') });
    setCodeModalVisible(false);
    await refreshAll();
    return true;
  };

  const periodColumns = useMemo(
    () => [
      {
        title: t('期数'),
        dataIndex: 'issue',
        render: (issue, record) => (
          <Button theme='borderless' onClick={() => setSelectedId(record.id)}>
            {t('第')} {issue} {t('期')}
          </Button>
        ),
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        render: (status) => statusTag(status, t),
      },
      {
        title: t('展示'),
        dataIndex: 'display_enabled',
        render: (enabled) =>
          enabled ? <Tag color='green'>{t('展示')}</Tag> : <Tag>{t('不展示')}</Tag>,
      },
      {
        title: t('参与/中奖'),
        render: (_, record) => `${record.entry_count || 0}/${record.winner_count || 0}`,
      },
      {
        title: t('操作'),
        render: (_, record) => (
          <Space>
            <Button icon={<IconEdit />} size='small' onClick={() => openEditPeriod(record)} />
            <Button
              icon={<PlayCircle size={16} />}
              size='small'
              type='primary'
              theme='outline'
              disabled={record.status === 'drawn'}
              onClick={() => drawPeriod(record)}
            />
            <Button
              icon={<IconDelete />}
              size='small'
              type='danger'
              theme='outline'
              disabled={record.status === 'drawn'}
              onClick={() => deletePeriod(record)}
            />
          </Space>
        ),
      },
    ],
    [t],
  );

  const prizeColumns = useMemo(
    () => [
      { title: t('奖项'), dataIndex: 'level_name' },
      { title: t('个数'), dataIndex: 'quantity' },
      {
        title: t('兑换码'),
        render: (_, record) => (
          <Tag color={record.code_count === record.quantity ? 'green' : 'red'}>
            {record.code_count || 0}/{record.quantity}
          </Tag>
        ),
      },
      {
        title: t('限制'),
        dataIndex: 'paid_only',
        render: (paidOnly) => (paidOnly ? <Tag color='amber'>{t('仅付费用户')}</Tag> : <Tag>{t('全部用户')}</Tag>),
      },
      { title: t('奖品描述'), dataIndex: 'description' },
      {
        title: t('操作'),
        render: (_, record) => (
          <Space>
            <Button icon={<IconUpload />} size='small' onClick={() => openImportCodes(record)} />
            <Button icon={<IconEdit />} size='small' onClick={() => openEditPrize(record)} />
            <Button
              icon={<IconDelete />}
              size='small'
              type='danger'
              theme='outline'
              onClick={() => deletePrize(record)}
            />
          </Space>
        ),
      },
    ],
    [t, detail],
  );

  return (
    <div className='mt-[60px] px-2 pb-8'>
      <div className='mb-4 flex flex-wrap items-center justify-between gap-3'>
        <div>
          <Title heading={3} style={{ margin: 0 }}>
            {t('抽奖管理')}
          </Title>
          <Text type='secondary'>{t('配置期数、奖品、兑换码，并在截止后手动开奖')}</Text>
        </div>
        <Space>
          <Button icon={<RefreshCw size={16} />} onClick={refreshAll}>
            {t('刷新')}
          </Button>
          <Button type='primary' icon={<IconPlus />} onClick={openCreatePeriod}>
            {t('新建期数')}
          </Button>
        </Space>
      </div>

      <div className='grid grid-cols-1 xl:grid-cols-[minmax(360px,0.9fr)_minmax(520px,1.1fr)] gap-4'>
        <Card bodyStyle={{ padding: 0 }}>
          <Table
            loading={loading}
            columns={periodColumns}
            dataSource={periods}
            rowKey='id'
            pagination={false}
            empty={<Empty description={t('暂无抽奖期数')} />}
          />
        </Card>

        <Card>
          {selectedPeriod ? (
            <>
              <div className='mb-4 flex flex-wrap items-start justify-between gap-3'>
                <div>
                  <div className='flex items-center gap-2'>
                    <Gift size={20} color='#c92130' />
                    <Text strong>
                      {t('第')} {selectedPeriod.issue} {t('期')} {selectedPeriod.title}
                    </Text>
                    {statusTag(selectedPeriod.status, t)}
                  </div>
                  <div className='mt-2 text-sm text-[var(--semi-color-text-2)]'>
                    {timestamp2string(selectedPeriod.start_time)} - {timestamp2string(selectedPeriod.end_time)}
                  </div>
                </div>
                <Button
                  type='primary'
                  icon={<Trophy size={16} />}
                  disabled={selectedPeriod.status === 'drawn'}
                  onClick={openCreatePrize}
                >
                  {t('新增奖品')}
                </Button>
              </div>
              <Table
                columns={prizeColumns}
                dataSource={detail?.prizes || []}
                rowKey='id'
                pagination={false}
                empty={<Empty description={t('暂无奖品')} />}
              />
            </>
          ) : (
            <Empty description={t('请选择抽奖期数')} />
          )}
        </Card>
      </div>

      <Modal
        title={periodForm.id ? t('编辑期数') : t('新建期数')}
        visible={periodModalVisible}
        onCancel={() => setPeriodModalVisible(false)}
        onOk={savePeriod}
        okText={t('保存')}
      >
        <Space vertical align='start' spacing='medium' style={{ width: '100%' }}>
          <InputNumber
            value={periodForm.issue}
            min={0}
            prefix={t('期数')}
            onChange={(value) => setPeriodForm((prev) => ({ ...prev, issue: value }))}
            style={{ width: '100%' }}
          />
          <Input
            value={periodForm.title}
            prefix={t('标题')}
            onChange={(value) => setPeriodForm((prev) => ({ ...prev, title: value }))}
          />
          <DatePicker
            type='dateTime'
            value={periodForm.start_time ? new Date(periodForm.start_time * 1000) : undefined}
            placeholder={t('启动时间')}
            onChange={(value) =>
              setPeriodForm((prev) => ({ ...prev, start_time: parseDateToTimestamp(value) }))
            }
            style={{ width: '100%' }}
          />
          <DatePicker
            type='dateTime'
            value={periodForm.end_time ? new Date(periodForm.end_time * 1000) : undefined}
            placeholder={t('截止时间')}
            onChange={(value) =>
              setPeriodForm((prev) => ({ ...prev, end_time: parseDateToTimestamp(value) }))
            }
            style={{ width: '100%' }}
          />
          <div className='flex items-center gap-3'>
            <Switch
              checked={periodForm.display_enabled}
              onChange={(checked) =>
                setPeriodForm((prev) => ({ ...prev, display_enabled: checked }))
              }
            />
            <Text>{t('前台展示该期')}</Text>
          </div>
        </Space>
      </Modal>

      <Modal
        title={prizeForm.id ? t('编辑奖品') : t('新增奖品')}
        visible={prizeModalVisible}
        onCancel={() => setPrizeModalVisible(false)}
        onOk={savePrize}
        okText={t('保存')}
      >
        <Space vertical align='start' spacing='medium' style={{ width: '100%' }}>
          <Input
            value={prizeForm.level_name}
            prefix={t('奖项')}
            placeholder={t('例如：一等奖')}
            onChange={(value) => setPrizeForm((prev) => ({ ...prev, level_name: value }))}
          />
          <InputNumber
            value={prizeForm.quantity}
            min={1}
            prefix={t('个数')}
            onChange={(value) => setPrizeForm((prev) => ({ ...prev, quantity: value }))}
            style={{ width: '100%' }}
          />
          <InputNumber
            value={prizeForm.sort_order}
            prefix={t('排序')}
            onChange={(value) => setPrizeForm((prev) => ({ ...prev, sort_order: value }))}
            style={{ width: '100%' }}
          />
          <TextArea
            value={prizeForm.description}
            placeholder={t('奖品描述')}
            autosize
            onChange={(value) => setPrizeForm((prev) => ({ ...prev, description: value }))}
          />
          <div className='flex items-center gap-3'>
            <Switch
              checked={prizeForm.paid_only}
              onChange={(checked) => setPrizeForm((prev) => ({ ...prev, paid_only: checked }))}
            />
            <Text>{t('仅限付费用户')}</Text>
          </div>
        </Space>
      </Modal>

      <Modal
        title={`${t('导入兑换码')} - ${codePrize?.level_name || ''}`}
        visible={codeModalVisible}
        onCancel={() => setCodeModalVisible(false)}
        onOk={importCodes}
        okText={t('导入')}
      >
        <Text type='secondary'>
          {t('每行一个兑换码，数量必须等于该奖项个数')}
        </Text>
        <TextArea
          value={codeText}
          autosize={{ minRows: 8, maxRows: 16 }}
          placeholder={t('请输入兑换码')}
          onChange={setCodeText}
          style={{ marginTop: 12 }}
        />
      </Modal>
    </div>
  );
};

export default LotteryAdmin;
