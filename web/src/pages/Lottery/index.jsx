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
  Button,
  Empty,
  Select,
  Spin,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import {
  CalendarDays,
  CheckCircle2,
  ChevronRight,
  Clock,
  Crown,
  Gift,
  Medal,
  PartyPopper,
  ShieldCheck,
  Sparkles,
  Star,
  Ticket,
  Trophy,
  Users,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { API, timestamp2string } from '../../helpers';
import './index.css';

const { Text, Title } = Typography;

const SCRATCH_THRESHOLD = 0.45;

const getLocalUser = () => {
  try {
    return JSON.parse(localStorage.getItem('user') || 'null');
  } catch (error) {
    return null;
  }
};

const formatCountdown = (seconds) => {
  const safe = Math.max(0, Number(seconds || 0));
  const days = Math.floor(safe / 86400);
  const hours = Math.floor((safe % 86400) / 3600);
  const minutes = Math.floor((safe % 3600) / 60);
  const secs = safe % 60;
  if (days > 0) {
    return `${days}天 ${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
  }
  return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
};

const resolveStage = (period, now) => {
  if (!period) return 'empty';
  if (period.status === 'drawn') return 'drawn';
  if (period.status === 'drawing') return 'drawing';
  if (now < period.start_time) return 'pending';
  if (now < period.end_time) return 'running';
  return 'drawing';
};

const PrizeQuantityGrid = ({ quantity, total, label }) => {
  const safeTotal = Math.max(1, Number(total || 0));
  const safeQuantity = Math.max(0, Math.min(Number(quantity || 0), safeTotal));
  const cellWidth = safeTotal > 120 ? 2 : safeTotal > 80 ? 3 : safeTotal > 40 ? 5 : 11;
  const cellGap = safeTotal > 80 ? 1 : 2;

  return (
    <div className='prize-quantity-box' aria-label={label}>
      <div
        className='prize-quantity-grid'
        style={{
          gridTemplateColumns: `repeat(${safeTotal}, ${cellWidth}px)`,
          gap: `${cellGap}px`,
        }}
      >
        {Array.from({ length: safeTotal }).map((_, index) => (
          <span
            key={index}
            className={index >= safeTotal - safeQuantity ? 'active' : ''}
          />
        ))}
      </div>
    </div>
  );
};

const ScratchCodeCard = ({ code, scratched, onReveal }) => {
  const canvasRef = useRef(null);
  const cardRef = useRef(null);
  const scratchingRef = useRef(false);
  const revealedRef = useRef(scratched);
  const [revealed, setRevealed] = useState(scratched);

  useEffect(() => {
    revealedRef.current = scratched;
    setRevealed(scratched);
  }, [scratched]);

  useEffect(() => {
    if (scratched) return;
    const canvas = canvasRef.current;
    const card = cardRef.current;
    if (!canvas || !card) return;

    const drawMask = () => {
      const rect = card.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      canvas.width = Math.max(1, Math.floor(rect.width * dpr));
      canvas.height = Math.max(1, Math.floor(rect.height * dpr));
      canvas.style.width = `${rect.width}px`;
      canvas.style.height = `${rect.height}px`;

      const ctx = canvas.getContext('2d');
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      const gradient = ctx.createLinearGradient(0, 0, rect.width, rect.height);
      gradient.addColorStop(0, '#55565b');
      gradient.addColorStop(0.45, '#73757b');
      gradient.addColorStop(1, '#34363b');
      ctx.globalCompositeOperation = 'source-over';
      ctx.fillStyle = gradient;
      ctx.fillRect(0, 0, rect.width, rect.height);
      ctx.fillStyle = 'rgba(255, 255, 255, 0.16)';
      for (let x = -rect.height; x < rect.width; x += 22) {
        ctx.save();
        ctx.translate(x, 0);
        ctx.rotate(-0.52);
        ctx.fillRect(0, 0, 8, rect.height * 2.4);
        ctx.restore();
      }
      ctx.fillStyle = 'rgba(0, 0, 0, 0.14)';
      for (let i = 0; i < 220; i += 1) {
        const x = Math.random() * rect.width;
        const y = Math.random() * rect.height;
        ctx.fillRect(x, y, 1.2, 1.2);
      }
      ctx.fillStyle = 'rgba(255, 255, 255, 0.92)';
      ctx.font = '900 18px sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText('刮兑换码', rect.width / 2, rect.height / 2);
    };

    drawMask();
    const resizeObserver = new ResizeObserver(drawMask);
    resizeObserver.observe(card);
    return () => resizeObserver.disconnect();
  }, [scratched]);

  const scratchAt = (event) => {
    const canvas = canvasRef.current;
    if (!canvas || revealedRef.current) return;
    const rect = canvas.getBoundingClientRect();
    const ctx = canvas.getContext('2d');
    ctx.globalCompositeOperation = 'destination-out';
    ctx.beginPath();
    ctx.arc(event.clientX - rect.left, event.clientY - rect.top, 22, 0, Math.PI * 2);
    ctx.fill();
  };

  const checkScratchProgress = async () => {
    const canvas = canvasRef.current;
    if (!canvas || revealedRef.current) return;
    const ctx = canvas.getContext('2d');
    const { width, height } = canvas;
    const pixels = ctx.getImageData(0, 0, width, height).data;
    let transparent = 0;
    for (let i = 3; i < pixels.length; i += 16) {
      if (pixels[i] < 32) transparent += 1;
    }
    const sampled = pixels.length / 16;
    if (sampled > 0 && transparent / sampled >= SCRATCH_THRESHOLD) {
      revealedRef.current = true;
      const ok = await onReveal();
      setRevealed(ok !== false);
      if (ok === false) {
        revealedRef.current = false;
      }
    }
  };

  if (scratched || revealed) {
    return (
      <div className='scratch-code-card revealed'>
        <strong>{code || '••••••••••••••••••••••••••••••••'}</strong>
      </div>
    );
  }

  return (
    <div className='scratch-code-card' ref={cardRef}>
      <strong>{code}</strong>
      <canvas
        ref={canvasRef}
        className='scratch-mask'
        onPointerDown={(event) => {
          scratchingRef.current = true;
          event.currentTarget.setPointerCapture?.(event.pointerId);
          scratchAt(event);
        }}
        onPointerMove={(event) => {
          if (!scratchingRef.current) return;
          scratchAt(event);
        }}
        onPointerUp={async (event) => {
          scratchingRef.current = false;
          event.currentTarget.releasePointerCapture?.(event.pointerId);
          await checkScratchProgress();
        }}
        onPointerLeave={async () => {
          scratchingRef.current = false;
          await checkScratchProgress();
        }}
      />
    </div>
  );
};

const Lottery = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [periods, setPeriods] = useState([]);
  const [selectedId, setSelectedId] = useState();
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(false);
  const [joining, setJoining] = useState(false);
  const [scratching, setScratching] = useState(false);
  const [tick, setTick] = useState(0);

  const now = Math.floor(Date.now() / 1000);
  const period = detail?.period;
  const stage = resolveStage(period, now);

  const countdownTarget = stage === 'pending' ? period?.start_time : period?.end_time;
  const countdownText = countdownTarget ? formatCountdown(countdownTarget - now) : '--';
  const prizeTotal = detail?.period?.prize_count || 0;
  const entryTotal = detail?.period?.entry_count || 0;
  const winnerTotal = detail?.period?.winner_count || 0;
  const maxPrizeQuantity = useMemo(
    () =>
      Math.max(
        1,
        ...(detail?.prizes || []).map((prize) => Number(prize.quantity || 0)),
      ),
    [detail?.prizes],
  );

  const loadPeriods = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/lottery/periods');
      const { success, data, message } = res.data;
      if (success) {
        const items = data.items || [];
        setPeriods(items);
        setSelectedId((current) => current || items[0]?.id);
      } else {
        Toast.error({ content: message || t('加载失败') });
      }
    } finally {
      setLoading(false);
    }
  };

  const loadDetail = async (id) => {
    if (!id) {
      setDetail(null);
      return;
    }
    setLoading(true);
    try {
      const res = await API.get(`/api/lottery/periods/${id}`, {
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

  useEffect(() => {
    const timer = window.setInterval(() => setTick((value) => value + 1), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!selectedId || !period) return;
    if (stage === 'drawing' || stage === 'drawn') {
      const timer = window.setTimeout(() => loadDetail(selectedId), 5000);
      return () => window.clearTimeout(timer);
    }
  }, [tick, selectedId, stage, period?.status]);

  const periodOptions = useMemo(
    () =>
      periods.map((item) => ({
        label: `${t('第')} ${item.issue} ${t('期')}${item.title ? ` - ${item.title}` : ''}`,
        value: item.id,
      })),
    [periods, t],
  );

  const winnerGroups = useMemo(() => {
    if (stage !== 'drawn') return [];
    const winners = detail?.winners || [];
    const prizes = [...(detail?.prizes || [])].sort((a, b) => {
      if (a.sort_order !== b.sort_order) return a.sort_order - b.sort_order;
      return a.id - b.id;
    });
    const winnersByPrize = new Map();
    winners.forEach((winner) => {
      const items = winnersByPrize.get(winner.prize_id) || [];
      items.push(winner);
      winnersByPrize.set(winner.prize_id, items);
    });
    const groups = prizes
      .map((prize, index) => ({
        prize,
        rank: index + 1,
        winners: winnersByPrize.get(prize.id) || [],
      }))
      .filter((group) => group.winners.length > 0);

    const knownPrizeIds = new Set(prizes.map((prize) => prize.id));
    const unknownWinners = winners.filter((winner) => !knownPrizeIds.has(winner.prize_id));
    if (unknownWinners.length > 0) {
      groups.push({
        prize: { id: 0, level_name: t('其他奖项'), quantity: unknownWinners.length, sort_order: Number.MAX_SAFE_INTEGER },
        rank: groups.length + 1,
        winners: unknownWinners,
      });
    }
    return groups;
  }, [detail?.prizes, detail?.winners, stage, t]);

  const statusCopy = {
    empty: t('请选择要查看的抽奖期数'),
    pending: t('距离抽奖开始还有'),
    running: t('距离开奖还有'),
    drawing: t('开奖中'),
    drawn: t('已开奖'),
  };

  const stageLabel = {
    empty: t('等待开启'),
    pending: t('即将开始'),
    running: t('火热进行中'),
    drawing: t('开奖中'),
    drawn: t('中奖名单已公布'),
  };

  const joinLottery = async () => {
    const user = getLocalUser();
    if (!user) {
      Toast.warning({ content: t('请先登录后再参与抽奖') });
      navigate('/login');
      return;
    }
    setJoining(true);
    try {
      const res = await API.post(`/api/lottery/periods/${selectedId}/join`);
      const { success, message } = res.data;
      if (success) {
        Toast.success({ content: t('已成功参与抽奖') });
        await loadDetail(selectedId);
      } else {
        Toast.error({ content: message || t('参与失败') });
      }
    } finally {
      setJoining(false);
    }
  };

  const revealWinnerCode = async () => {
    if (!selectedId || scratching) return false;
    setScratching(true);
    try {
      const res = await API.post(`/api/lottery/periods/${selectedId}/scratch`);
      const { success, data, message } = res.data;
      if (success) {
        setDetail((current) => ({
          ...current,
          self_winner: data,
        }));
        Toast.success({ content: t('兑换码已揭晓') });
        return true;
      } else {
        Toast.error({ content: message || t('揭晓失败') });
        return false;
      }
    } catch (error) {
      Toast.error({ content: t('揭晓失败') });
      return false;
    } finally {
      setScratching(false);
    }
  };

  const renderSelfResult = () => {
    if (!period || stage !== 'drawn') return null;
    if (detail?.self_winner) {
      return (
        <div className='lottery-self-result winner'>
          <div className='lottery-result-icon'>
            <Crown size={26} />
          </div>
          <div className='lottery-result-body'>
            <Text className='lottery-result-title'>
              {t('恭喜中奖')} - {detail.self_winner.prize_name}
            </Text>
            <Text type='secondary'>{detail.self_winner.prize_description}</Text>
            <ScratchCodeCard
              code={detail.self_winner.code}
              scratched={detail.self_winner.code_scratched}
              onReveal={revealWinnerCode}
            />
          </div>
        </div>
      );
    }
    if (detail?.self_entry) {
      return (
        <div className='lottery-self-result'>
          <CheckCircle2 size={24} />
          <div>
            <Text className='lottery-result-title'>{t('感谢参与')}</Text>
            <div>{t('本期未中奖，期待下次好运')}</div>
          </div>
        </div>
      );
    }
    return null;
  };

  return (
    <div className='lottery-page'>
      <section className='lottery-hero'>
        <div className='lottery-confetti' aria-hidden='true' />
        <div className='lottery-hero-copy'>
          <div className='lottery-eyebrow'>
            <Sparkles size={16} />
            <span>{t('年度幸运抽奖')}</span>
          </div>
          <Title heading={1} className='lottery-title'>
            {period ? `${period.title || t('幸运抽奖')}` : t('幸运抽奖')}
          </Title>
          <p className='lottery-subtitle'>
            {period
              ? `${t('第')} ${period.issue} ${t('期')} · ${stageLabel[stage]}`
              : t('选择一期正在展示的抽奖活动')}
          </p>
          <div className='lottery-hero-actions'>
            <Select
              value={selectedId}
              optionList={periodOptions}
              placeholder={t('选择抽奖期数')}
              onChange={setSelectedId}
              showClear
              className='lottery-period-select'
            />
            <Button
              type='primary'
              size='large'
              disabled={stage !== 'running' || detail?.self_entry}
              loading={joining}
              onClick={joinLottery}
              icon={detail?.self_entry ? <CheckCircle2 size={18} /> : <Gift size={18} />}
            >
              {detail?.self_entry ? t('已成功参与') : stage === 'running' ? t('立即参与抽奖') : t('等待活动开始')}
            </Button>
          </div>
        </div>

        <div className='lottery-stage'>
          <div className='lottery-wheel' aria-hidden='true'>
            <div className='wheel-ring' />
            <div className='wheel-core'>
              <Crown size={46} />
              <span>{t('LUCKY')}</span>
            </div>
          </div>
          <div className='lottery-countdown'>
            <Clock size={20} />
            <span>{statusCopy[stage]}</span>
            <strong>{stage === 'drawn' || stage === 'drawing' ? statusCopy[stage] : countdownText}</strong>
          </div>
        </div>
      </section>

      <Spin spinning={loading}>
        {!selectedId ? (
          <div className='lottery-empty'>
            <Empty description={t('当前没有正在展示的抽奖活动')} />
          </div>
        ) : (
          <>
            <section className='lottery-overview'>
              <div className='lottery-stat'>
                <Gift size={22} />
                <span>{t('奖品总数')}</span>
                <strong>{prizeTotal}</strong>
              </div>
              <div className='lottery-stat'>
                <Users size={22} />
                <span>{t('参与人数')}</span>
                <strong>{entryTotal}</strong>
              </div>
              <div className='lottery-stat'>
                <Trophy size={22} />
                <span>{t('中奖人数')}</span>
                <strong>{winnerTotal}</strong>
              </div>
              <div className='lottery-stat'>
                <ShieldCheck size={22} />
                <span>{t('开奖状态')}</span>
                <strong>{stageLabel[stage]}</strong>
              </div>
            </section>

            <section className='lottery-flow'>
              <div className='lottery-ticket-panel'>
                <div className='ticket-stub'>
                  <PartyPopper size={26} />
                  <span>{t('抽奖券')}</span>
                </div>
                <div className='ticket-main'>
                  <div className='panel-heading'>
                    <Ticket size={22} />
                    <span>{t('我的参与状态')}</span>
                  </div>
                  <div className='lottery-time-row'>
                    <CalendarDays size={18} />
                    <span>{t('启动时间')}</span>
                    <strong>{timestamp2string(period?.start_time || 0)}</strong>
                  </div>
                  <div className='lottery-time-row'>
                    <Clock size={18} />
                    <span>{t('截止时间')}</span>
                    <strong>{timestamp2string(period?.end_time || 0)}</strong>
                  </div>
                  {detail?.self_entry ? (
                    <div className='joined-state'>
                      <CheckCircle2 size={24} />
                      <span>{stage === 'drawn' ? t('已参与本期抽奖') : t('已成功参与，开奖后查看结果')}</span>
                    </div>
                  ) : (
                    <Button
                      type='primary'
                      size='large'
                      disabled={stage !== 'running'}
                      loading={joining}
                      onClick={joinLottery}
                      icon={<Gift size={18} />}
                      block
                    >
                      {stage === 'running' ? t('立即参与') : t('暂不可参与')}
                    </Button>
                  )}
                  {renderSelfResult()}
                </div>
              </div>

              <div className='lottery-prize-panel'>
                <div className='panel-heading'>
                  <Medal size={22} />
                  <span>{t('本期奖池')}</span>
                </div>
                <div className='prize-list'>
                  {(detail?.prizes || []).map((prize, index) => (
                    <div className='prize-item' key={prize.id}>
                      <div className='prize-rank'>
                        <Star size={18} />
                        <span>{String(index + 1).padStart(2, '0')}</span>
                      </div>
                      <div className='prize-copy'>
                        <strong>{prize.level_name}</strong>
                        <p>{prize.description || t('暂无奖品描述')}</p>
                      </div>
                      <div className='prize-meta'>
                        <PrizeQuantityGrid
                          quantity={prize.quantity}
                          total={maxPrizeQuantity}
                          label={`${prize.level_name} ${prize.quantity} ${t('份')}`}
                        />
                        {prize.paid_only && <Tag className='lottery-paid-tag'>{t('限付费用户')}</Tag>}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </section>

            <section className='lottery-winners'>
              <div className='panel-heading'>
                <Users size={22} />
                <span>{t('中奖公示')}</span>
              </div>
              {stage !== 'drawn' ? (
                <div className='notice-waiting'>
                  <Gift size={34} />
                  <Text>{t('开奖后将在这里公布中奖用户和奖项')}</Text>
                </div>
              ) : winnerGroups.length > 0 ? (
                <div className='winner-group-list'>
                  {winnerGroups.map((group) => (
                    <div className='winner-group' key={group.prize.id || group.prize.level_name}>
                      <div className='winner-group-head'>
                        <div className='winner-group-badge'>
                          <Trophy size={20} />
                          <span>{String(group.rank).padStart(2, '0')}</span>
                        </div>
                        <div>
                          <strong>{group.prize.level_name}</strong>
                          <p>{group.prize.description || t('幸运名单已公布')}</p>
                        </div>
                        <Tag color='red'>{group.winners.length} / {group.prize.quantity || group.winners.length}</Tag>
                      </div>
                      <div className='winner-list'>
                        {group.winners.map((winner, index) => (
                          <div className='winner-item' key={winner.id}>
                            <span>{String(index + 1).padStart(2, '0')}</span>
                            <strong>{winner.username}</strong>
                            <ChevronRight size={18} />
                            <em>{winner.prize_name}</em>
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <Empty description={t('本期暂无中奖记录')} />
              )}
            </section>
          </>
        )}
      </Spin>
    </div>
  );
};

export default Lottery;
