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

import React, {
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react';
import { Button, Input, ScrollItem, ScrollList } from '@douyinfe/semi-ui';
import { IconCopy } from '@douyinfe/semi-icons';
import {
  ArrowRight,
  BookOpen,
  MoreHorizontal,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import { marked } from 'marked';
import { useTranslation } from 'react-i18next';

import NoticeModal from '../../components/layout/NoticeModal';
import { API_ENDPOINTS } from '../../constants/common.constant';
import { StatusContext } from '../../context/Status';
import { useActualTheme } from '../../context/Theme';
import { UserContext } from '../../context/User';
import { useIsMobile } from '../../hooks/common/useIsMobile';
import { API } from '../../helpers/api';
import { copy, showError, showSuccess } from '../../helpers/utils';

const API_DEMOS = [
  {
    id: 'chat',
    label: 'Chat',
    method: 'POST',
    endpoint: '/v1/chat/completions',
    headers: ['"Authorization: Bearer sk-****"'],
    request: [
      '"model": "your-model",',
      '"messages": [',
      '  { "role": "user", "content": "..." }',
      ']',
    ],
    response: [
      '{',
      '  "choices": [{ "message": { "content": <text> } }],',
      '  "usage": { "total_tokens": <tokens> }',
      '}',
    ],
    text: 'Chat request routed.',
    tokens: 27,
    latency: 142,
    accent: 'emerald',
  },
  {
    id: 'responses',
    label: 'Responses',
    method: 'POST',
    endpoint: '/v1/responses',
    headers: ['"Authorization: Bearer sk-****"'],
    request: ['"model": "your-model",', '"input": "..."'],
    response: [
      '{',
      '  "output": [{ "type": "output_text", "text": <text> }],',
      '  "usage": { "total_tokens": <tokens> }',
      '}',
    ],
    text: 'Response workflow ready.',
    tokens: 31,
    latency: 168,
    accent: 'amber',
  },
  {
    id: 'claude',
    label: 'Claude',
    method: 'POST',
    endpoint: '/v1/messages',
    headers: ['"x-api-key: sk-****"', '"anthropic-version: 2023-06-01"'],
    request: [
      '"model": "your-model",',
      '"max_tokens": 1024,',
      '"messages": [',
      '  { "role": "user", "content": "..." }',
      ']',
    ],
    response: [
      '{',
      '  "content": [{ "type": "text", "text": <text> }],',
      '  "usage": { "input_tokens": <in>, "output_tokens": <out> }',
      '}',
    ],
    text: 'Claude message routed.',
    tokens: 29,
    latency: 156,
    accent: 'blue',
  },
  {
    id: 'gemini',
    label: 'Gemini',
    method: 'POST',
    endpoint: '/v1beta/models/{model}:generateContent',
    headers: ['"x-goog-api-key: sk-****"'],
    request: [
      '"contents": [',
      '  { "role": "user",',
      '    "parts": [{ "text": "..." }] }',
      ']',
    ],
    response: [
      '{',
      '  "candidates": [{ "content": { "parts": [{ "text": <text> }] } }],',
      '  "usageMetadata": { "totalTokenCount": <tokens> }',
      '}',
    ],
    text: 'Gemini request served.',
    tokens: 25,
    latency: 93,
    accent: 'violet',
  },
];

const isHttpUrl = (value) => {
  if (!value) return false;
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
};

const renderJsonLine = (line, demo) => {
  const replaced = line
    .replace('<text>', `"${demo.text}"`)
    .replace('<tokens>', `${demo.tokens}`)
    .replace('<in>', `${Math.floor(demo.tokens * 0.4)}`)
    .replace('<out>', `${Math.ceil(demo.tokens * 0.6)}`);

  return replaced.split(/("[^"]*"|-X|-H|-d|\d+)/g).map((part, index) => {
    if (!part) return null;
    if (part.startsWith('"')) {
      const isKey = replaced
        .slice(replaced.indexOf(part) + part.length)
        .trimStart()
        .startsWith(':');
      return (
        <span
          key={index}
          className={isKey ? 'home-code-key' : 'home-code-string'}
        >
          {part}
        </span>
      );
    }
    if (part === '-X' || part === '-H' || part === '-d') {
      return (
        <span key={index} className='home-code-flag'>
          {part}
        </span>
      );
    }
    if (/^\d+$/.test(part)) {
      return (
        <span key={index} className='home-code-number'>
          {part}
        </span>
      );
    }
    return (
      <span key={index} className='home-code-muted'>
        {part}
      </span>
    );
  });
};

function HeroTerminalDemo() {
  const [activeIndex, setActiveIndex] = useState(0);
  const [transitioning, setTransitioning] = useState(false);
  const intervalRef = useRef(undefined);
  const timeoutRef = useRef(undefined);
  const demo = API_DEMOS[activeIndex];

  useEffect(() => {
    const mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)');
    if (mediaQuery.matches) return undefined;

    intervalRef.current = window.setInterval(() => {
      setTransitioning(true);
      timeoutRef.current = window.setTimeout(() => {
        setActiveIndex((prev) => (prev + 1) % API_DEMOS.length);
        setTransitioning(false);
      }, 220);
    }, 4500);

    return () => {
      window.clearInterval(intervalRef.current);
      window.clearTimeout(timeoutRef.current);
    };
  }, []);

  const handleSelect = (index) => {
    if (index === activeIndex) return;
    window.clearInterval(intervalRef.current);
    window.clearTimeout(timeoutRef.current);
    setTransitioning(true);
    timeoutRef.current = window.setTimeout(() => {
      setActiveIndex(index);
      setTransitioning(false);
    }, 220);
  };

  return (
    <div className='home-terminal'>
      <div className='home-terminal-tabs'>
        {API_DEMOS.map((item, index) => (
          <button
            key={item.id}
            type='button'
            onClick={() => handleSelect(index)}
            className={`home-terminal-tab home-terminal-tab-${item.accent} ${
              index === activeIndex ? 'home-terminal-tab-active' : ''
            }`}
          >
            {item.label}
          </button>
        ))}
        <div className='home-terminal-status'>
          <span className='home-terminal-status-dot' />
          <span>200 ok</span>
        </div>
      </div>

      <div className='home-terminal-endpoint'>
        <span className={`home-method home-method-${demo.accent}`}>
          {demo.method}
        </span>
        <code className={transitioning ? 'home-fade-out' : ''}>
          {demo.endpoint}
        </code>
      </div>

      <div className='home-terminal-body'>
        <div className='home-terminal-block'>
          <span className='home-terminal-label'>Request</span>
          <div className={transitioning ? 'home-fade-out' : ''}>
            <div>
              <span className='home-code-command'>curl</span>{' '}
              <span className='home-code-flag'>-X</span>{' '}
              <span className='home-code-flag'>POST</span>{' '}
              <span className='home-code-string'>
                &quot;{demo.endpoint}&quot;
              </span>{' '}
              <span className='home-code-muted'>\</span>
            </div>
            {demo.headers.map((header) => (
              <div key={header} className='home-code-indent-2'>
                <span className='home-code-flag'>-H</span>{' '}
                <span className='home-code-string'>{header}</span>{' '}
                <span className='home-code-muted'>\</span>
              </div>
            ))}
            <div className='home-code-indent-2'>
              <span className='home-code-flag'>-d</span>{' '}
              <span className='home-code-string'>&apos;{'{'}</span>
            </div>
            {demo.request.map((line) => (
              <div key={line} className='home-code-indent-4'>
                {renderJsonLine(line, demo)}
              </div>
            ))}
            <div className='home-code-indent-2'>
              <span className='home-code-string'>{'}'}&apos;</span>
            </div>
          </div>
        </div>

        <div className='home-terminal-block home-terminal-response'>
          <span className='home-terminal-label'>Response</span>
          <div className={transitioning ? 'home-fade-out' : ''}>
            {demo.response.map((line) => (
              <div key={line}>{renderJsonLine(line, demo)}</div>
            ))}
          </div>
        </div>
      </div>

      <div className='home-terminal-footer'>
        <span>{demo.latency} ms</span>
        <span>{demo.tokens} tokens</span>
        <span>${(demo.tokens * 0.00003).toFixed(5)}</span>
        <span>stream / sse</span>
      </div>
    </div>
  );
}

