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

export const channelTypeIconMap = {
  1: 'OpenAI', // OpenAI
  3: 'OpenAI', // Azure OpenAI
  57: 'OpenAI', // Codex
  2: 'Midjourney', // Midjourney Proxy
  5: 'Midjourney', // Midjourney Proxy Plus
  36: 'Suno', // Suno API
  4: 'Ollama', // Ollama
  14: 'Claude.Color', // Anthropic Claude
  33: 'Claude.Color', // AWS Claude
  41: 'Gemini.Color', // Vertex AI
  34: 'Cohere.Color', // Cohere
  39: 'Cloudflare.Color', // Cloudflare
  43: 'DeepSeek.Color', // DeepSeek
  58: 'XiaomiMiMo', // Xiaomi MiMo
  15: 'Wenxin.Color', // 百度文心千帆
  46: 'Wenxin.Color', // 百度文心千帆V2
  17: 'Qwen.Color', // 阿里通义千问
  18: 'Spark.Color', // 讯飞星火认知
  16: 'Zhipu.Color', // 智谱 ChatGLM
  26: 'Zhipu.Color', // 智谱 GLM-4V
  24: 'Gemini.Color', // Google Gemini
  11: 'Gemini.Color', // Google PaLM2
  47: 'Xinference.Color', // Xinference
  25: 'Moonshot', // Moonshot
  27: 'Perplexity.Color', // Perplexity
  20: 'OpenRouter', // OpenRouter
  19: 'Ai360.Color', // 360 智脑
  23: 'Hunyuan.Color', // 腾讯混元
  31: 'Yi.Color', // 零一万物
  35: 'Minimax.Color', // MiniMax
  37: 'Dify.Color', // Dify
  38: 'Jina', // Jina
  40: 'SiliconCloud.Color', // SiliconCloud
  42: 'Mistral.Color', // Mistral AI
  45: 'Doubao.Color', // 字节火山方舟、豆包通用
  48: 'XAI', // xAI
  49: 'Coze', // Coze
  50: 'Kling.Color', // 可灵 Kling
  51: 'Jimeng.Color', // 即梦 Jimeng
  54: 'Doubao.Color', // 豆包视频 Doubao Video
  56: 'Replicate', // Replicate
  22: 'FastGPT.Color', // 知识库：FastGPT
};

// A provider keeps the same badge color across all groups and icon variants.
// Each entry carries a `light`/`dark` pair: saturated for light mode and
// luminous for dark mode, so the text stays bright and readable on both.
export const CHANNEL_ICON_COLORS = {
  OpenAI: { light: '#15803D', dark: '#4ADE80' },
  Midjourney: { light: '#4F46E5', dark: '#818CF8' },
  Suno: { light: '#9333EA', dark: '#C084FC' },
  Ollama: { light: '#475569', dark: '#94A3B8' },
  Claude: { light: '#B45309', dark: '#FBBF24' },
  Gemini: { light: '#4F46E5', dark: '#818CF8' },
  Cohere: { light: '#047857', dark: '#34D399' },
  Cloudflare: { light: '#C2410C', dark: '#FB923C' },
  DeepSeek: { light: '#2563EB', dark: '#60A5FA' },
  XiaomiMiMo: { light: '#C2410C', dark: '#FB923C' },
  Wenxin: { light: '#2563EB', dark: '#60A5FA' },
  Qwen: { light: '#7C3AED', dark: '#A78BFA' },
  Spark: { light: '#2563EB', dark: '#60A5FA' },
  Zhipu: { light: '#2563EB', dark: '#60A5FA' },
  Xinference: { light: '#7C3AED', dark: '#A78BFA' },
  Moonshot: { light: '#475569', dark: '#94A3B8' },
  Perplexity: { light: '#0F766E', dark: '#2DD4BF' },
  OpenRouter: { light: '#4F46E5', dark: '#818CF8' },
  Ai360: { light: '#15803D', dark: '#4ADE80' },
  Hunyuan: { light: '#2563EB', dark: '#60A5FA' },
  Yi: { light: '#7C3AED', dark: '#A78BFA' },
  Minimax: { light: '#DC2626', dark: '#F87171' },
  Dify: { light: '#2563EB', dark: '#60A5FA' },
  Jina: { light: '#2563EB', dark: '#60A5FA' },
  SiliconCloud: { light: '#7C3AED', dark: '#A78BFA' },
  Mistral: { light: '#C2410C', dark: '#FB923C' },
  Doubao: { light: '#2563EB', dark: '#60A5FA' },
  XAI: { light: '#475569', dark: '#E2E8F0' },
  Coze: { light: '#2563EB', dark: '#60A5FA' },
  Kling: { light: '#15803D', dark: '#4ADE80' },
  Jimeng: { light: '#0E7490', dark: '#22D3EE' },
  Replicate: { light: '#475569', dark: '#94A3B8' },
  FastGPT: { light: '#2563EB', dark: '#60A5FA' },
};

const GROUP_ICON_FALLBACK_COLORS = { light: '#475569', dark: '#94A3B8' };

export const GROUP_ICON_NAMES = new Set(
  Object.values(channelTypeIconMap).flatMap((name) => [
    name,
    name.split('.')[0],
  ]),
);

export const getGroupIconVariables = (icon) => {
  const entry =
    CHANNEL_ICON_COLORS[icon?.split('.')[0]] || GROUP_ICON_FALLBACK_COLORS;
  return {
    '--group-icon-light': entry.light,
    '--group-icon-dark': entry.dark,
  };
};

// Tag style: luminous text on a subtle same-hue tint, adapting to the
// current theme so the label stays bright and readable in dark mode.
export const getGroupIconStyle = (icon) => {
  return {
    ...getGroupIconVariables(icon),
    color: 'var(--group-icon-color)',
    backgroundColor:
      'color-mix(in srgb, var(--group-icon-color) 10%, transparent)',
    borderColor: 'color-mix(in srgb, var(--group-icon-color) 20%, transparent)',
    fontWeight: 600,
  };
};
