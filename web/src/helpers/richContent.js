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

import { marked } from 'marked';

const htmlTagPattern = /^<(?:!DOCTYPE|!--|\/?[a-zA-Z][\w:-]*)(?:\s[^>]*)?>/i;

export const renderRichContent = (content) => {
  if (typeof content !== 'string') return '';

  const normalized = content.replace(/\r\n?/g, '\n');
  const trimmed = normalized.trim();
  if (!trimmed) return '';

  // Preserve complete or indented HTML snippets instead of turning them into
  // Markdown code blocks. Markdown content can still contain inline HTML.
  if (htmlTagPattern.test(trimmed)) {
    return trimmed;
  }

  return marked.parse(normalized);
};
