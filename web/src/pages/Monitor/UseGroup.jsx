import React, { useEffect, useState } from 'react';
import {
  Modal,
  Button,
  Input,
  Table,
  Radio,
  Tag,
  Space,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import EditTokenModal from '../../components/table/tokens/modals/EditTokenModal';

export default function UseGroup({ group, onClose }) {
  const { t } = useTranslation();
  const [tokens, setTokens] = useState([]),
    [page, setPage] = useState(1),
    [total, setTotal] = useState(0);
  const [search, setSearch] = useState(''),
    [selected, setSelected] = useState(null);
  const [loading, setLoading] = useState(false),
    [creating, setCreating] = useState(false),
    [allowed, setAllowed] = useState(false);
  useEffect(() => {
    API.get('/api/user/self/groups')
      .then(({ data }) =>
        setAllowed(data.success && Object.hasOwn(data.data, group.name)),
      )
      .catch((e) => showError(e.message));
  }, [group.name]);
  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(async () => {
      setLoading(true);
      try {
        const { data } = await API.get('/api/token/search', {
          params: { keyword: search, p: page, page_size: 10 },
        });
        if (!data.success) throw new Error(data.message);
        if (!cancelled) {
          setTokens(data.data.items || []);
          setTotal(data.data.total || 0);
        }
      } catch (e) {
        if (!cancelled) showError(e.message);
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, 250);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [search, page]);
  const change = async () => {
    setLoading(true);
    try {
      const { data } = await API.put('/api/monitor/token-group', {
        token_id: selected,
        group: group.name,
      });
      if (!data.success) throw new Error(data.message);
      showSuccess(t('令牌更新成功！'));
      onClose();
    } catch (e) {
      showError(e.message);
    } finally {
      setLoading(false);
    }
  };
  return (
    <>
      <Modal
        title={`${t('使用分组')}：${group.name}`}
        visible
        onCancel={onClose}
        width={850}
        footer={
          <Space>
            <Button onClick={onClose}>{t('取消')}</Button>
            <Button
              theme='solid'
              disabled={!selected || !allowed}
              loading={loading}
              onClick={change}
            >
              {t('切换到此分组')}
            </Button>
          </Space>
        }
      >
        <div className='flex justify-between mb-4'>
          <Tag>{group.ratio ?? '—'}x</Tag>
          <Button disabled={!allowed} onClick={() => setCreating(true)}>
            {t('新建令牌')}
          </Button>
        </div>
        {!allowed && <p>{t('当前账户不可使用此分组')}</p>}
        <Input
          placeholder={t('搜索令牌名称')}
          value={search}
          onChange={(v) => {
            setSearch(v);
            setPage(1);
            setSelected(null);
          }}
        />
        <Table
          dataSource={tokens}
          rowKey='id'
          loading={loading}
          size='small'
          pagination={{
            currentPage: page,
            pageSize: 10,
            total,
            onPageChange: (p) => {
              setPage(p);
              setSelected(null);
            },
          }}
          columns={[
            {
              title: t('选择'),
              render: (_, row) => (
                <Radio
                  aria-label={`${t('选择')} ${row.name}`}
                  checked={selected === row.id}
                  disabled={row.group === group.name || !allowed}
                  onChange={() => setSelected(row.id)}
                />
              ),
            },
            { title: t('名称'), dataIndex: 'name' },
            { title: t('令牌'), dataIndex: 'key' },
            {
              title: t('状态'),
              render: (_, row) => (row.status === 1 ? t('启用') : t('禁用')),
            },
            {
              title: t('当前分组'),
              render: (_, row) =>
                row.group === group.name ? (
                  <Tag color='green'>{t('当前分组')}</Tag>
                ) : (
                  row.group
                ),
            },
            {
              title: t('最后使用时间'),
              render: (_, row) =>
                row.accessed_time ? timestamp2string(row.accessed_time) : '—',
            },
          ]}
        />
      </Modal>
      {creating && (
        <EditTokenModal
          editingToken={{}}
          defaultGroup={group.name}
          createEndpoint='/api/monitor/token'
          visiable
          handleClose={() => setCreating(false)}
          refresh={onClose}
        />
      )}
    </>
  );
}