function EndpointBar({
  endpointItems,
  endpointIndex,
  handleCopyBaseURL,
  isMobile,
  serverAddress,
  setEndpointIndex,
}) {
  return (
    <div className='home-endpoint-bar'>
      <Input
        readOnly
        value={serverAddress}
        className='home-endpoint-input'
        size={isMobile ? 'default' : 'large'}
        suffix={
          <div className='home-endpoint-suffix'>
            <ScrollList
              bodyHeight={32}
              style={{ border: 'unset', boxShadow: 'unset' }}
            >
              <ScrollItem
                mode='wheel'
                cycled
                list={endpointItems}
                selectedIndex={endpointIndex}
                onSelect={({ index }) => setEndpointIndex(index)}
              />
            </ScrollList>
            <Button
              type='primary'
              onClick={handleCopyBaseURL}
              icon={<IconCopy />}
              className='home-copy-button'
              aria-label='Copy base URL'
            />
          </div>
        }
      />
    </div>
  );
}

function CherryStudioLogo({ className, size = 24 }) {
  return (
    <svg
      className={className}
      height={size}
      style={{ flex: 'none', lineHeight: 1 }}
      viewBox='0 0 24 24'
      width={size}
      xmlns='http://www.w3.org/2000/svg'
      aria-hidden='true'
    >
      <path
        d='M6.513 18.419c-1.6 0-3.107-.64-4.247-1.802A6.146 6.146 0 01.5 12.287c0-1.63.626-3.168 1.766-4.33 1.14-1.162 2.647-1.802 4.247-1.802s3.132.655 4.25 1.795c.835.849.835 2.23 0 3.078a2.11 2.11 0 01-3.02 0 1.737 1.737 0 00-1.234-.521c-.945 0-1.744.813-1.744 1.776 0 .964.799 1.777 1.744 1.777.46 0 .907-.19 1.234-.522a2.11 2.11 0 013.02 0c.835.85.835 2.23 0 3.079a5.997 5.997 0 01-4.25 1.794v.008z'
        fill='#EA5E5D'
      />
      <path
        d='M12.026 24c-1.6 0-3.107-.64-4.247-1.802a6.146 6.146 0 01-1.766-4.33c0-1.63.644-3.193 1.762-4.337a2.11 2.11 0 013.021 0c.834.849.834 2.23 0 3.078-.324.331-.51.788-.51 1.255 0 .964.798 1.777 1.744 1.777.945 0 1.744-.813 1.744-1.777 0-.341-.083-.83-.475-1.233a6.255 6.255 0 01-1.77-4.348c0-1.615.627-3.168 1.767-4.33s2.646-1.802 4.247-1.802c1.6 0 3.107.64 4.247 1.802a6.146 6.146 0 011.766 4.33c0 1.63-.644 3.194-1.762 4.337a2.11 2.11 0 01-3.021 0 2.206 2.206 0 010-3.078c.323-.331.51-.788.51-1.255 0-.964-.798-1.777-1.744-1.777s-1.744.813-1.744 1.777c0 .47.19.935.521 1.27 1.115 1.136 1.727 2.667 1.727 4.311a6.122 6.122 0 01-1.766 4.33C15.137 23.36 13.63 24 12.03 24h-.004z'
        fill='#EA5E5D'
      />
      <path
        d='M12.026 6.867L8.53 3.587a1.336 1.336 0 111.827-1.949l1.4 1.313L13.744.495a1.336 1.336 0 012.075 1.68l-3.798 4.692h.004z'
        fill='#23AF69'
      />
    </svg>
  );
}

