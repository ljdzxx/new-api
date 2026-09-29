import React, { useState } from 'react';
import { API } from '../../helpers';
import { useTranslation } from 'react-i18next';

export default function HTMLPreview({ artwork, title, onExpand }) {
  const { t } = useTranslation();
  // The parent keys this component by artwork ID. Keep its navigation URL for
  // its lifetime: a new signature from polling is not a different document.
  const [src] = useState(() => {
    if (!artwork.preview_url) return artwork.html_url;
    return new URL(
      artwork.preview_url,
      API.defaults.baseURL || window.location.origin,
    ).href;
  });
  const frame = (
    <iframe
      title={title}
      sandbox='allow-scripts'
      referrerPolicy='no-referrer'
      loading='lazy'
      scrolling='no'
      src={src}
      className='monitor-html-preview'
      tabIndex={onExpand ? -1 : undefined}
      aria-hidden={onExpand ? true : undefined}
    />
  );
  return onExpand ? (
    <div className='monitor-preview-thumbnail'>
      {frame}
      <button
        type='button'
        className='monitor-preview-open'
        aria-label={`${title} · ${t('放大预览')}`}
        onClick={onExpand}
      />
    </div>
  ) : (
    frame
  );
}
