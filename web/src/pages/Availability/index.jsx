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
import { API, showError } from '../../helpers';
import { marked } from 'marked';
import { Empty } from '@douyinfe/semi-ui';
import {
  IllustrationConstruction,
  IllustrationConstructionDark,
} from '@douyinfe/semi-illustrations';
import { useTranslation } from 'react-i18next';

const Availability = () => {
  const { t } = useTranslation();
  const [availability, setAvailability] = useState('');
  const [availabilityLoaded, setAvailabilityLoaded] = useState(false);

  const displayAvailability = async () => {
    setAvailability(localStorage.getItem('availability') || '');
    const res = await API.get('/api/availability');
    const { success, message, data } = res.data;
    if (success) {
      let availabilityContent = data;
      if (!data.startsWith('https://')) {
        availabilityContent = marked.parse(data);
      }
      setAvailability(availabilityContent);
      localStorage.setItem('availability', availabilityContent);
    } else {
      showError(message);
      setAvailability(t('加载可用性内容失败...'));
    }
    setAvailabilityLoaded(true);
  };

  useEffect(() => {
    displayAvailability().then();
  }, []);

  const emptyStyle = {
    padding: '24px',
  };

  const customDescription = (
    <div style={{ textAlign: 'center' }}>
      <p>{t('可在设置页面设置可用性内容，支持 HTML & Markdown')}</p>
    </div>
  );

  return (
    <div className='mt-[60px] px-2'>
      {availabilityLoaded && availability === '' ? (
        <div className='flex justify-center items-center h-screen p-8'>
          <Empty
            image={
              <IllustrationConstruction style={{ width: 150, height: 150 }} />
            }
            darkModeImage={
              <IllustrationConstructionDark
                style={{ width: 150, height: 150 }}
              />
            }
            description={t('管理员暂时未设置任何可用性内容')}
            style={emptyStyle}
          >
            {customDescription}
          </Empty>
        </div>
      ) : (
        <>
          {availability.startsWith('https://') ? (
            <iframe
              src={availability}
              style={{ width: '100%', height: '100vh', border: 'none' }}
            />
          ) : (
            <div
              style={{ fontSize: 'larger' }}
              dangerouslySetInnerHTML={{ __html: availability }}
            ></div>
          )}
        </>
      )}
    </div>
  );
};

export default Availability;
