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
import { Button, Card, Empty, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useActualTheme } from '../../context/Theme';
import { API, showError } from '../../helpers';
import {
  ArrowRight,
  ChevronRight,
  Clock,
  Crown,
  Layers,
  Shield,
  Sparkles,
  TrendingUp,
  Zap,
} from 'lucide-react';

const { Text } = Typography;
const textToneTokens = {
  heading: 'text-gray-900 dark:text-gray-100',
  primary: 'text-gray-700 dark:text-gray-100',
  secondary: 'text-gray-600 dark:text-gray-300',
  tertiary: 'text-gray-500 dark:text-gray-400',
};

function formatMoney(value) {
  const amount = Number(value || 0);
  return `$${amount.toFixed(2)}`;
}

function getChannelText(level) {
  if (level?.channel_text) return level.channel_text;
  if (!Array.isArray(level?.channel) || level.channel.length === 0) return '所有渠道';
  return `${level.channel.length}个渠道`;
}

function getRateText(rate) {
  if (!rate || rate <= 0) return '无限制';
  return `${rate} 次/分钟`;
}

function getDiscountText(level) {
  if (level?.discount_text) return level.discount_text;
  const discount = Number(level?.discount || 0);
  if (discount <= 0) return '无折扣';
  return `${Math.round(discount * 100)}% OFF`;
}

function LevelIcon({ level, size = 22 }) {
  if (level?.icon) {
    return (
      <img
        src={level.icon}
        alt={level.level || 'level-icon'}
        style={{ width: size, height: size, objectFit: 'cover', borderRadius: 8 }}
      />
    );
  }
  return <Shield size={size} />;
}

