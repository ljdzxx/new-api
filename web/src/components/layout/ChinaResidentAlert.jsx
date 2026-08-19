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

import React from 'react';
import { Button, Modal } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';

import './ChinaResidentAlert.css';

const ChinaResidentAlert = ({ visible, onConfirm, onCancel }) => {
  const { t } = useTranslation();

  return (
    <Modal
      className='china-resident-alert'
      visible={visible}
      centered
      width={360}
      title={null}
      footer={null}
      closable={false}
      maskClosable={false}
      closeOnEsc={false}
      onCancel={onCancel}
    >
      <div className='china-resident-alert-content'>
        <div className='china-resident-alert-icon' aria-hidden>
          !
        </div>
        <p className='china-resident-alert-message'>
          {t(
            '本网站提供的产品与服务不适用于中国居民。本网站上的任何内容均不得解释为对中国任何个人的邀请。',
          )}
        </p>
        <div className='china-resident-alert-actions'>
          <Button
            theme='solid'
            className='china-resident-alert-confirm'
            onClick={onConfirm}
          >
            {t('我知道了')}
          </Button>
          <Button
            theme='borderless'
            className='china-resident-alert-cancel'
            onClick={onCancel}
          >
            {t('取消')}
          </Button>
        </div>
      </div>
    </Modal>
  );
};

export default ChinaResidentAlert;
