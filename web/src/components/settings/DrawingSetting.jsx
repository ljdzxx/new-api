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

import React, { useEffect, useState } from 'react';
import { Card, Spin } from '@douyinfe/semi-ui';
import SettingsDrawing from '../../pages/Setting/Drawing/SettingsDrawing';
import { API, showError, toBoolean } from '../../helpers';

const defaultDrawingInputs = {
  DrawingEnabled: false,
  MjNotifyEnabled: false,
  MjAccountFilterEnabled: false,
  MjForwardUrlEnabled: false,
  MjModeClearEnabled: false,
  MjActionCheckSuccessEnabled: false,
  'image_storage_setting.r2_enabled': false,
  'image_storage_setting.r2_account_id': '',
  'image_storage_setting.r2_bucket': '',
  'image_storage_setting.r2_endpoint': '',
  'image_storage_setting.r2_access_key_id': '',
  'image_storage_setting.r2_secret': '',
  'image_storage_setting.r2_object_prefix': 'generated-images/',
  'image_storage_setting.r2_url_expire_hours': 24,
};

const DrawingSetting = () => {
  let [inputs, setInputs] = useState(defaultDrawingInputs);

  let [loading, setLoading] = useState(false);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      let newInputs = { ...defaultDrawingInputs };
      data.forEach((item) => {
        if (item.key.endsWith('Enabled') || typeof inputs[item.key] === 'boolean') {
          newInputs[item.key] = toBoolean(item.value);
        } else {
          newInputs[item.key] = item.value;
        }
      });

      setInputs(newInputs);
    } else {
      showError(message);
    }
  };

  async function onRefresh() {
    try {
      setLoading(true);
      await getOptions();
    } catch (error) {
      showError('刷新失败');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    onRefresh();
  }, []);

  return (
    <>
      <Spin spinning={loading} size='large'>
        {/* 绘图设置 */}
        <Card style={{ marginTop: '10px' }}>
          <SettingsDrawing options={inputs} refresh={onRefresh} />
        </Card>
      </Spin>
    </>
  );
};

export default DrawingSetting;
