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

import React, { useState } from 'react';
import { Button, Modal } from '@douyinfe/semi-ui';
import { useNavigate } from 'react-router-dom';

const UsersActions = ({
  setShowAddUser,
  setShowInviteRewardAudits,
  selectedRowKeys,
  batchLoading,
  batchManageUsers,
  loading,
  t,
}) => {
  const navigate = useNavigate();
  const [batchOperation, setBatchOperation] = useState(null);

  const handleAddUser = () => {
    setShowAddUser(true);
  };

  return (
    <div className='flex flex-wrap gap-2 w-full md:w-auto order-2 md:order-1'>
      <Button
        className='w-full md:w-auto'
        type='tertiary'
        size='small'
        onClick={() => navigate('/console/subscription-rank')}
      >
        {t('订阅使用榜')}
      </Button>
      <Button
        className='w-full md:w-auto'
        type='tertiary'
        size='small'
        onClick={() => setShowInviteRewardAudits(true)}
      >
        {t('邀请奖励风控审计')}
      </Button>
      <Button className='w-full md:w-auto' onClick={handleAddUser} size='small'>
        {t('添加用户')}
      </Button>
      <Button
        className='w-full md:w-auto'
        size='small'
        type='danger'
        disabled={!selectedRowKeys.length || loading || batchLoading}
        onClick={() =>
          setBatchOperation({ action: 'disable', ids: [...selectedRowKeys] })
        }
      >
        {t('批量禁用')}
      </Button>
      <Button
        className='w-full md:w-auto'
        size='small'
        disabled={!selectedRowKeys.length || loading || batchLoading}
        onClick={() =>
          setBatchOperation({ action: 'enable', ids: [...selectedRowKeys] })
        }
      >
        {t('批量启用')}
      </Button>
      {selectedRowKeys.length > 0 && (
        <span className='self-center text-sm'>
          {t('已勾选 {{selected}} 个用户', {
            selected: selectedRowKeys.length,
          })}
        </span>
      )}
      <Modal
        title={
          batchOperation?.action === 'disable' ? t('批量禁用') : t('批量启用')
        }
        visible={!!batchOperation}
        confirmLoading={batchLoading}
        cancelButtonProps={{ disabled: batchLoading }}
        closable={!batchLoading}
        maskClosable={false}
        closeOnEsc={!batchLoading}
        onCancel={() => {
          if (!batchLoading) setBatchOperation(null);
        }}
        onOk={async () => {
          if (!batchOperation || batchLoading) return;
          await batchManageUsers(batchOperation.ids, batchOperation.action);
          setBatchOperation(null);
        }}
      >
        {batchOperation?.action === 'disable'
          ? t('确定要禁用选中的 {{selected}} 个用户吗？', {
              selected: batchOperation?.ids.length,
            })
          : t('确定要启用选中的 {{selected}} 个用户吗？', {
              selected: batchOperation?.ids.length,
            })}
      </Modal>
    </div>
  );
};

export default UsersActions;
