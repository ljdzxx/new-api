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

import React, { lazy, Suspense, useContext, useMemo } from 'react';
import { Route, Routes, useLocation, useParams } from 'react-router-dom';
import Loading from './components/common/ui/Loading';
import { AuthRedirect, PrivateRoute, AdminRoute } from './helpers/auth';
import { StatusContext } from './context/Status';
import SetupCheck from './components/layout/SetupCheck';

const Home = lazy(() => import('./pages/Home'));
const Setup = lazy(() => import('./pages/Setup'));
const Forbidden = lazy(() => import('./pages/Forbidden'));
const NotFound = lazy(() => import('./pages/NotFound'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const About = lazy(() => import('./pages/About'));
const Availability = lazy(() => import('./pages/Availability'));
const Docs = lazy(() => import('./pages/Docs'));
const UserAgreement = lazy(() => import('./pages/UserAgreement'));
const PrivacyPolicy = lazy(() => import('./pages/PrivacyPolicy'));
const User = lazy(() => import('./pages/User'));
const Setting = lazy(() => import('./pages/Setting'));
const Channel = lazy(() => import('./pages/Channel'));
const Token = lazy(() => import('./pages/Token'));
const Redemption = lazy(() => import('./pages/Redemption'));
const TopUp = lazy(() => import('./pages/TopUp'));
const Log = lazy(() => import('./pages/Log'));
const Chat = lazy(() => import('./pages/Chat'));
const Chat2Link = lazy(() => import('./pages/Chat2Link'));
const Midjourney = lazy(() => import('./pages/Midjourney'));
const Pricing = lazy(() => import('./pages/Pricing'));
const Task = lazy(() => import('./pages/Task'));
const ModelPage = lazy(() => import('./pages/Model'));
const ModelDeploymentPage = lazy(() => import('./pages/ModelDeployment'));
const Playground = lazy(() => import('./pages/Playground'));
const ImageGeneration = lazy(() => import('./pages/ImageGeneration'));
const Subscription = lazy(() => import('./pages/Subscription'));
const UserSubscriptions = lazy(() => import('./pages/UserSubscriptions'));
const Order = lazy(() => import('./pages/Order'));
const UserLevelPage = lazy(() => import('./pages/UserLevel'));
const SubscriptionUsageRank = lazy(() => import('./pages/SubscriptionUsageRank'));
const Lottery = lazy(() => import('./pages/Lottery'));
const LotteryAdmin = lazy(() => import('./pages/LotteryAdmin'));
const RegisterForm = lazy(() => import('./components/auth/RegisterForm'));
const LoginForm = lazy(() => import('./components/auth/LoginForm'));
const PasswordResetForm = lazy(() => import('./components/auth/PasswordResetForm'));
const PasswordResetConfirm = lazy(
  () => import('./components/auth/PasswordResetConfirm'),
);
const OAuth2Callback = lazy(() => import('./components/auth/OAuth2Callback'));
const PersonalSetting = lazy(
  () => import('./components/settings/PersonalSetting'),
);

function DynamicOAuth2Callback() {
  const { provider } = useParams();
  return <OAuth2Callback type={provider} />;
}

function App() {
  const location = useLocation();
  const [statusState] = useContext(StatusContext);

  // 获取模型广场权限配置
  const pricingRequireAuth = useMemo(() => {
    const headerNavModulesConfig = statusState?.status?.HeaderNavModules;
    if (headerNavModulesConfig) {
      try {
        const modules = JSON.parse(headerNavModulesConfig);

        // 处理向后兼容性：如果pricing是boolean，默认不需要登录
        if (typeof modules.pricing === 'boolean') {
          return false; // 默认不需要登录鉴权
        }

        // 如果是对象格式，使用requireAuth配置
        return modules.pricing?.requireAuth === true;
      } catch (error) {
        console.error('解析顶栏模块配置失败:', error);
        return false; // 默认不需要登录
      }
    }
    return false; // 默认不需要登录
  }, [statusState?.status?.HeaderNavModules]);

  const withSuspense = (children) => (
    <Suspense fallback={<Loading></Loading>} key={location.pathname}>
      {children}
    </Suspense>
  );

  const withPrivateRoute = (children) => (
    <PrivateRoute>{withSuspense(children)}</PrivateRoute>
  );

  const withAdminRoute = (children) => (
    <AdminRoute>{withSuspense(children)}</AdminRoute>
  );

  return (
    <SetupCheck>
      <Routes>
        <Route path='/' element={withSuspense(<Home />)} />
        <Route path='/setup' element={withSuspense(<Setup />)} />
        <Route path='/forbidden' element={withSuspense(<Forbidden />)} />
        <Route
          path='/console/models'
          element={withAdminRoute(<ModelPage />)}
        />
        <Route
          path='/console/deployment'
          element={withAdminRoute(<ModelDeploymentPage />)}
        />
        <Route
          path='/console/subscription'
          element={withAdminRoute(<Subscription />)}
        />
        <Route
          path='/console/channel'
          element={withAdminRoute(<Channel />)}
        />
        <Route
          path='/console/token'
          element={withPrivateRoute(<Token />)}
        />
        <Route
          path='/console/playground'
          element={withPrivateRoute(<Playground />)}
        />
        <Route
          path='/console/image-generation'
          element={withPrivateRoute(<ImageGeneration />)}
        />
        <Route
          path='/console/redemption'
          element={withAdminRoute(<Redemption />)}
        />
        <Route
          path='/console/lottery-admin'
          element={withAdminRoute(<LotteryAdmin />)}
        />
        <Route
          path='/console/user'
          element={withAdminRoute(<User />)}
        />
        <Route
          path='/console/user-subscriptions'
          element={withAdminRoute(<UserSubscriptions />)}
        />
        <Route
          path='/console/subscription-rank'
          element={withAdminRoute(<SubscriptionUsageRank />)}
        />
        <Route
          path='/user/reset'
          element={withSuspense(<PasswordResetConfirm />)}
        />
        <Route
          path='/login'
          element={withSuspense(
            <AuthRedirect>
              <LoginForm />
            </AuthRedirect>,
          )}
        />
        <Route
          path='/register'
          element={withSuspense(
            <AuthRedirect>
              <RegisterForm />
            </AuthRedirect>,
          )}
        />
        <Route
          path='/reset'
          element={withSuspense(<PasswordResetForm />)}
        />
        <Route
          path='/oauth/github'
          element={withSuspense(<OAuth2Callback type='github' />)}
        />
        <Route
          path='/oauth/discord'
          element={withSuspense(<OAuth2Callback type='discord' />)}
        />
        <Route
          path='/oauth/oidc'
          element={withSuspense(<OAuth2Callback type='oidc' />)}
        />
        <Route
          path='/oauth/linuxdo'
          element={withSuspense(<OAuth2Callback type='linuxdo' />)}
        />
        <Route
          path='/oauth/:provider'
          element={withSuspense(<DynamicOAuth2Callback />)}
        />
        <Route
          path='/console/setting'
          element={withAdminRoute(<Setting />)}
        />
        <Route
          path='/console/personal'
          element={withPrivateRoute(<PersonalSetting />)}
        />
        <Route
          path='/console/topup'
          element={withPrivateRoute(<TopUp />)}
        />
        <Route
          path='/console/order'
          element={withAdminRoute(<Order />)}
        />
        <Route
          path='/console/level'
          element={withPrivateRoute(<UserLevelPage />)}
        />
        <Route
          path='/console/lottery'
          element={withPrivateRoute(<Lottery />)}
        />
        <Route path='/lottery' element={withSuspense(<Lottery />)} />
        <Route
          path='/console/log'
          element={withPrivateRoute(<Log />)}
        />
        <Route
          path='/console'
          element={withPrivateRoute(<Dashboard />)}
        />
        <Route
          path='/console/midjourney'
          element={withPrivateRoute(<Midjourney />)}
        />
        <Route
          path='/console/task'
          element={withPrivateRoute(<Task />)}
        />
        <Route
          path='/pricing'
          element={
            pricingRequireAuth ? (
              withPrivateRoute(<Pricing />)
            ) : (
              withSuspense(<Pricing />)
            )
          }
        />
        <Route path='/docs' element={withSuspense(<Docs />)} />
        <Route path='/about' element={withSuspense(<About />)} />
        <Route
          path='/availability'
          element={withSuspense(<Availability />)}
        />
        <Route
          path='/user-agreement'
          element={withSuspense(<UserAgreement />)}
        />
        <Route
          path='/privacy-policy'
          element={withSuspense(<PrivacyPolicy />)}
        />
        <Route
          path='/console/chat/:id?'
          element={withSuspense(<Chat />)}
        />
        {/* 方便使用chat2link直接跳转聊天... */}
        <Route
          path='/chat2link'
          element={withPrivateRoute(<Chat2Link />)}
        />
        <Route path='*' element={withSuspense(<NotFound />)} />
      </Routes>
    </SetupCheck>
  );
}

export default App;
