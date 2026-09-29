import React, { useState } from 'react';
import { API } from '../../helpers';

export default function HTMLPreview({ artwork, title }) {
  // The parent keys this component by artwork ID. Keep its navigation URL for
  // its lifetime: a new signature from polling is not a different document.
  const [src] = useState(() => {
    if (!artwork.preview_url) return artwork.html_url;
    return new URL(
      artwork.preview_url,
      API.defaults.baseURL || window.location.origin,
    ).href;
  });
  return (
    <iframe
      title={title}
      sandbox='allow-scripts'
      referrerPolicy='no-referrer'
      loading='lazy'
      scrolling='no'
      src={src}
      className='monitor-html-preview'
    />
  );
}
