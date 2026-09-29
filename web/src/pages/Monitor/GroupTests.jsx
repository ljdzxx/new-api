import React, { useEffect, useRef, useState } from 'react';
import { Banner, Button, Modal, Spin, Tag } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { monitorRequests } from './requests';
import { monitorFailureMessage } from './errors';
import HTMLPreview from './HTMLPreview';

const statusOf = (r) =>
  r.status ||
  (r.ok
    ? 'success'
    : r.error === 'answer_mismatch'
      ? 'test_failed'
      : 'request_failed');
const timeOf = (at) => new Date(at).toLocaleString();
const durationOf = (ms) => `${Math.round((ms || 0) / 1000)} s`;

function HistoryRow({ kind, records, enabled, model, selected, onSelect }) {
  const { t } = useTranslation();
  const passed = records.filter((r) => statusOf(r) === 'success').length;
  const requestsFailed = records.filter(
    (r) => statusOf(r) === 'request_failed',
  ).length;
  const testsFailed = records.filter(
    (r) => statusOf(r) === 'test_failed',
  ).length;
  const average = records.length
    ? records.reduce((sum, r) => sum + (r.duration_ms || 0), 0) / records.length
    : 0;
  const labels = {
    success: t('请求成功'),
    request_failed: t('请求失败'),
    test_failed: t('检测失败'),
  };
  return (
    <section
      className='monitor-test-row'
      aria-label={kind === 'logic' ? t('逻辑题测试') : t('SVG 绘图测试')}
    >
      <div className='monitor-test-heading'>
        <strong>
          {kind === 'logic' ? t('逻辑题测试') : t('SVG 绘图测试')}
        </strong>
        <span title={model}>{model}</span>
        {!enabled && <Tag size='small'>{t('已关闭')}</Tag>}
      </div>
      <div className='monitor-test-stats'>
        <b>
          {records.length
            ? `${((passed / records.length) * 100).toFixed(1)}%`
            : '—'}
        </b>
        <span>
          {passed}/{records.length}{' '}
          {kind === 'logic' ? t('通过') : t('完成应答')}
        </span>
        {!!requestsFailed && (
          <span className='monitor-request-failed'>
            {requestsFailed} {t('请求失败')}
          </span>
        )}
        {!!testsFailed && (
          <span className='monitor-test-failed'>
            {testsFailed} {t('检测失败')}
          </span>
        )}
        {!!records.length && (
          <span>
            {t('平均耗时')} {durationOf(average)}
          </span>
        )}
      </div>
      <div className='monitor-test-blocks'>
        {records.map((record) => {
          const status = statusOf(record);
          const label = `${timeOf(record.at)} · ${record.model} · ${labels[status]} · ${durationOf(record.duration_ms)}`;
          return (
            <button
              type='button'
              key={record.id}
              className={`monitor-test-block ${status} ${selected === record.id ? 'selected' : ''}`}
              title={label}
              aria-label={label}
              aria-pressed={selected === record.id}
              onClick={() => onSelect(record)}
            />
          );
        })}
        {!records.length && (
          <span className='monitor-test-empty'>{t('暂无测试记录')}</span>
        )}
      </div>
      {!!records.length && (
        <div className='monitor-test-times'>
          <span>{timeOf(records[0].at)}</span>
          <span>{timeOf(records[records.length - 1].at)}</span>
        </div>
      )}
    </section>
  );
}