function HomeHero({
  docsLink,
  endpointItems,
  endpointIndex,
  handleCopyBaseURL,
  isAuthenticated,
  isMobile,
  serverAddress,
  setEndpointIndex,
  t,
}) {
  const openDocs = () => {
    const target = docsLink || '/docs';
    //window.open(target, '_self', 'noopener,noreferrer');
  };

  return (
    <section className='home-hero-section'>
      <div className='home-hero-mesh' aria-hidden />
      <div className='home-hero-grid-pattern' aria-hidden />

      <div className='home-container home-hero-grid'>
        <div className='home-hero-copy'>
          <div
            className='home-pill home-animate'
            style={{ animationDelay: '0ms' }}
          >
            <span className='home-pill-ping' />
            <span>{t('AI Application Infrastructure Foundation')}</span>
          </div>

          <h1
            className='home-title home-animate'
            style={{ animationDelay: '60ms' }}
          >
            {t('Unified API Gateway for')}
            <br />
            <span>{t('Vast Range of AI Models')}</span>
          </h1>

          <p
            className='home-subtitle home-animate'
            style={{ animationDelay: '120ms' }}
          >
            {t(
              'Access a vast selection of models via a standard, unified API protocol. Power AI applications, manage digital assets, and connect the Future.',
            )}
          </p>

          <div
            className='home-actions home-animate'
            style={{ animationDelay: '180ms' }}
          >
            <Link to={isAuthenticated ? '/console' : '/register'}>
              <Button
                theme='solid'
                type='primary'
                size={isMobile ? 'default' : 'large'}
                className='home-primary-action'
              >
                <span>
                  {isAuthenticated ? t('Go to Dashboard') : t('Get Started')}
                </span>
                <ArrowRight size={16} />
              </Button>
            </Link>
            <Link to='/pricing'>
              <Button
                size={isMobile ? 'default' : 'large'}
                className='home-secondary-action'
              >
                {t('View Pricing')}
              </Button>
            </Link>

            <Link to='/docs'>
              <Button
                size={isMobile ? 'default' : 'large'}
                className='home-secondary-action'
                icon={<BookOpen size={16} />}
              >
                {t('Docs')}
              </Button>
            </Link>
          </div>

          <div className='home-animate' style={{ animationDelay: '240ms' }}>
            <EndpointBar
              endpointItems={endpointItems}
              endpointIndex={endpointIndex}
              handleCopyBaseURL={handleCopyBaseURL}
              isMobile={isMobile}
              serverAddress={serverAddress}
              setEndpointIndex={setEndpointIndex}
            />
          </div>

          <div
            className='home-supported-apps home-animate'
            style={{ animationDelay: '300ms' }}
          >
            <div>
              <span>{t('Supported Applications')}</span>
              <p>
                {t(
                  'Supports one-click configuration and perfectly adapts to NewAPI multi-protocol configuration.',
                )}
              </p>
            </div>
            <div className='home-app-row'>
              <a href='https://cherry-ai.com' target='_blank' rel='noreferrer'>
                <CherryStudioLogo size={24} className='home-app-icon' />
                <span>Cherry Studio</span>
              </a>
              <a href='https://ccswitch.io' target='_blank' rel='noreferrer'>
                <img
                  src='https://ccswitch.io/favicon.png'
                  alt='CC Switch'
                  className='home-app-logo-image'
                  onError={(e) => {
                    e.currentTarget.style.display = 'none';
                    const fallback = e.currentTarget.nextElementSibling;
                    if (fallback) {
                      fallback.style.display = 'inline-flex';
                    }
                  }}
                />
                <span className='home-app-logo home-app-logo-fallback'>CC</span>
                <span>CC Switch</span>
              </a>
              <div className='home-app-more'>
                <MoreHorizontal size={22} />
                <span>{t('More Apps')}</span>
              </div>
            </div>
          </div>
        </div>

        <div
          className='home-terminal-wrap home-animate'
          style={{ animationDelay: '360ms' }}
        >
          <HeroTerminalDemo />
        </div>
      </div>
    </section>
  );
}

