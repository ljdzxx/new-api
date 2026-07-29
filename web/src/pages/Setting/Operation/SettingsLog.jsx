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

import React, { useEffect, useState, useRef } from 'react';
import {
  Button,
  Col,
  Form,
  Row,
  Spin,
  DatePicker,
  Typography,
  Modal,
} from '@douyinfe/semi-ui';
import dayjs from 'dayjs';
import { useTranslation } from 'react-i18next';
import {
  compareObjects,
  API,
  getUserIdFromLocalStorage,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';

const { Text } = Typography;

export default function SettingsLog(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [loadingCleanHistoryLog, setLoadingCleanHistoryLog] = useState(false);
  const [cleanedHistoryLogCount, setCleanedHistoryLogCount] = useState(0);
  const [inputs, setInputs] = useState({
    LogConsumeEnabled: false,
    historyTimestamp: dayjs().subtract(1, 'month').toDate(),
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow).filter(
      (item) => item.key !== 'historyTimestamp',
    );

    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = inputs[item.key];
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }
  async function onCleanHistoryLog() {
    if (!inputs.historyTimestamp) {
      showError(t('请选择日志记录时间'));
      return;
    }

    const now = dayjs();
    const targetDate = dayjs(inputs.historyTimestamp);
    const targetTime = targetDate.format('YYYY-MM-DD HH:mm:ss');
    const currentTime = now.format('YYYY-MM-DD HH:mm:ss');
    const daysDiff = now.diff(targetDate, 'day');

    const renderCleanHistoryLogContent = (deletedCount = null) => (
      <div style={{ lineHeight: '1.8' }}>
        <p>
          <Text>{t('当前时间')}：</Text>
          <Text strong style={{ color: '#52c41a' }}>
            {currentTime}
          </Text>
        </p>
        <p>
          <Text>{t('选择时间')}：</Text>
          <Text strong type='danger'>
            {targetTime}
          </Text>
          {daysDiff > 0 && (
            <Text type='tertiary'>
              {' '}
              ({t('约')} {daysDiff} {t('天前')})
            </Text>
          )}
        </p>
        <div
          style={{
            background: '#fff7e6',
            border: '1px solid #ffd591',
            padding: '12px',
            borderRadius: '4px',
            marginTop: '12px',
            color: '#333',
          }}
        >
          <Text strong style={{ color: '#d46b08' }}>
            ⚠️ {t('注意')}：
          </Text>
          <Text style={{ color: '#333' }}>{t('将删除')} </Text>
          <Text strong style={{ color: '#cf1322' }}>
            {targetTime}
          </Text>
          {daysDiff > 0 && (
            <Text style={{ color: '#8c8c8c' }}>
              {' '}
              ({t('约')} {daysDiff} {t('天前')})
            </Text>
          )}
          <Text style={{ color: '#333' }}> {t('之前的所有日志')}</Text>
        </div>
        {deletedCount === null ? (
          <p style={{ marginTop: '12px' }}>
            <Text type='danger'>
              {t('此操作不可恢复，请仔细确认时间后再操作！')}
            </Text>
          </p>
        ) : (
          <div
            style={{
              backgroundColor: '#eaf4ff',
              border: '1px solid #84b9e8',
              padding: '12px',
              borderRadius: '4px',
              marginTop: '12px',
              color: '#0b4a7e',
            }}
          >
            <Spin size='small' />{' '}
            <Text strong style={{ color: '#0b4a7e' }}>
              {t('正在清理，已清理 {{count}} 条日志', {
                count: deletedCount,
              })}
            </Text>
          </div>
        )}
      </div>
    );

    let confirmModal;
    confirmModal = Modal.confirm({
      title: t('确认清除历史日志'),
      content: renderCleanHistoryLogContent(),
      okText: t('确认删除'),
      cancelText: t('取消'),
      okType: 'danger',
      maskClosable: false,
      closeOnEsc: false,
      onOk: async () => {
        try {
          setLoadingCleanHistoryLog(true);
          setCleanedHistoryLogCount(0);
          confirmModal.update({
            content: renderCleanHistoryLogContent(0),
            cancelButtonProps: { disabled: true },
          });
          const targetTimestamp = Date.parse(inputs.historyTimestamp) / 1000;
          const baseURL = String(API.defaults.baseURL || '').replace(/\/$/, '');
          const response = await fetch(
            `${baseURL}/api/log/?target_timestamp=${targetTimestamp}&stream=true`,
            {
              method: 'DELETE',
              headers: {
                Accept: 'text/event-stream',
                'Cache-Control': 'no-store',
                'New-API-User': String(getUserIdFromLocalStorage()),
              },
            },
          );

          if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
          }
          if (
            !response.headers.get('content-type')?.includes('text/event-stream')
          ) {
            const result = await response.json();
            throw new Error(t('日志清理失败：') + (result.message || ''));
          }

          const reader = response.body.getReader();
          const decoder = new TextDecoder();
          let buffer = '';
          let deletedCount = 0;
          let completed = false;

          while (true) {
            const { done, value } = await reader.read();
            buffer += decoder.decode(value || new Uint8Array(), {
              stream: !done,
            });
            buffer = buffer.replace(/\r\n/g, '\n');
            const eventBlocks = buffer.split('\n\n');
            buffer = eventBlocks.pop() || '';

            for (const eventBlock of eventBlocks) {
              const dataLine = eventBlock
                .split('\n')
                .find((line) => line.startsWith('data: '));
              if (!dataLine || dataLine === 'data: [DONE]') continue;

              const event = JSON.parse(dataLine.substring(6));
              if (event.type === 'progress' || event.type === 'retry') {
                deletedCount = event.deleted;
                setCleanedHistoryLogCount(deletedCount);
                confirmModal.update({
                  content: renderCleanHistoryLogContent(deletedCount),
                });
              } else if (event.type === 'complete') {
                deletedCount = event.deleted;
                setCleanedHistoryLogCount(deletedCount);
                confirmModal.update({
                  content: renderCleanHistoryLogContent(deletedCount),
                });
                completed = true;
              } else if (event.type === 'error') {
                throw new Error(t('日志清理失败：') + event.message);
              }
            }

            if (done) break;
          }

          if (!completed) {
            throw new Error(t('数据传输中断'));
          }

          showSuccess(`${deletedCount} ${t('条日志已清理！')}`);
        } catch (error) {
          if (
            (error?.name === 'AxiosError' && !error.response) ||
            error instanceof TypeError
          ) {
            showError(t('网络错误'));
          } else {
            showError(error);
          }
        } finally {
          setLoadingCleanHistoryLog(false);
        }
      },
    });
  }

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        currentInputs[key] = props.options[key];
      }
    }
    currentInputs['historyTimestamp'] = inputs.historyTimestamp;
    setInputs(Object.assign(inputs, currentInputs));
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);
  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('日志设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'LogConsumeEnabled'}
                  label={t('启用额度消费日志记录')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) => {
                    setInputs({
                      ...inputs,
                      LogConsumeEnabled: value,
                    });
                  }}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Spin spinning={loadingCleanHistoryLog}>
                  <Form.DatePicker
                    label={t('清除历史日志')}
                    field={'historyTimestamp'}
                    type='dateTime'
                    inputReadOnly={true}
                    onChange={(value) => {
                      setInputs({
                        ...inputs,
                        historyTimestamp: value,
                      });
                    }}
                  />
                  <Text
                    type='tertiary'
                    size='small'
                    style={{ display: 'block', marginTop: 4, marginBottom: 8 }}
                  >
                    {t('将清除选定时间之前的所有日志')}
                  </Text>
                  <Button
                    size='default'
                    type='danger'
                    loading={loadingCleanHistoryLog}
                    onClick={onCleanHistoryLog}
                  >
                    {t('清除历史日志')}
                  </Button>
                </Spin>
                {loadingCleanHistoryLog && (
                  <Text
                    size='small'
                    style={{
                      color: '#0b4a7e',
                      display: 'block',
                      marginTop: 8,
                    }}
                  >
                    {t('正在清理，已清理 {{count}} 条日志', {
                      count: cleanedHistoryLogCount,
                    })}
                  </Text>
                )}
              </Col>
            </Row>

            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存日志设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