export default function GroupTests({ group }) {
  const { t } = useTranslation();
  const history = group.history || { logic: [], svg: [] };
  const artworks = group.artworks || [];
  const [selectedSVG, setSelectedSVG] = useState(null);
  const [record, setRecord] = useState(null);
  const [recordLoading, setRecordLoading] = useState(false);
  const [details, setDetails] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const artworkError = !!group.artworks_error;
  const selection = useRef(0);
  const detailRequest = useRef(null);
  useEffect(
    () => () => {
      selection.current++;
      detailRequest.current?.abort();
    },
    [],
  );
  const select = async (summary, showDetails = false) => {
    const current = ++selection.current;
    detailRequest.current?.abort();
    const controller = new AbortController();
    detailRequest.current = controller;
    if (summary.kind === 'svg') setSelectedSVG(summary.id);
    setRecord(summary);
    setDetails(showDetails);
    if (!showDetails) {
      setRecordLoading(false);
      return;
    }
    setRecordLoading(true);
    try {
      const { data } = await monitorRequests.get('/api/monitor/record', {
        params: { group: group.name, kind: summary.kind, id: summary.id },
        signal: controller.signal,
      });
      if (current !== selection.current) return;
      if (!data.success) throw Error();
      setRecord(data.data);
    } catch {
      if (current === selection.current)
        setRecord({ ...summary, detailError: t('测试记录已过期或暂时不可用') });
    } finally {
      if (current === selection.current) setRecordLoading(false);
    }
  };
  const selected = selectedSVG
    ? history.svg.find((r) => r.id === selectedSVG)
    : null;
  const artwork = selectedSVG
    ? artworks.find((a) => a.id === selectedSVG)
    : artworks[0];
  const preview = artwork && (
    <HTMLPreview
      key={artwork.id}
      artwork={artwork}
      title={`${group.name} ${t('HTML 预览')}`}
    />
  );
  return (
    <>
      <td className='monitor-tests-cell'>
        <HistoryRow
          kind='logic'
          records={history.logic}
          enabled={group.logic_test}
          model={group.logic_model}
          selected={record?.kind === 'logic' ? record.id : null}
          onSelect={(r) => select(r, true)}
        />
        <HistoryRow
          kind='svg'
          records={history.svg}
          enabled={group.svg_test}
          model={group.svg_model}
          selected={selectedSVG || artwork?.id}
          onSelect={(r) => select(r)}
        />
        <div className='monitor-test-key'>
          <span>
            <i className='success' />
            {t('请求成功')}
          </span>
          <span>
            <i className='request_failed' />
            {t('请求失败')}
          </span>
          <span>
            <i className='test_failed' />
            {t('检测失败')} ({t('仅逻辑题')})
          </span>
        </div>
        {record && (
          <Button size='small' onClick={() => select(record, true)}>
            {t('查看所选测试详情')}
          </Button>
        )}
      </td>
      <td className='monitor-preview-cell'>
        <div className='monitor-preview-caption'>
          <strong>{t('HTML 预览')}</strong>
          {(selected || artwork) && (
            <small>{timeOf((selected || artwork).at)}</small>
          )}
        </div>
        {preview || (
          <div className='monitor-preview-empty'>
            {selected && !selected.ok
              ? t('本次请求未完成，无预览')
              : artworkError
                ? t('HTML 预览暂时不可用')
                : t('HTML 预览未上传或已清理')}
          </div>
        )}
        {selected?.artifact_error && (
          <small className='monitor-error'>
            {monitorFailureMessage(selected.artifact_error, t)}
          </small>
        )}
        <div className='monitor-preview-controls'>
          <small>{t('点击 SVG 状态方块切换预览')}</small>
          {artwork && (
            <Button size='small' onClick={() => setExpanded(true)}>
              {t('放大预览')}
            </Button>
          )}
        </div>
      </td>
      <Modal
        title={`${group.name} · ${t('测试详情')}`}
        visible={details}
        onCancel={() => setDetails(false)}
        footer={null}
        width={820}
      >
        {recordLoading ? (
          <Spin />
        ) : (
          record && (
            <div className='monitor-record'>
              <p>
                {timeOf(record.at)} · {record.model} ·{' '}
                {durationOf(record.duration_ms)}
              </p>
              <Tag
                color={
                  statusOf(record) === 'success'
                    ? 'green'
                    : statusOf(record) === 'test_failed'
                      ? 'red'
                      : 'yellow'
                }
              >
                {statusOf(record) === 'success'
                  ? t('请求成功')
                  : statusOf(record) === 'test_failed'
                    ? t('检测失败')
                    : t('请求失败')}
              </Tag>
              {record.detailError && (
                <Banner type='warning' description={record.detailError} />
              )}
              {record.error && (
                <p className='monitor-error'>
                  {record.error === 'answer_mismatch'
                    ? t('模型回复与预期答案不符')
                    : monitorFailureMessage(record.error, t)}
                </p>
              )}
              {record.artifact_error && (
                <p className='monitor-error'>
                  {monitorFailureMessage(record.artifact_error, t)}
                </p>
              )}
              <h4>{t('测试题目')}</h4>
              <pre>{record.prompt || '—'}</pre>
              {record.kind === 'logic' && (
                <>
                  <h4>
                    {t('预期答案')} ·{' '}
                    {record.match_mode === 'contains'
                      ? t('包含指定的字符')
                      : t('完全相等')}
                  </h4>
                  <pre>{record.expected || '—'}</pre>
                </>
              )}
              <h4>{t('模型应答')}</h4>
              <pre>{record.answer || t('未收到模型应答')}</pre>
            </div>
          )
        )}
      </Modal>
      <Modal
        title={`${group.name} · ${t('HTML 预览')}`}
        visible={expanded}
        onCancel={() => setExpanded(false)}
        footer={null}
        width={1040}
      >
        <div className='monitor-preview-expanded'>{preview}</div>
      </Modal>
    </>
  );
}