function CtaSection({ isAuthenticated, t }) {
  if (isAuthenticated) return null;

  return (
    <section className='home-cta-section'>
      <div className='home-cta-mesh' aria-hidden />
      <div className='home-container home-cta-content'>
        <h2>
          {t('Ready to simplify')}
          <br />
          <span>{t('your AI integration?')}</span>
        </h2>
        <p>
          {t(
            'Deploy your own gateway and start routing requests through your configured upstream services.',
          )}
        </p>
        <div className='home-cta-actions'>
          <Link to='/register'>
            <Button
              theme='solid'
              type='primary'
              className='home-primary-action'
            >
              <span>{t('Get Started')}</span>
              <ArrowRight size={15} />
            </Button>
          </Link>
          <Link to='/pricing'>
            <Button className='home-secondary-action'>
              {t('View Pricing')}
            </Button>
          </Link>
        </div>
      </div>
    </section>
  );
}

function DefaultHome(props) {
  return (
    <div className='home-landing'>
      <HomeHero {...props} />
      <CtaSection isAuthenticated={props.isAuthenticated} t={props.t} />
    </div>
  );
}

const Home = () => {
  const { t, i18n } = useTranslation();
  const [statusState] = useContext(StatusContext);
  const [userState] = useContext(UserContext);
  const actualTheme = useActualTheme();
  const [homePageContentLoaded, setHomePageContentLoaded] = useState(false);
  const [homePageContent, setHomePageContent] = useState('');
  const [noticeVisible, setNoticeVisible] = useState(false);
  const isMobile = useIsMobile();
  const docsLink = statusState?.status?.docs_link || '';
  const serverAddress =
    statusState?.status?.server_address || 'https://api.jucodex.com';
  const endpointItems = API_ENDPOINTS.map((e) => ({ value: e }));
  const [endpointIndex, setEndpointIndex] = useState(0);
  const isAuthenticated = !!userState?.user;

  const displayHomePageContent = async () => {
    setHomePageContent(localStorage.getItem('home_page_content') || '');
    const res = await API.get('/api/home_page_content');
    const { success, message, data } = res.data;
    if (success) {
      const rawContent = typeof data === 'string' ? data : '';
      const content =
        rawContent && !isHttpUrl(rawContent)
          ? marked.parse(rawContent)
          : rawContent;

      setHomePageContent(content);
      if (content) {
        localStorage.setItem('home_page_content', content);
      } else {
        localStorage.removeItem('home_page_content');
      }

      if (isHttpUrl(rawContent)) {
        const iframe = document.querySelector('iframe');
        if (iframe) {
          iframe.onload = () => {
            iframe.contentWindow.postMessage({ themeMode: actualTheme }, '*');
            iframe.contentWindow.postMessage({ lang: i18n.language }, '*');
          };
        }
      }
    } else {
      showError(message);
      setHomePageContent(t('Failed to load home page content...'));
    }
    setHomePageContentLoaded(true);
  };

  const handleCopyBaseURL = async () => {
    const ok = await copy(serverAddress);
    if (ok) {
      showSuccess(t('Copied to clipboard'));
    }
  };

  useEffect(() => {
    const checkNoticeAndShow = async () => {
      const lastCloseDate = localStorage.getItem('notice_close_date');
      const today = new Date().toDateString();
      if (lastCloseDate !== today) {
        try {
          const res = await API.get('/api/notice');
          const { success, data } = res.data;
          if (success && data && data.trim() !== '') {
            setNoticeVisible(true);
          }
        } catch (error) {
          console.error('获取公告失败:', error);
        }
      }
    };

    checkNoticeAndShow();
  }, []);

  useEffect(() => {
    displayHomePageContent().then();
  }, []);

  useEffect(() => {
    const timer = setInterval(() => {
      setEndpointIndex((prev) => (prev + 1) % endpointItems.length);
    }, 3000);
    return () => clearInterval(timer);
  }, [endpointItems.length]);

  return (
    <div className='w-full overflow-x-hidden'>
      <NoticeModal
        visible={noticeVisible}
        onClose={() => setNoticeVisible(false)}
        isMobile={isMobile}
      />
      {homePageContentLoaded && homePageContent === '' ? (
        <DefaultHome
          docsLink={docsLink}
          endpointItems={endpointItems}
          endpointIndex={endpointIndex}
          handleCopyBaseURL={handleCopyBaseURL}
          isAuthenticated={isAuthenticated}
          isMobile={isMobile}
          serverAddress={serverAddress}
          setEndpointIndex={setEndpointIndex}
          t={t}
        />
      ) : (
        <div className='overflow-x-hidden w-full'>
          {isHttpUrl(homePageContent) ? (
            <iframe
              src={homePageContent}
              title={t('Custom Home Page')}
              className='w-full h-screen border-none'
            />
          ) : (
            <div
              className='mt-[60px]'
              dangerouslySetInnerHTML={{ __html: homePageContent }}
            />
          )}
        </div>
      )}
    </div>
  );
};

export default Home;
