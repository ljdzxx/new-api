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
  Banner,
  Button,
  Card,
  Empty,
  Input,
  Modal,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import {
  CircleAlert,
  Clock3,
  Download,
  FileText,
  History,
  Mail,
  Receipt,
  RefreshCw,
  Wallet,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import CardTable from '../../components/common/ui/CardTable';

const { Text, Title } = Typography;

const TOPUP_STATUS_CONFIG = {
  insufficient: { color: 'grey', key: '金额不足' },
  available: { color: 'blue', key: '可申请' },
  applied: { color: 'green', key: '已申请' },
  issued: { color: 'teal', key: '已开具' },
};

const INVOICE_STATUS_CONFIG = {
  1: { color: 'orange', key: '未处理' },
  2: { color: 'green', key: '已开具' },
  3: { color: 'red', key: '已拒绝' },
};

const PAYMENT_METHOD_MAP = {
  alipay: '支付宝',
  wxpay: '微信',
  redemption: '兑换码',
};

function formatMoney(money) {
  return `CNY ${Number(money || 0).toFixed(2)}`;
}

const InvoicePage = () => {
  const { t } = useTranslation();
  const isMobile = useIsMobile();

  const [config, setConfig] = useState({ min_amount: 300, online_time: '' });
  const [lastInvoiceProfile, setLastInvoiceProfile] = useState(null);

  // 可申请充值
  const [topups, setTopups] = useState([]);
  const [topupsTotal, setTopupsTotal] = useState(0);
  const [topupsPage, setTopupsPage] = useState(1);
  const [topupsPageSize, setTopupsPageSize] = useState(20);
  const [topupsLoading, setTopupsLoading] = useState(false);

  // 申请记录
  const [invoices, setInvoices] = useState([]);
  const [invoicesTotal, setInvoicesTotal] = useState(0);
  const [invoicesPage, setInvoicesPage] = useState(1);
  const [invoicesPageSize, setInvoicesPageSize] = useState(20);
  const [invoicesLoading, setInvoicesLoading] = useState(false);

  // 申请开票弹窗
  const [applyTarget, setApplyTarget] = useState(null);
  const [applyTitle, setApplyTitle] = useState('');
  const [applyTaxNo, setApplyTaxNo] = useState('');
  const [applyEmails, setApplyEmails] = useState('');
  const [applyLoading, setApplyLoading] = useState(false);

  const loadConfig = async () => {
    try {
      const res = await API.get('/api/user/invoice/config');
      if (res.data.success) {
        const data = res.data.data || {};
        setConfig(data);
        setLastInvoiceProfile(data.last_invoice_profile || null);
      }
    } catch (e) {
      // 配置加载失败时使用默认值
    }
  };

  const loadTopups = async (page = topupsPage, pageSize = topupsPageSize) => {
    setTopupsLoading(true);
    try {
      const res = await API.get(
        `/api/user/invoice/topups?p=${page}&page_size=${pageSize}`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setTopups(data.items || []);
        setTopupsTotal(data.total || 0);
      } else {
        showError(message || t('加载失败'));
      }
    } catch (e) {
      showError(t('加载失败'));
    } finally {
      setTopupsLoading(false);
    }
  };

  const loadInvoices = async (
    page = invoicesPage,
    pageSize = invoicesPageSize,
  ) => {
    setInvoicesLoading(true);
    try {
      const res = await API.get(
        `/api/user/invoice/self?p=${page}&page_size=${pageSize}`,
      );
      const { success, message, data } = res.data;
      if (success) {
        setInvoices(data.items || []);
        setInvoicesTotal(data.total || 0);
      } else {
        showError(message || t('加载失败'));
      }
    } catch (e) {
      showError(t('加载失败'));
    } finally {
      setInvoicesLoading(false);
    }
  };

  useEffect(() => {
    loadConfig();
  }, []);

  useEffect(() => {
    loadTopups(topupsPage, topupsPageSize);
  }, [topupsPage, topupsPageSize]);

  useEffect(() => {
    loadInvoices(invoicesPage, invoicesPageSize);
  }, [invoicesPage, invoicesPageSize]);

  const openApplyModal = (record) => {
    setApplyTarget(record);
    setApplyTitle(lastInvoiceProfile?.title || '');
    setApplyTaxNo(lastInvoiceProfile?.tax_no || '');
    setApplyEmails(lastInvoiceProfile?.emails || '');
  };

  const submitApply = async () => {
    if (!applyTitle.trim()) {
      Toast.warning({ content: t('请输入公司或个人抬头') });
      return;
    }
    if (!applyEmails.trim()) {
      Toast.warning({ content: t('请输入一个或多个收票邮箱') });
      return;
    }
    setApplyLoading(true);
    try {
      const res = await API.post('/api/user/invoice/apply', {
        top_up_id: applyTarget.id,
        title: applyTitle.trim(),
        tax_no: applyTaxNo.trim(),
        emails: applyEmails.trim(),
      });
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('开票申请已提交') });
        setLastInvoiceProfile({
          title: applyTitle.trim(),
          tax_no: applyTaxNo.trim(),
          emails: applyEmails.trim(),
        });
        setApplyTarget(null);
        loadTopups();
        setInvoicesPage(1);
        loadInvoices(1, invoicesPageSize);
      } else {
        showError(message || t('提交失败'));
      }
    } catch (e) {
      showError(t('提交失败'));
    } finally {
      setApplyLoading(false);
    }
  };

  const downloadInvoice = async (invoiceId) => {
    try {
      const res = await API.get(`/api/user/invoice/download/${invoiceId}`);
      const { success, message, data } = res.data;
      if (success && data?.url) {
        window.open(data.url, '_blank');
      } else {
        showError(message || t('获取下载链接失败'));
      }
    } catch (e) {
      showError(t('获取下载链接失败'));
    }
  };

  const renderTopupStatus = (record) => {
    const status = record.invoice_status;
    if (status === 'available') {
      return (
        <Button
          size='small'
          type='primary'
          onClick={() => openApplyModal(record)}
        >
          {t('申请开票')}
        </Button>
      );
    }
    const cfg = TOPUP_STATUS_CONFIG[status] || {
      color: 'grey',
      key: status,
    };
    return (
      <Tag color={cfg.color} shape='circle' size='small'>
        {status === 'insufficient'
          ? `${t(cfg.key)} ${config.min_amount}`
          : t(cfg.key)}
      </Tag>
    );
  };

  const topupColumns = useMemo(
    () => [
      {
        title: t('充值订单'),
        dataIndex: 'id',
        key: 'id',
        render: (id) => <Text strong>#{id}</Text>,
      },
      {
        title: t('充值金额'),
        dataIndex: 'money',
        key: 'money',
        render: (money) => <Text>{formatMoney(money)}</Text>,
      },
      {
        title: t('支付方式'),
        dataIndex: 'payment_method',
        key: 'payment_method',
        render: (paymentMethod) => {
          const displayName = PAYMENT_METHOD_MAP[paymentMethod];
          return (
            <Text>{displayName ? t(displayName) : paymentMethod || '-'}</Text>
          );
        },
      },
      {
        title: t('完成时间'),
        dataIndex: 'complete_time',
        key: 'complete_time',
        render: (time) => timestamp2string(time),
      },
      {
        title: t('状态'),
        key: 'invoice_status',
        align: 'right',
        render: (_, record) => renderTopupStatus(record),
      },
    ],
    [t, config.min_amount],
  );

  const invoiceColumns = useMemo(
    () => [
      {
        title: t('充值订单'),
        dataIndex: 'top_up_id',
        key: 'top_up_id',
        render: (id) => <Text strong>#{id}</Text>,
      },
      {
        title: t('公司抬头'),
        dataIndex: 'title',
        key: 'title',
        render: (text) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 260 }}>
            {text}
          </Text>
        ),
      },
      {
        title: t('收票邮箱'),
        dataIndex: 'emails',
        key: 'emails',
        render: (text) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 220 }}>
            {(text || '').split(';').join(', ')}
          </Text>
        ),
      },
      {
        title: t('申请时间'),
        dataIndex: 'created_time',
        key: 'created_time',
        render: (time) => timestamp2string(time),
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        align: 'right',
        render: (status, record) => {
          const cfg = INVOICE_STATUS_CONFIG[status] || {
            color: 'grey',
            key: status,
          };
          return (
            <span className='flex items-center justify-end gap-2'>
              <Tag color={cfg.color} shape='circle' size='small'>
                {t(cfg.key)}
              </Tag>
              {status === 2 && (
                <Button
                  size='small'
                  type='primary'
                  theme='outline'
                  icon={<Download size={14} />}
                  onClick={() => downloadInvoice(record.id)}
                >
                  {t('下载')}
                </Button>
              )}
            </span>
          );
        },
      },
    ],
    [t],
  );

  const emptyPlaceholder = (text) => (
    <Empty
      image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
      darkModeImage={
        <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
      }
      description={text}
      style={{ padding: 30 }}
    />
  );

  return (
    <div className='mt-[60px] px-2 pb-24'>
      {/* 顶部说明 */}
      <Card className='!rounded-2xl mb-4' bodyStyle={{ padding: '20px 24px' }}>
        <div className='flex items-center gap-3'>
          <div className='w-11 h-11 rounded-full bg-gradient-to-r from-blue-500 to-cyan-500 flex items-center justify-center text-white shrink-0 shadow-sm'>
            <Receipt size={22} />
          </div>
          <div>
            <Title heading={4} style={{ margin: 0 }}>
              {t('自助发票')}
            </Title>
            <Text type='secondary' className='block mt-1'>
              {t('查看符合条件的充值记录并提交开票申请。')}
            </Text>
          </div>
        </div>
        <div className='flex flex-wrap items-center gap-x-8 gap-y-2 mt-4'>
          <Text
            type='tertiary'
            size='small'
            className='flex items-center gap-1'
          >
            <Clock3 size={14} />
            {t('发票需要大约1-3个工作日会发送到您提交邮箱')}
          </Text>
          <Text
            type='tertiary'
            size='small'
            className='flex items-center gap-1'
          >
            <FileText size={14} />
            {t('开票内容为研发服务')}
          </Text>
        </div>
      </Card>

      {/* 可申请充值 */}
      <Card
        className='!rounded-2xl mb-4'
        bodyStyle={{ padding: '20px 24px' }}
        title={
          <div className='flex items-center justify-between w-full'>
            <div className='flex items-center gap-2'>
              <Wallet size={16} className='text-blue-500' />
              <div>
                <Text strong>{t('可申请充值')}</Text>
                <Text type='tertiary' size='small' className='block'>
                  {t('单笔充值金额达到')} {config.min_amount}{' '}
                  {t('才可申请开票')}
                </Text>
              </div>
            </div>
            <Button
              theme='outline'
              type='tertiary'
              icon={<RefreshCw size={15} />}
              loading={topupsLoading}
              onClick={() => loadTopups()}
            />
          </div>
        }
      >
        <CardTable
          columns={topupColumns}
          dataSource={topups}
          loading={topupsLoading}
          rowKey='id'
          size='small'
          className='rounded-xl overflow-hidden'
          pagination={{
            currentPage: topupsPage,
            pageSize: topupsPageSize,
            total: topupsTotal,
            showSizeChanger: true,
            pageSizeOpts: [10, 20, 50, 100],
            onPageChange: (p) => setTopupsPage(p),
            onPageSizeChange: (s) => {
              setTopupsPageSize(s);
              setTopupsPage(1);
            },
          }}
          empty={emptyPlaceholder(t('暂无可开票的充值记录'))}
        />
      </Card>

      {/* 申请记录 */}
      <Card
        className='!rounded-2xl'
        bodyStyle={{ padding: '20px 24px' }}
        title={
          <div className='flex items-center gap-2'>
            <History size={16} className='text-teal-500' />
            <div>
              <Text strong>{t('申请记录')}</Text>
              <Text type='tertiary' size='small' className='block'>
                {t('发票将在处理后通过您提交的邮箱发送，也可在此处下载。')}
              </Text>
            </div>
          </div>
        }
      >
        <CardTable
          columns={invoiceColumns}
          dataSource={invoices}
          loading={invoicesLoading}
          rowKey='id'
          size='small'
          className='rounded-xl overflow-hidden'
          pagination={{
            currentPage: invoicesPage,
            pageSize: invoicesPageSize,
            total: invoicesTotal,
            showSizeChanger: true,
            pageSizeOpts: [10, 20, 50, 100],
            onPageChange: (p) => setInvoicesPage(p),
            onPageSizeChange: (s) => {
              setInvoicesPageSize(s);
              setInvoicesPage(1);
            },
          }}
          empty={emptyPlaceholder(t('暂无开票申请记录'))}
        />
      </Card>

      {/* 申请开票弹窗 */}
      <Modal
        title={t('申请开票')}
        visible={Boolean(applyTarget)}
        onCancel={() => !applyLoading && setApplyTarget(null)}
        footer={
          <div className='flex justify-end gap-2'>
            <Button
              theme='outline'
              type='tertiary'
              onClick={() => setApplyTarget(null)}
              disabled={applyLoading}
            >
              {t('取消')}
            </Button>
            <Button
              type='primary'
              icon={<Mail size={15} />}
              loading={applyLoading}
              onClick={submitApply}
            >
              {t('提交申请')}
            </Button>
          </div>
        }
        width={isMobile ? '100vw' : 520}
      >
        <div className='space-y-2 mb-4'>
          <div className='flex justify-between'>
            <Text type='secondary'>{t('充值订单')}</Text>
            <Text strong>#{applyTarget?.id}</Text>
          </div>
          <div className='flex justify-between'>
            <Text type='secondary'>{t('充值金额')}</Text>
            <Text strong>{formatMoney(applyTarget?.money)}</Text>
          </div>
        </div>

        <Banner
          type='warning'
          closeIcon={null}
          icon={<CircleAlert size={18} />}
          className='!rounded-xl mb-4'
          description={
            <div>
              <div className='font-medium mb-1'>{t('填写注意事项')}</div>
              <ul className='list-disc pl-4 space-y-1'>
                <li>{t('税号请核对无误，错误信息会影响开票。')}</li>
                <li>{t('收票邮箱请填写正确，支持填写多个邮箱。')}</li>
              </ul>
            </div>
          }
        />

        <div className='space-y-4'>
          <div>
            <Text strong className='block mb-1'>
              {t('公司抬头')}
            </Text>
            <Input
              value={applyTitle}
              onChange={setApplyTitle}
              placeholder={t('请输入公司或个人抬头')}
              maxLength={100}
            />
          </div>
          <div>
            <Text strong className='block mb-1'>
              {t('税号')}
            </Text>
            <Input
              value={applyTaxNo}
              onChange={setApplyTaxNo}
              placeholder={t('请输入统一社会信用代码或税号')}
              maxLength={64}
            />
          </div>
          <div>
            <Text strong className='block mb-1'>
              {t('收票邮箱')}
            </Text>
            <Input
              value={applyEmails}
              onChange={setApplyEmails}
              placeholder={t('请输入一个或多个收票邮箱')}
            />
            <Text type='tertiary' size='small'>
              {t('多个邮箱请使用英文逗号、分号或空格分隔')}
            </Text>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default InvoicePage;
