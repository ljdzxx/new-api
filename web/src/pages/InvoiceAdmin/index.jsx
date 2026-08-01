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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Empty,
  Input,
  Modal,
  Select,
  Table,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';
import { IconSearch } from '@douyinfe/semi-icons';
import { FileUp, Mail, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import { useIsMobile } from '../../hooks/common/useIsMobile';

const { Text } = Typography;

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

const InvoiceAdminPage = () => {
  const { t } = useTranslation();
  const isMobile = useIsMobile();

  const [invoices, setInvoices] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [loading, setLoading] = useState(false);

  const [keyword, setKeyword] = useState('');
  const [username, setUsername] = useState('');
  const [status, setStatus] = useState(0);

  // 开具发票弹窗
  const [issueTarget, setIssueTarget] = useState(null);
  const [issueFile, setIssueFile] = useState(null);
  const [issueLoading, setIssueLoading] = useState(false);
  const fileInputRef = useRef(null);

  // 拒绝弹窗
  const [rejectTarget, setRejectTarget] = useState(null);
  const [rejectRemark, setRejectRemark] = useState('');
  const [rejectLoading, setRejectLoading] = useState(false);
  const [resendingInvoiceId, setResendingInvoiceId] = useState(null);

  const loadInvoices = async (
    currentPage = page,
    currentPageSize = pageSize,
  ) => {
    setLoading(true);
    try {
      const qs =
        `p=${currentPage}&page_size=${currentPageSize}` +
        (keyword ? `&keyword=${encodeURIComponent(keyword)}` : '') +
        (username ? `&username=${encodeURIComponent(username)}` : '') +
        (status > 0 ? `&status=${status}` : '');
      const res = await API.get(`/api/invoice/?${qs}`);
      const { success, message, data } = res.data;
      if (success) {
        setInvoices(data.items || []);
        setTotal(data.total || 0);
      } else {
        showError(message || t('加载失败'));
      }
    } catch (e) {
      showError(t('加载失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadInvoices(page, pageSize);
  }, [page, pageSize]);

  const handleSearch = () => {
    if (page !== 1) {
      setPage(1);
    } else {
      loadInvoices(1, pageSize);
    }
  };

  const openIssueModal = (record) => {
    setIssueTarget(record);
    setIssueFile(null);
  };

  const submitIssue = async () => {
    if (!issueFile) {
      Toast.warning({ content: t('请选择要上传的发票文件') });
      return;
    }
    setIssueLoading(true);
    try {
      const formData = new FormData();
      formData.append('file', issueFile);
      const res = await API.put(
        `/api/invoice/${issueTarget.id}/issue`,
        formData,
      );
      const { success, message, data } = res.data;
      if (success) {
        if (data?.email_sent === false) {
          Toast.warning({
            content:
              t('发票已开具，但邮件发送失败') +
              (data?.email_error ? `：${data.email_error}` : ''),
            duration: 5,
          });
        } else {
          Toast.success({ content: t('发票已开具并邮件通知用户') });
        }
        setIssueTarget(null);
        setIssueFile(null);
        loadInvoices();
      } else {
        showError(message || t('开具失败'));
      }
    } catch (e) {
      showError(t('开具失败'));
    } finally {
      setIssueLoading(false);
    }
  };

  const submitReject = async () => {
    setRejectLoading(true);
    try {
      const res = await API.put(`/api/invoice/${rejectTarget.id}/reject`, {
        remark: rejectRemark.trim(),
      });
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('已拒绝该申请') });
        setRejectTarget(null);
        setRejectRemark('');
        loadInvoices();
      } else {
        showError(message || t('操作失败'));
      }
    } catch (e) {
      showError(t('操作失败'));
    } finally {
      setRejectLoading(false);
    }
  };

  const resendInvoiceEmail = async (invoiceId) => {
    setResendingInvoiceId(invoiceId);
    try {
      const res = await API.post(`/api/invoice/${invoiceId}/resend-email`);
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('发票邮件已重新发送') });
      } else {
        showError(message || t('操作失败'));
      }
    } catch (e) {
      showError(t('操作失败'));
    } finally {
      setResendingInvoiceId(null);
    }
  };

  const columns = useMemo(
    () => [
      {
        title: 'ID',
        dataIndex: 'id',
        key: 'id',
        width: 70,
      },
      {
        title: t('用户'),
        dataIndex: 'username',
        key: 'username',
        width: 110,
        render: (text) => <Text>{text || '-'}</Text>,
      },
      {
        title: t('充值订单'),
        dataIndex: 'trade_no',
        key: 'trade_no',
        width: 220,
        render: (text, record) => (
          <Text
            copyable={{ content: text }}
            ellipsis={{ showTooltip: true }}
            style={{ maxWidth: 200 }}
          >
            #{record.top_up_id} {text}
          </Text>
        ),
      },
      {
        title: t('充值金额'),
        dataIndex: 'money',
        key: 'money',
        width: 110,
        render: (money) => (
          <Text type='danger'>CNY {Number(money || 0).toFixed(2)}</Text>
        ),
      },
      {
        title: t('支付方式'),
        dataIndex: 'payment_method',
        key: 'payment_method',
        width: 110,
        render: (paymentMethod) => {
          const displayName = PAYMENT_METHOD_MAP[paymentMethod];
          return (
            <Text>{displayName ? t(displayName) : paymentMethod || '-'}</Text>
          );
        },
      },
      {
        title: t('公司抬头'),
        dataIndex: 'title',
        key: 'title',
        render: (text) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 220 }}>
            {text}
          </Text>
        ),
      },
      {
        title: t('税号'),
        dataIndex: 'tax_no',
        key: 'tax_no',
        width: 150,
        render: (text) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 140 }}>
            {text || '-'}
          </Text>
        ),
      },
      {
        title: t('收票邮箱'),
        dataIndex: 'emails',
        key: 'emails',
        render: (text) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 200 }}>
            {(text || '').split(';').join(', ')}
          </Text>
        ),
      },
      {
        title: t('申请时间'),
        dataIndex: 'created_time',
        key: 'created_time',
        width: 150,
        render: (time) => timestamp2string(time),
      },
      {
        title: t('状态'),
        dataIndex: 'status',
        key: 'status',
        width: 90,
        render: (value, record) => {
          const cfg = INVOICE_STATUS_CONFIG[value] || {
            color: 'grey',
            key: value,
          };
          return (
            <Tag
              color={cfg.color}
              shape='circle'
              size='small'
              suffixIcon={undefined}
            >
              {t(cfg.key)}
            </Tag>
          );
        },
      },
      {
        title: t('操作'),
        key: 'action',
        width: 190,
        fixed: 'right',
        render: (_, record) => {
          if (record.status !== 1) {
            if (record.status === 2) {
              return (
                <Button
                  size='small'
                  theme='outline'
                  icon={<Mail size={14} />}
                  loading={resendingInvoiceId === record.id}
                  onClick={() => resendInvoiceEmail(record.id)}
                >
                  {t('重发邮件')}
                </Button>
              );
            }
            return record.status === 3 && record.remark ? (
              <Text
                type='tertiary'
                size='small'
                ellipsis={{ showTooltip: true }}
                style={{ maxWidth: 180 }}
              >
                {record.remark}
              </Text>
            ) : null;
          }
          return (
            <span className='flex items-center gap-2'>
              <Button
                size='small'
                type='primary'
                icon={<FileUp size={14} />}
                onClick={() => openIssueModal(record)}
              >
                {t('开具发票')}
              </Button>
              <Button
                size='small'
                type='danger'
                theme='outline'
                onClick={() => {
                  setRejectTarget(record);
                  setRejectRemark('');
                }}
              >
                {t('拒绝')}
              </Button>
            </span>
          );
        },
      },
    ],
    [resendingInvoiceId, t],
  );

  return (
    <div className='w-full max-w-7xl mx-auto relative min-h-screen lg:min-h-0 mt-[60px] px-2 pb-24'>
      <Card
        className='!rounded-2xl'
        bodyStyle={{ padding: '20px 24px' }}
        title={
          <div className='flex items-center justify-between w-full'>
            <div>
              <Text strong>{t('发票管理')}</Text>
              <Text type='tertiary' size='small' className='block'>
                {t('查询用户开票申请，上传开具的发票文件并邮件通知用户。')}
              </Text>
            </div>
            <Button
              theme='outline'
              type='tertiary'
              icon={<RefreshCw size={15} />}
              loading={loading}
              onClick={() => loadInvoices()}
            />
          </div>
        }
      >
        <div className='flex flex-wrap items-center gap-2 mb-4'>
          <Input
            prefix={<IconSearch />}
            placeholder={t('搜索充值订单号 / 公司抬头')}
            value={keyword}
            onChange={setKeyword}
            onEnterPress={handleSearch}
            showClear
            style={{ width: isMobile ? '100%' : 240 }}
          />
          <Input
            placeholder={t('搜索用户名')}
            value={username}
            onChange={setUsername}
            onEnterPress={handleSearch}
            showClear
            style={{ width: isMobile ? '100%' : 180 }}
          />
          <Select
            value={status}
            onChange={(v) => setStatus(v)}
            style={{ width: isMobile ? '100%' : 140 }}
            optionList={[
              { label: t('全部状态'), value: 0 },
              { label: t('未处理'), value: 1 },
              { label: t('已开具'), value: 2 },
              { label: t('已拒绝'), value: 3 },
            ]}
          />
          <Button type='primary' onClick={handleSearch}>
            {t('查询')}
          </Button>
        </div>

        <Table
          columns={columns}
          dataSource={invoices}
          loading={loading}
          rowKey='id'
          size='small'
          scroll={{ x: 1500 }}
          pagination={{
            currentPage: page,
            pageSize: pageSize,
            total: total,
            showSizeChanger: true,
            pageSizeOpts: [10, 20, 50, 100],
            onPageChange: (p) => setPage(p),
            onPageSizeChange: (s) => {
              setPageSize(s);
              setPage(1);
            },
          }}
          empty={
            <Empty
              image={
                <IllustrationNoResult style={{ width: 150, height: 150 }} />
              }
              darkModeImage={
                <IllustrationNoResultDark style={{ width: 150, height: 150 }} />
              }
              description={t('暂无开票申请')}
              style={{ padding: 30 }}
            />
          }
        />
      </Card>

      {/* 开具发票弹窗 */}
      <Modal
        title={t('开具发票')}
        visible={Boolean(issueTarget)}
        onCancel={() => !issueLoading && setIssueTarget(null)}
        onOk={submitIssue}
        confirmLoading={issueLoading}
        okText={t('上传并开具')}
        cancelText={t('取消')}
        width={isMobile ? '100vw' : 520}
      >
        <div className='space-y-2 mb-4'>
          <div className='flex justify-between'>
            <Text type='secondary'>{t('充值订单')}</Text>
            <Text strong>
              #{issueTarget?.top_up_id} {issueTarget?.trade_no}
            </Text>
          </div>
          <div className='flex justify-between'>
            <Text type='secondary'>{t('充值金额')}</Text>
            <Text strong>CNY {Number(issueTarget?.money || 0).toFixed(2)}</Text>
          </div>
          <div className='flex justify-between'>
            <Text type='secondary'>{t('公司抬头')}</Text>
            <Text strong>{issueTarget?.title}</Text>
          </div>
          <div className='flex justify-between'>
            <Text type='secondary'>{t('税号')}</Text>
            <Text strong>{issueTarget?.tax_no || '-'}</Text>
          </div>
          <div className='flex justify-between'>
            <Text type='secondary'>{t('收票邮箱')}</Text>
            <Text strong>
              {(issueTarget?.emails || '').split(';').join(', ')}
            </Text>
          </div>
        </div>

        <Banner
          type='info'
          closeIcon={null}
          className='!rounded-xl mb-4'
          description={t(
            '发票文件将上传至 Cloudflare R2 存储，开具后系统会向收票邮箱发送含下载链接的通知邮件。请先在「系统设置-运营设置-发票设置」中完成 R2 配置。',
          )}
        />

        <input
          ref={fileInputRef}
          type='file'
          accept='.pdf,.jpg,.jpeg,.png'
          style={{ display: 'none' }}
          onChange={(e) => {
            const file = e.target.files?.[0] || null;
            setIssueFile(file);
            e.target.value = '';
          }}
        />
        <div className='flex items-center gap-3'>
          <Button
            theme='outline'
            type='tertiary'
            icon={<FileUp size={15} />}
            onClick={() => fileInputRef.current?.click()}
          >
            {t('选择发票文件')}
          </Button>
          <Text type={issueFile ? 'primary' : 'tertiary'} size='small'>
            {issueFile ? issueFile.name : t('支持 PDF、JPG、PNG，20MB 以内')}
          </Text>
        </div>
      </Modal>

      {/* 拒绝弹窗 */}
      <Modal
        title={t('拒绝开票申请')}
        visible={Boolean(rejectTarget)}
        onCancel={() => !rejectLoading && setRejectTarget(null)}
        onOk={submitReject}
        confirmLoading={rejectLoading}
        okButtonProps={{ type: 'danger' }}
        okText={t('确认拒绝')}
        cancelText={t('取消')}
        width={isMobile ? '100vw' : 480}
      >
        <Text type='warning' className='block mb-3'>
          {t('拒绝后用户可修改信息重新提交申请，请填写拒绝原因。')}
        </Text>
        <TextArea
          value={rejectRemark}
          onChange={setRejectRemark}
          maxCount={200}
          autosize={{ minRows: 3, maxRows: 5 }}
          placeholder={t('请填写拒绝原因（可选）')}
        />
      </Modal>
    </div>
  );
};

export default InvoiceAdminPage;
