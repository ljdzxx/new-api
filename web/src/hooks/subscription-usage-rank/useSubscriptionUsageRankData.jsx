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

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';
import { useTableCompactMode } from '../common/useTableCompactMode';

const DEFAULT_RANGE = '1d';

export const useSubscriptionUsageRankData = () => {
  const { t } = useTranslation();
  const [compactMode, setCompactMode] = useTableCompactMode(
    'subscription-usage-rank',
  );

  const [items, setItems] = useState([]);
  const [summary, setSummary] = useState(null);
  const [loading, setLoading] = useState(true);
  const [keywordInput, setKeywordInput] = useState('');
  const [keyword, setKeyword] = useState('');
  const [rangeKey, setRangeKey] = useState(DEFAULT_RANGE);
  const [activePage, setActivePage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [total, setTotal] = useState(0);

  const normalizeRankItems = (rankItems = []) =>
    rankItems.map((item) => ({
      ...item,
      id: item?.id || item?.user_id,
      key: item?.user_id,
      DeletedAt: item?.DeletedAt ?? null,
    }));

  const loadRankings = async ({
    page = activePage,
    size = pageSize,
    nextRange = rangeKey,
    nextKeyword = keyword,
  } = {}) => {
    setLoading(true);
    try {
      const params = new URLSearchParams({
        range: nextRange || DEFAULT_RANGE,
        p: String(page),
        page_size: String(size),
      });
      if (nextKeyword?.trim()) {
        params.set('keyword', nextKeyword.trim());
      }

      const res = await API.get(
        `/api/user/subscription_usage_rank?${params.toString()}`,
      );
      if (!res.data?.success) {
        showError(res.data?.message || t('加载失败'));
        return;
      }

      const payload = res.data?.data || {};
      const pageData = payload?.page || {};

      setItems(normalizeRankItems(pageData.items || []));
      setTotal(pageData.total || 0);
      setSummary(payload.summary || null);
      setActivePage(pageData.page || page);
    } catch (error) {
      showError(t('请求失败'));
    } finally {
      setLoading(false);
    }
  };

  const handleSearch = async () => {
    const nextKeyword = keywordInput.trim();
    setKeyword(nextKeyword);
    setActivePage(1);
    await loadRankings({
      page: 1,
      size: pageSize,
      nextRange: rangeKey,
      nextKeyword: nextKeyword,
    });
  };

  const handleReset = async () => {
    setKeywordInput('');
    setKeyword('');
    setActivePage(1);
    await loadRankings({
      page: 1,
      size: pageSize,
      nextRange: rangeKey,
      nextKeyword: '',
    });
  };

  const handleRangeChange = async (nextRange) => {
    setRangeKey(nextRange);
    setActivePage(1);
    await loadRankings({
      page: 1,
      size: pageSize,
      nextRange,
      nextKeyword: keyword,
    });
  };

  const handlePageChange = async (page) => {
    setActivePage(page);
    await loadRankings({
      page,
      size: pageSize,
      nextRange: rangeKey,
      nextKeyword: keyword,
    });
  };

  const handlePageSizeChange = async (size) => {
    setPageSize(size);
    setActivePage(1);
    await loadRankings({
      page: 1,
      size,
      nextRange: rangeKey,
      nextKeyword: keyword,
    });
  };

  const manageUser = async (userId, action) => {
    setLoading(true);
    try {
      const res = await API.post('/api/user/manage', {
        id: userId,
        action,
      });

      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('操作成功完成！'));
        setItems((prevItems) =>
          prevItems.map((item) => {
            if (item.id !== userId && item.user_id !== userId) {
              return item;
            }
            if (action === 'delete') {
              return { ...item, DeletedAt: new Date() };
            }
            return {
              ...item,
              status: data?.status ?? item.status,
              role: data?.role ?? item.role,
            };
          }),
        );
      } else {
        showError(message);
      }
    } catch (error) {
      showError(t('鎿嶄綔澶辫触锛岃閲嶈瘯'));
    } finally {
      setLoading(false);
    }
  };

  const resetUserPasskey = async (user) => {
    if (!user) {
      return;
    }
    try {
      const res = await API.delete(`/api/user/${user.id}/reset_passkey`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('Passkey 已重置'));
      } else {
        showError(message || t('鎿嶄綔澶辫触锛岃閲嶈瘯'));
      }
    } catch (error) {
      showError(t('鎿嶄綔澶辫触锛岃閲嶈瘯'));
    }
  };

  const resetUserTwoFA = async (user) => {
    if (!user) {
      return;
    }
    try {
      const res = await API.delete(`/api/user/${user.id}/2fa`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('二步验证已重置'));
      } else {
        showError(message || t('鎿嶄綔澶辫触锛岃閲嶈瘯'));
      }
    } catch (error) {
      showError(t('鎿嶄綔澶辫触锛岃閲嶈瘯'));
    }
  };

  useEffect(() => {
    loadRankings({
      page: 1,
      size: pageSize,
      nextRange: rangeKey,
      nextKeyword: keyword,
    });
  }, []);

  return {
    items,
    summary,
    loading,
    keywordInput,
    keyword,
    rangeKey,
    activePage,
    pageSize,
    total,
    compactMode,
    setCompactMode,
    setKeywordInput,
    handleSearch,
    handleReset,
    handleRangeChange,
    handlePageChange,
    handlePageSizeChange,
    manageUser,
    resetUserPasskey,
    resetUserTwoFA,
    refresh: loadRankings,
    t,
  };
};