const UserLevelPage = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const actualTheme = useActualTheme();
  const isDark = actualTheme === 'dark';
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState(null);

  const loadData = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/user/self/user-level');
      const { success, data: payload, message } = res.data;
      if (!success) {
        showError(message || t('加载失败'));
        return;
      }
      setData(payload);
    } catch {
      showError(t('加载失败'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const levels = useMemo(() => data?.levels || [], [data]);
  const current = data?.current || null;
  const next = data?.next || null;

  if (loading) {
    return (
      <div className='mt-[60px] px-3 py-4'>
        <Spin spinning />
      </div>
    );
  }

  return (
    <div className='mt-[60px] px-3 py-4'>
      <div className='mx-auto max-w-7xl space-y-5'>
        <Card
          bodyStyle={{ padding: 0 }}
          style={{
            overflow: 'hidden',
            borderRadius: 20,
            background:
              'linear-gradient(135deg, rgb(90, 98, 118) 0%, rgb(132, 141, 164) 58%, rgb(190, 198, 218) 100%)',
            border: 'none',
          }}
        >
          <div className='p-6 text-white'>
            <div className='flex flex-col gap-5 md:flex-row md:items-center md:justify-between'>
              <div className='space-y-3'>
                <Tag
                  size='small'
                  color='white'
                  style={{ background: 'rgba(255,255,255,0.2)', color: '#fff' }}
                  prefixIcon={<Crown size={12} />}
                >
                  {t('当前等级')}
                </Tag>
                <div className='flex items-center gap-3'>
                  <div
                    className='flex items-center justify-center rounded-xl bg-white/85'
                    style={{ width: 52, height: 52, color: '#525f7f' }}
                  >
                    <LevelIcon level={current} size={24} />
                  </div>
                  <div>
                    <div className='text-2xl font-bold'>
                      {current?.level || data?.user_group || '-'}
                    </div>
                    <Text style={{ color: 'rgba(255,255,255,0.82)' }}>
                      {t('累计充值')}: {formatMoney(data?.total_recharge)}
                    </Text>
                  </div>
                </div>
                <div className='flex flex-wrap gap-2'>
                  <Tag color='white' style={{ background: 'rgba(255,255,255,0.15)', color: '#fff' }} prefixIcon={<Sparkles size={12} />}>
                    {getDiscountText(current)}
                  </Tag>
                  <Tag color='white' style={{ background: 'rgba(255,255,255,0.15)', color: '#fff' }} prefixIcon={<Layers size={12} />}>
                    {getChannelText(current)}
                  </Tag>
                  <Tag color='white' style={{ background: 'rgba(255,255,255,0.15)', color: '#fff' }} prefixIcon={<Clock size={12} />}>
                    {getRateText(current?.rate)}
                  </Tag>
                </div>
              </div>

              <div className='w-full rounded-2xl bg-white/90 p-4 md:w-[290px] dark:bg-slate-900/80 dark:ring-1 dark:ring-slate-700/70'>
                <div className={`mb-2 flex items-center justify-between ${textToneTokens.primary}`}>
                  <span className='text-sm'>{t('升级到')}</span>
                  <span className='flex items-center gap-1 font-semibold'>
                    {next?.level || t('最高等级')}
                    {next ? <ChevronRight size={15} /> : null}
                  </span>
                </div>
                <div className='h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-slate-700'>
                  <div
                    className='h-full rounded-full'
                    style={{
                      width: `${Math.max(0, Math.min(Number(data?.progress_percent || 0), 100))}%`,
                      background: 'linear-gradient(90deg,#3b82f6,#93c5fd)',
                    }}
                  />
                </div>
                <div className={`mt-2 flex items-center justify-between text-xs ${textToneTokens.tertiary}`}>
                  <span>{formatMoney(data?.current_recharge || 0)}</span>
                  <span>{formatMoney(data?.next_recharge || data?.current_recharge || 0)}</span>
                </div>
                <div className={`mt-3 flex items-center gap-2 text-xs ${textToneTokens.secondary}`}>
                  <TrendingUp size={13} />
                  {next ? `${t('还需')}: ${formatMoney(data?.remaining_recharge || 0)}` : t('已达最高等级')}
                </div>
              </div>
            </div>
          </div>
        </Card>

        <Card title={t('所有等级')} headerLine={false}>
          {!levels.length ? (
            <Empty description={t('暂无等级配置')} />
          ) : (
            <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4'>
              {levels.map((level) => {
                const isCurrent =
                  (current && current.id === level.id) ||
                  (!current && data?.user_group === level.level);

                return (
                  <Card
                    key={level.id}
                    bodyStyle={{ padding: 16 }}
                    style={{
                      borderRadius: 16,
                      border: isCurrent
                        ? '2px solid #3b82f6'
                        : isDark
                          ? '1px solid rgba(148, 163, 184, 0.35)'
                          : '1px solid #e7e8eb',
                      background: isCurrent
                        ? isDark
                          ? 'rgba(59,130,246,0.18)'
                          : 'rgba(59,130,246,0.06)'
                        : isDark
                          ? 'rgba(15, 23, 42, 0.9)'
                          : '#fff',
                      color: isDark ? '#e5e7eb' : undefined,
                    }}
                  >
                    <div className='space-y-3'>
                      <div className='flex items-start justify-between gap-2'>
                        <div className='flex items-center gap-2'>
                          <div
                            className='flex items-center justify-center rounded-xl bg-gray-50 dark:bg-slate-700/70'
                            style={{
                              width: 40,
                              height: 40,
                              color: isDark ? '#cbd5e1' : '#5f6f90',
                            }}
                          >
                            <LevelIcon level={level} size={20} />
                          </div>
                          <div>
                            <div className={`font-semibold ${textToneTokens.heading}`}>{level.level}</div>
                            <Text
                              type='tertiary'
                              size='small'
                              style={isDark ? { color: 'rgba(203, 213, 225, 0.88)' } : undefined}
                            >
                              {t('累计充值')}: {formatMoney(level.recharge)}
                            </Text>
                          </div>
                        </div>
                        {isCurrent ? <Tag color='blue'>{t('当前')}</Tag> : null}
                      </div>

                      <div className='space-y-2 text-sm'>
                        <div className={`flex items-center justify-between ${textToneTokens.secondary}`}>
                          <span className='flex items-center gap-1'>
                            <Zap size={14} />
                            {t('折扣')}
                          </span>
                          <span className='font-medium'>{getDiscountText(level)}</span>
                        </div>
                        <div className={`flex items-center justify-between ${textToneTokens.secondary}`}>
                          <span className='flex items-center gap-1'>
                            <Layers size={14} />
                            {t('可用渠道')}
                          </span>
                          <span className='font-medium'>{getChannelText(level)}</span>
                        </div>
                        <div className={`flex items-center justify-between ${textToneTokens.secondary}`}>
                          <span className='flex items-center gap-1'>
                            <Clock size={14} />
                            {t('速率限制')}
                          </span>
                          <span className='font-medium'>{getRateText(level.rate)}</span>
                        </div>
                      </div>

                      {!isCurrent ? (
                        <Button
                          block
                          theme='solid'
                          type='primary'
                          onClick={() => navigate('/console/topup')}
                          icon={<ArrowRight size={14} />}
                        >
                          {t('立即升级')}
                        </Button>
                      ) : null}
                    </div>
                  </Card>
                );
              })}
            </div>
          )}
        </Card>
      </div>
    </div>
  );
};

export default UserLevelPage;
