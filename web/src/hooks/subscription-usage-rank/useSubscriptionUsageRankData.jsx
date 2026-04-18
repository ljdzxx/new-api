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
import { API, showError } from '../../helpers';
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

      setItems(pageData.items || []);
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
    refresh: loadRankings,
    t,
  };
};
