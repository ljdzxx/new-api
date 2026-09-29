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

import React, { useCallback, useContext, useEffect, useState } from 'react';
import { ConfigProvider } from '@douyinfe/semi-ui';
import ConfigContext from '@douyinfe/semi-ui/lib/es/configProvider/context';

export default function MonitorTheme({ children }) {
  const inheritedConfig = useContext(ConfigContext);
  const [popupContainer] = useState(() => {
    const container = document.createElement('div');
    container.className = 'monitor-theme monitor-overlays';
    return container;
  });
  const getPopupContainer = useCallback(() => popupContainer, [popupContainer]);

  useEffect(() => {
    document.body.appendChild(popupContainer);
    return () => popupContainer.remove();
  }, [popupContainer]);

  return (
    <ConfigProvider {...inheritedConfig} getPopupContainer={getPopupContainer}>
      <div className='monitor-theme monitor-page'>{children}</div>
    </ConfigProvider>
  );
}
