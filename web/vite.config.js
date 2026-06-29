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

import react from '@vitejs/plugin-react';
import { defineConfig, transformWithEsbuild } from 'vite';
import pkg from '@douyinfe/vite-plugin-semi';
import path from 'path';
import { codeInspectorPlugin } from 'code-inspector-plugin';
const { vitePluginSemi } = pkg;

// https://vitejs.dev/config/
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  plugins: [
    codeInspectorPlugin({
      bundler: 'vite',
    }),
    {
      name: 'treat-js-files-as-jsx',
      async transform(code, id) {
        if (!/src\/.*\.js$/.test(id)) {
          return null;
        }

        // Use the exposed transform from vite, instead of directly
        // transforming with esbuild
        return transformWithEsbuild(code, id, {
          loader: 'jsx',
          jsx: 'automatic',
        });
      },
    },
    react(),
    vitePluginSemi({
      cssLayer: true,
    }),
  ],
  optimizeDeps: {
    force: true,
    esbuildOptions: {
      loader: {
        '.js': 'jsx',
        '.json': 'json',
      },
    },
  },
  build: {
    // 降低build内存
    sourcemap: false,
    minify: 'esbuild',
    // 路由级拆包后仍存在 Markdown/Mermaid、Lobe icons 等懒加载库级块。
    // 默认 500KB 阈值对当前后台应用过低，避免每次构建产生误导性告警。
    chunkSizeWarningLimit: 5000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return;
          }

          const normalizedId = id.split(path.sep).join('/');

          if (
            /\/node_modules\/(react|react-dom|react-router-dom)\//.test(
              normalizedId,
            )
          ) {
            return 'react-core';
          }
        },
      },
    },
  },
  server: {
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      '/mj': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      '/pg': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
    },
  },
});
