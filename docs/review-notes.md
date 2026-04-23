# Project Review Notes

Last updated: 2026-04-24

This note summarizes the static review findings and codebase entry points that were identified during the review session. It is intended as a quick handoff file for future sessions.

## 1. Architecture Summary

- Startup entry: `main.go`
- Initialization flow: `main.go` -> `InitResources()` -> `model.InitDB()` -> `model.CheckSetup()` -> router setup
- Router aggregation: `router/main.go`
- Relay entry: `controller/relay.go`
- Relay adaptor dispatch: `relay/relay_adaptor.go`
- Database layer: `model/`
- Frontend app entry: `web/src/App.jsx`

Main architecture observation:

- The high-level layering is clear, but runtime state is heavily passed through `gin.Context` across middleware, controller, relay, and service layers.
- `main.go` has become a broad runtime orchestrator and already owns multiple background tasks.

## 2. Code Quality Findings

- Project convention says JSON marshal/unmarshal should use `common/json.go`, but many files still directly use `encoding/json`.
- Representative file: `controller/user.go`
- Source files contain many garbled comments/strings, which increases maintenance risk and makes review harder.

## 3. Potential Bug / Risk Findings

### 3.1 Video proxy credential snapshot mismatch

Files:

- `controller/video_proxy.go`
- `model/task.go`

Observation:

- Historical video fetch for OpenAI/Sora uses current `channel.Key`.
- Task private data only snapshots provider key for Gemini/Vertex.
- If channel keys rotate or multi-key selection changes, old task content fetch may fail.

### 3.2 Auth middleware returns 200 for auth failures

File:

- `middleware/auth.go`

Observation:

- Several unauthenticated or unauthorized branches return `200 OK` instead of `401/403`.
- This can break frontend behavior, proxy behavior, monitoring, and cache semantics.

### 3.3 WebSocket Origin check is fully open

File:

- `controller/relay.go`

Observation:

- WebSocket upgrader uses `CheckOrigin: return true`.
- This is a clear security risk if browser-based realtime access is used.

### 3.4 Session cookie is not marked Secure

File:

- `main.go`

Observation:

- Session options set `Secure: false`.
- This is a deployment risk in HTTPS environments.

## 4. Testing Gaps

Current tests are concentrated in:

- DTO zero-value behavior
- Relay conversion / override logic
- Some billing logic

Weak coverage areas:

- `middleware/auth.go`
- `controller/video_proxy.go`
- Session / cookie security behavior
- WebSocket security behavior
- Cross-database migration behavior for SQLite/MySQL/PostgreSQL

## 5. Frontend Main Menu Entry Points

There are two menu/navigation systems in `web/`:

### 5.1 Left sidebar main menu

Primary file:

- `web/src/components/layout/SiderBar.jsx`

Important code blocks:

- `routerMap`
- `workspaceItems`
- `financeItems`
- `adminItems`
- `chatMenuItems`

Related files when adding a new sidebar menu item and page:

- `web/src/components/layout/SiderBar.jsx`
- `web/src/App.jsx`
- `web/src/helpers/render.jsx`
- new page file under `web/src/pages/...`

If sidebar visibility needs admin/user configuration, also update:

- `web/src/hooks/common/useSidebar.js`
- `web/src/pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx`
- `web/src/components/settings/personal/cards/NotificationSettings.jsx`

### 5.2 Top header navigation

Primary files:

- `web/src/hooks/common/useNavigation.js`
- `web/src/components/layout/headerbar/Navigation.jsx`

If a new top nav item is needed, also inspect:

- `web/src/hooks/common/useHeaderBar.js`
- `web/src/pages/Setting/Operation/SettingsHeaderNavModules.jsx`

## 6. Database Initialization Notes

### 6.1 Where database initialization happens

Primary files:

- `main.go`
- `model/main.go`

Initialization path:

- `main.go` calls `InitResources()`
- `InitResources()` calls `model.InitDB()`
- then `model.CheckSetup()`

### 6.2 Database framework

Backend database framework:

- GORM v2

Drivers in use:

- MySQL: `gorm.io/driver/mysql`
- PostgreSQL: `gorm.io/driver/postgres`
- SQLite: `github.com/glebarez/sqlite`

### 6.3 How first-run initialization is detected

Primary files:

- `model/main.go`
- `model/setup.go`

Observation:

- The project does not mainly rely on "does a table exist" as the setup check.
- It checks the setup record via `GetSetup()`.
- If setup record exists, system is considered initialized.
- If setup record does not exist, it checks whether a root user exists.
- If root user exists, it backfills a setup record and marks initialized.
- If neither exists, it marks the system as not initialized.

### 6.4 Where the SQL script is

There is no single canonical SQL script for first-time database creation.

Primary mechanism:

- GORM `AutoMigrate()` in `model/main.go`

Exceptions:

- Some SQLite compatibility DDL is written manually in code, such as `ensureSubscriptionPlanTableSQLite()`
- Some schema patches use `DB.Exec(...)` inside migration helpers

Historical SQL files exist under `bin/`, but they appear to be upgrade scripts rather than the main first-time initialization path:

- `bin/migration_v0.2-v0.3.sql`
- `bin/migration_v0.3-v0.4.sql`

## 7. Practical Reuse Note For Future Sessions

If a future session needs prior context, ask the agent to read:

- `AGENTS.md`

## 8. 2026-04-18 User Management Changes

This section summarizes the user-management related feature work and UI adjustments completed after the earlier review notes.

### 8.1 Daily subscription usage statistics

Goal:

- Support real-time daily subscription quota statistics per user.
- Persist daily records instead of only relying on the mutable `user_subscriptions.amount_used` current-cycle value.
- Expose the data from the admin user list via a modal with pagination.

Backend changes:

- Added daily statistics model:
  - `model/subscription_daily_stat.go`
- Added migration registration:
  - `model/main.go`
- Added real-time/stateless synchronization hooks around subscription lifecycle changes:
  - `model/subscription.go`
- Added periodic补全/snapshot maintenance from the reset task:
  - `service/subscription_reset_task.go`
- Added admin query API:
  - `controller/user_subscription_daily_stat.go`
  - `router/api-router.go`

Frontend changes:

- Added user statistics modal:
  - `web/src/components/table/users/modals/UserSubscriptionStatsModal.jsx`
- Added entry point wiring from the user list:
  - `web/src/components/table/users/UsersTable.jsx`
  - `web/src/components/table/users/UsersColumnDefs.jsx`

UI behavior:

- Summary cards for today's total/used/remain quota and active subscriptions.
- Tabbed display for daily aggregates and per-subscription details.
- Supports filters and server-side pagination.

### 8.2 User list support for redemption-code reverse lookup

Goal:

- Allow admin to search users by redemption code, so a specific redeemed code can be traced back to the user who used it.

Changed files:

- `web/src/components/table/users/UsersFilters.jsx`
- `web/src/hooks/users/useUsersData.jsx`
- `controller/user.go`
- `model/user.go`

Notes:

- The redemption code field was added to the user-list query conditions.
- Search logic was extended on the backend instead of doing page-local filtering.

### 8.3 Redemption records modal

Goal:

- Show how many redemption codes a user has used.
- Provide a paginated detail modal for each user's redemption history.

Backend changes:

- Added redemption-record query endpoint:
  - `controller/user_redemption.go`
  - `model/user_redemption.go`
  - `router/api-router.go`

Model changes:

- Added `redemption_id` association on subscription records so redemption-created subscriptions can be linked more accurately:
  - `model/subscription.go`
  - `model/redemption.go`

Frontend changes:

- Added redemption records modal:
  - `web/src/components/table/users/modals/UserRedemptionRecordsModal.jsx`
- Added user-list column and action:
  - `web/src/components/table/users/UsersColumnDefs.jsx`
  - `web/src/components/table/users/UsersTable.jsx`

Display rules:

- Subscription redemption and quota redemption are shown separately.
- For quota redemption:
  - source/status are shown explicitly instead of pretending it is a subscription.
  - subscription start/end time remain empty.
- Admin-granted subscriptions are intentionally excluded from redemption records because they are not redemption events.

Later enhancement:

- Added status filter support in the redemption modal.
- Added a dedicated `来源` column to explicitly distinguish:
  - `兑换订阅`
  - `额度兑换`

### 8.4 Subscription source records

Goal:

- Provide a unified view of how a subscription was created:
  - admin gift
  - redemption
  - paid purchase

Backend work completed:

- `model/user_subscription_source.go`
- `controller/user_subscription_source.go`
- `router/api-router.go`

Frontend work completed:

- `web/src/components/table/users/modals/UserSubscriptionSourcesModal.jsx`

Current status:

- The backend endpoint and modal component exist.
- The `订阅来源` column was later removed from the user list to reduce table width pressure.
- So this capability is currently implemented but not exposed from the user table entry column.

### 8.5 User list table layout and interaction adjustments

The `/console/user` table had grown significantly, which caused multiple rounds of layout tuning.

Final direction in the current branch:

- Keep horizontal scrolling enabled for the ordinary columns.
- Keep `操作` as the right-fixed column.
- Move `统计` into the `operate` actions rather than keeping it as a standalone column.
- Keep `兑换记录` as an ordinary column.
- Move `注册时间` to the end of the ordinary columns.
- Remove the `订阅来源` column from the visible user table.

Key files:

- `web/src/components/table/users/UsersColumnDefs.jsx`
- `web/src/components/table/users/UsersTable.jsx`

Specific adjustments made:

- Rebalanced column widths to reduce overlap and clipping.
- Added or adjusted explicit widths for multiple columns.
- Restored horizontal scrolling behavior for wide tables.
- Put `统计` back into the fixed right-side action area as the first action button.
- Increased the `operate` column width so action buttons are not silently clipped.

Important implementation note:

- `showUserSubscriptionStatsModal` was already passed from `UsersTable`.
- The reason the stats button previously did not render was that `renderOperations()` did not destructure or render it.
- This was fixed by wiring the prop into `renderOperations()` and rendering the button explicitly.

### 8.6 Frontend text cleanup

The user-management files contained visible garbled Chinese strings/mojibake.

Primary cleanup file:

- `web/src/components/table/users/UsersColumnDefs.jsx`

Representative fixes:

- user role labels
- user status labels
- quota labels
- invite info labels
- operation labels
- `重置 Passkey`
- `重置 2FA`

There may still be additional garbled strings in some modal files under:

- `web/src/components/table/users/modals/`

Those should be checked separately if `/console/user` still shows mojibake in modal content.

### 8.7 Wallet/top-up page note

The wallet page subscription status display was restored from plain text back to styled tag/badge presentation.

Changed file:

- `web/src/components/topup/SubscriptionPlansCard.jsx`

Context:

- A historical change had replaced status tags with plain text such as `生效` / `已过期` / `已作废`.
- The styled badge/tag rendering was restored for clarity and UI consistency.

### 8.8 Build verification during this round

Verified successfully:

- Backend API-only build:
  - `go build -tags noweb -o new-api-api`
- Frontend build:
  - `bun run build`

Additional note:

- On Windows, `vite build` previously hit `EPERM unlink web/dist/favicon.lemon.ico`.
- Root cause was an external process locking the icon file rather than a code issue.
- `docs/review-notes.md`

This should restore the main review context quickly without repeating the full static reading pass.

## 8. Session Notes (2026-03-27)

This section summarizes the full conversation in this session and ties conclusions back to local git changes.

### 8.1 Conversation Goals Covered

1. Read local git changes and summarize them with `AGENTS.md` + `docs/review-notes.md` context.
2. Locate and fix the `/console/user` "group vs tier" confusion in user management.
3. Trace `/console/token` -> token list -> "Edit" -> "Token Group" data source.
4. Write a consolidated session summary back into `docs/review-notes.md`.

### 8.2 Git-Backed Change Summary (Relevant to This Session)

Core backend changes indicate a separation of **user group** and **user level** semantics:

- New user level context propagation:
  - `constant/context_key.go` adds `ContextKeyUserLevelID`.
  - `model/user_cache.go` caches/writes `UserLevelID` into request context.
- User model and API data:
  - `model/user.go` adds `user_level_id` (default `1`) and keeps `group` as an independent field.
  - `controller/user.go` returns `user_level_id` in `GetSelf`.
- Level policy framework:
  - `setting/user_level_policy.go` introduces level policy parsing/getters by level and by ID.
  - `model/option.go` and `controller/option.go` add `UserLevelPolicies` option storage and validation.
- Upgrade behavior on recharge/redeem:
  - `model/topup.go` auto-upgrade flow updates `user_level_id` only.
  - `model/redemption.go` creates a top-up record and reuses the same level-upgrade logic.
- Level-driven runtime controls:
  - `middleware/model-rate-limit.go` supports level-based rate limits.
  - `middleware/distributor.go` and `service/channel_select.go` enforce level-based channel allow-lists.
  - `relay/helper/price.go` applies level discount multiplier.
  - `middleware/user-level-group-day-limit.go` enforces level group day limits.
  - `model/log.go` adds daily consumed-money calculation for level/day-limit checks.
- Routing/API additions:
  - `controller/user_level.go` + `router/api-router.go` add `/api/user/self/user-level`.
  - `router/relay-router.go` and `router/video-router.go` wire `UserLevelGroupDayLimit` middleware.

Frontend-related additions in the same working tree:

- New user level page and menu entry:
  - `web/src/App.jsx` adds `/console/level`.
  - `web/src/components/layout/SiderBar.jsx` adds "等级/限制".
  - `web/src/pages/Setting/index.jsx` adds level settings tab.

### 8.3 Resolution: "Group vs Tier" Mix-Up

The session conclusion (based on code and prior test logs in this conversation) is:

- User **group** (e.g. `default/vip/svip`) should remain the channel-routing and usable-group concept.
- User **level** (e.g. `Tier 1/Tier 2/...`) should be stored and evaluated via `user_level_id` + level policy.
- Auto-upgrade logic now upgrades `user_level_id` rather than overwriting `user.group`.

Recorded test commands from this session context (not re-run in this specific turn):

- `go test ./model -run "UserRegister_DefaultUserLevelIDIsOne|AutoUpgradeByRecharge|GetUserLevelGroupDailyConsumedMoney" -count=1 -v`
- `go test ./setting -run UserLevel -count=1`
- `go test ./controller ./middleware ./service ./relay/helper -run ^$ -count=1`

### 8.4 `/console/token` "Token Group" Data Source Trace

Trace result:

- Menu route entry:
  - `web/src/App.jsx` (`/console/token`)
  - `web/src/components/layout/SiderBar.jsx` (`token: '/console/token'`)
- Edit button opens modal:
  - `web/src/components/table/tokens/TokensColumnDefs.jsx` (`setEditingToken(record)` + `setShowEdit(true)`)
  - `web/src/components/table/tokens/index.jsx` renders `EditTokenModal`.
- "Token Group" field in modal:
  - `web/src/components/table/tokens/modals/EditTokenModal.jsx` (`field='group'`).

Two distinct data sources:

1. **Group option list**:
   - Frontend calls `GET /api/user/self/groups` in `loadGroups()`.
   - Route: `router/api-router.go` -> `selfRoute.GET("/self/groups", controller.GetUserGroups)`.
   - Backend source logic: `controller/group.go` + `service/group.go` + `ratio_setting.GetGroupRatioCopy()`.
2. **Current token selected group value**:
   - Frontend calls `GET /api/token/{id}` in `loadToken()`.
   - Route: `router/api-router.go` -> `tokenRoute.GET("/:id", controller.GetToken)`.
   - Backend returns token data (`group` field from `model/token.go`).

Conclusion: token "令牌分组" comes from **group** APIs/fields, not from tier policy objects.

### 8.5 Notes / Risks to Keep Watching

- The working tree contains many other modified and untracked files; some are unrelated to this specific issue.
- `web/bun.lock` has a large diff and should be reviewed separately when preparing commits.
- There are garbled strings in some localized messages; this is pre-existing and also appears in new code paths (for example some middleware message constants).

## 9. Session Notes (2026-04-08)

This section records the changes and code traces covered in the 2026-04-08 collaboration. It avoids repeating the 2026-03-27 notes above.

### 9.1 `/console/subscription` + user subscription logic trace

Main frontend entry points reviewed:

- `web/src/components/topup/SubscriptionPlansCard.jsx`
- `web/src/components/topup/index.jsx`

Key API calls in user wallet flow:

- `GET /api/subscription/plans`
- `GET /api/subscription/self`
- `PUT /api/subscription/self/preference`
- `POST /api/subscription/epay/pay`
- `POST /api/subscription/stripe/pay`
- `POST /api/subscription/creem/pay`

Backend/data structure anchors:

- `model/subscription.go` (`SubscriptionPlan`, `UserSubscription`, order/payment-related structs)
- `controller/subscription.go`
- `controller/subscription_payment_epay.go`
- `controller/subscription_payment_stripe.go`
- `controller/subscription_payment_creem.go`

### 9.2 `/console/redemption` add/edit logic + subscription binding support

The redemption model and admin UI already support dual reward types (quota or subscription):

- `model/redemption.go`
  - `reward_type` (default quota)
  - `plan_id` (bound subscription plan when reward type is subscription)
- `web/src/constants/redemption.constants.js`
  - includes `SUBSCRIPTION` reward type constant
- `web/src/components/table/redemptions/modals/EditRedemptionModal.jsx`
  - add/edit form supports selecting reward type and subscription plan
  - validates quota path vs subscription path separately
- `web/src/components/table/redemptions/RedemptionsColumnDefs.jsx`
  - list display distinguishes quota vs subscription-bound redemption code

User redeem handling in wallet page:

- `web/src/components/topup/index.jsx`
  - redeem action checks `data.reward_type`
  - if subscription reward: shows plan info and refreshes `getSubscriptionSelf()`
  - if quota reward: updates quota display path

### 9.3 Wallet recharge -> payment gateway jump trace

For `/console/topup` quota recharge flow, frontend and backend path confirmed:

- Frontend submit path: `web/src/components/topup/index.jsx`
  - `onlineTopUp()` posts `/api/user/pay`
  - receives `{ url, data }` from backend
  - builds form and submits to returned `url`
- Backend payment request path: `controller/topup.go` -> `RequestEpay`
  - creates epay client with `operation_setting.PayAddress`
  - calls epay purchase API and returns `url` + params to frontend

Observed behavior: actual payment submission target is generated by the epay client based on configured payment address (commonly payment gateway `.../submit.php`).

### 9.4 Fix: topup "actual payment" amount used stale price ratio

Issue reproduced from report:

- Admin changed `Price` in payment settings (example `2.00`)
- `/console/topup` preset card still showed old computed amount (example `$10` -> `73` CNY)

Root cause:

- Topup preset display used frontend local `priceRatio`
- `priceRatio` could be sourced from stale `statusState.status.price`
- `/api/user/topup/info` did not return current `price`

Code changes applied:

1. `controller/topup.go`
   - `GetTopUpInfo` now returns `"price": operation_setting.Price`
2. `web/src/components/topup/index.jsx`
   - in `getTopupInfo()`, read `data.price` and set `priceRatio`
   - removed status effect overwrite: `setPriceRatio(statusState.status.price || 1)`

Validation run:

- `go test ./controller ./model ./service ./middleware ./relay/helper -run ^$ -count=1`
- `bun run build` (under `web/`)

Both completed successfully in this session.
### 9.5 Residual Fix (2026-04-09)

Follow-up issue reported after 9.4:

- In wallet recharge preset cards (`/console/topup` -> "选择充值额度"), values like:
  - `10 $`
  - `实付 $1.37`
  - `节省 $0.00`
  could still use stale exchange logic.

Root cause:

- `web/src/components/topup/RechargeCard.jsx` still read `localStorage.status.usd_exchange_rate` as `usdRate` for card amount conversion.
- This bypassed the already-fixed `priceRatio` source synced from `/api/user/topup/info.price`.

Code change:

- `web/src/components/topup/RechargeCard.jsx`
  - Removed local `usdRate` read from `localStorage.status`.
  - Introduced `topupRate` derived from `priceRatio` prop with safe fallback.
  - Updated USD/CNY/CUSTOM branches to use `topupRate` for:
    - `displayActualPay`
    - `displaySave`
    - `displayValue` (where applicable)

Result:

- Preset card "实付/节省" now stays aligned with latest recharge price setting (`Price`) instead of stale local status exchange value.

Validation run:

- `bun run build` (under `web/`) passed.

### 9.6 Session Update (2026-04-09): Mall payment flow extensions

This update extends both wallet top-up and subscription purchase flows with mall-style external link redirect behavior.

#### A) Wallet recharge (`/console/topup`) mall flow

Goal:

- Support a `mall` payment method in payment settings.
- When user selects recharge amount + `mall` payment, redirect to configured product link in a new tab.

Implementation notes:

- Payment method config supports `type: "mall"` in recharge methods JSON.
- Added payment setting `mall_links` (amount-to-link JSON), similar to discount mapping.
- Frontend topup flow checks selected amount and selected payment method:
  - if payment method is `mall` and matched link exists, open target via `target=_blank` (new tab)
  - otherwise keep existing epay/stripe/creem logic unchanged.

UI/icon update:

- Replaced mall icon from lucide `ShoppingCart` to custom image `/taobao_75px.png` in:
  - `web/src/components/topup/RechargeCard.jsx`
  - `web/src/components/topup/modals/PaymentConfirmModal.jsx`

#### B) Subscription management (`/console/subscription`) `mall_link` field

Goal:

- Add `mall_link` to subscription plan create/edit.
- If `mall_link` is set for a plan, clicking `立即订阅` should directly open this link in a new tab and skip all other payment methods.

Backend changes:

- `model/subscription.go`
  - `SubscriptionPlan` adds `MallLink string` (`json:"mall_link"`).
- `controller/subscription.go`
  - Admin plan update map includes `mall_link`.
- `model/main.go`
  - SQLite subscription plan table creation/ensure-column logic adds `mall_link` (`varchar(2048)`), keeping SQLite/MySQL/PostgreSQL compatibility.

Frontend changes:

- `web/src/components/table/subscriptions/modals/AddEditSubscriptionModal.jsx`
  - Added `mall_link` input in create/edit form.
  - Included `mall_link` in init values and submit payload.
- `web/src/components/topup/SubscriptionPlansCard.jsx`
  - `立即订阅` click path now:
    - if `plan.mall_link` exists and is valid `http/https`, `window.open(mallLink, '_blank')`
    - if invalid URL, show error
    - if empty, fallback to existing purchase modal/payment flow.
- `web/src/components/table/subscriptions/SubscriptionsColumnDefs.jsx`
  - Payment channel column shows `Mall` tag when `plan.mall_link` is configured.

Validation run for this update:

- `bun run build` (under `web/`) passed.
- `go build ./...` (project root) passed.

## 10. Session Notes (2026-04-11)

This section summarizes the global model ratio feature implementation, related billing-chain verification, and frontend follow-up fixes completed in this session.

### 10.1 Requirement covered

Added a new `GlobalModelRatio` setting under:

- `系统设置 -> 分组与模型定价设置 -> 模型倍率设置`

Target behavior implemented:

- Default is `1`.
- It participates in real billing calculations (both pre-consume and settlement).
- It is designed to affect charging even when some free-model pre-consume bypass conditions exist.
- It is not explicitly displayed as a separate item in usage-log billing detail.

### 10.2 Backend changes

Core setting storage and runtime config:

- `setting/ratio_setting/global_model_ratio.go`
  - Added atomic global ratio storage.
  - Default `1.0`.
  - Getter/setter with invalid/negative value guards.
- `model/option.go`
  - Added `GlobalModelRatio` to `OptionMap` initialization and update switch.
- `controller/option.go`
  - Added validation for `GlobalModelRatio` (`number` and `>= 0`).

Billing data model propagation:

- `types/price_data.go`
  - Added `GlobalModelRatio` to `PriceData`.

Pre-consume chain integration:

- `relay/helper/price.go`
  - `ModelPriceHelper` and `ModelPriceHelperPerCall` now multiply pre-consume quota by `globalModelRatio`.
  - Free-model pre-consume bypass logic updated to include global ratio condition.

Settlement chain integration:

- `relay/compatible_handler.go`
  - Global ratio applied in token-based and fixed-price settlement branches.
  - Global ratio also applied for tool/call-like charges:
    - web search
    - file search
    - claude web search
    - image generation call
    - separate audio input pricing path
- `service/quota.go`
  - Global ratio used in audio and other quota settlement paths via `PriceData`.
- `service/task_billing.go`
  - Async task quota recalculation includes global ratio.
- `controller/channel-test.go`
  - Channel billing estimate path includes global ratio.

### 10.3 Frontend changes and fixes

Main UI addition:

- `web/src/pages/Setting/Ratio/ModelRatioSettings.jsx`
  - Added new input field `GlobalModelRatio`.
  - Added user-facing hint text for behavior.

Follow-up bugfixes in this session:

1. Fixed false "你似乎并没有修改什么" detection for new field when backend options did not yet include `GlobalModelRatio`.
2. Fixed regression where unrelated ratio JSON fields could be submitted as empty values, causing backend JSON parse errors.
3. Finalized default-display behavior so first page load shows `GlobalModelRatio = 1` in the input (not empty).
4. Normalized frontend value handling:
   - kept `GlobalModelRatio` as numeric value for `InputNumber`.
   - converted number/bool to string at submit time for `/api/option/` compatibility.

### 10.4 Billing pre-check verification result

Code-path verification confirms global ratio participates before upstream API call:

- `controller/relay.go`
  - `ModelPriceHelper(...)` computes pre-consume quota.
  - `PreConsumeBilling(...)` is executed before relay `DoRequest(...)`.
- `relay/helper/price.go`
  - pre-consume formula includes `globalModelRatio`.

Conclusion:

- `GlobalModelRatio` is active in pre-consume pre-check and final settlement.

### 10.5 Validation runs

Validation commands executed during this session:

- `go test ./... -run TestNonExistent -count=1` (compile-level backend sanity)
- `bun run build` (frontend build sanity; re-run after each frontend fix)

## 11. Session Notes (2026-04-18 to 2026-04-22)

This section summarizes the feature work and deployment-related adjustments completed after the global model ratio rollout.

### 11.1 User-level global model ratio

Goal:

- Add a per-user `global_model_ratio` that behaves like system `GlobalModelRatio`.
- Make it participate in all billing and pre-consume paths.
- Keep it hidden from user-facing APIs and usage-log `other` payloads.

Backend changes:

- Added persistent user field:
  - `model/user.go`
- Added Redis/user cache support for the new ratio:
  - `model/user_cache.go`
  - `common/redis.go`
- Merged system global ratio and user ratio inside the pricing helper:
  - `relay/helper/price.go`
- Covered async task recalculation and channel billing preview:
  - `service/task_billing.go`
  - `controller/channel-test.go`

Behavior notes:

- Effective billing multiplier is now:
  - `system global model ratio * users.global_model_ratio`
- `0` is allowed and effectively makes the user free under current pricing formulas.
- The field is intentionally not written into usage-log `other` JSON.
- User-side `GetSelf` style payloads do not expose this field.

Admin UI / API changes:

- Added edit support in the admin user modal:
  - `web/src/components/table/users/modals/EditUserModal.jsx`
- `controller/user.go` appends `global_model_ratio` only for admin single-user edit responses.

### 11.2 Admin user list support for user global model ratio

Goal:

- Make the new per-user billing ratio operationally visible from `/console/user`.

Implemented changes:

- Added backend filter param:
  - `global_model_ratio_filter`
- Filter logic implemented in:
  - `model/user.go`
  - `controller/user.go`
- Added frontend filter dropdown in:
  - `web/src/components/table/users/UsersFilters.jsx`
  - `web/src/hooks/users/useUsersData.jsx`

Current filter modes:

- `default`
  - `global_model_ratio = 1`
- `custom`
  - `global_model_ratio != 1`
- `free`
  - `global_model_ratio = 0`

List-display follow-up:

- Admin user list responses now append `global_model_ratio` via a safe map-based response conversion in:
  - `controller/user.go`
- Added visible `用户倍率` column in:
  - `web/src/components/table/users/UsersColumnDefs.jsx`

Display behavior:

- Default users show neutral tag styling.
- Custom users use highlighted tag styling.
- Free users show a dedicated `免费` tag.
- Tooltip explicitly notes that the ratio only affects the current user and multiplies with system global ratio.

### 11.3 Subscription usage rank page

Goal:

- Add an admin leaderboard for subscription users based on rolling-window usage.
- Support `1d / 3d / 7d` windows.
- Show:
  - usage amount
  - today used / today total snapshot ratio
  - request count
  - active-period ARPM

Backend changes:

- Added rank query model:
  - `model/subscription_usage_rank.go`
- Added admin API:
  - `controller/user_subscription_usage_rank.go`
- Added route registration:
  - `router/api-router.go`

Metric design implemented:

- Window type:
  - rolling `1d / 3d / 7d`
- Population:
  - users with active subscriptions
- Usage source:
  - aggregated consume logs
- Today subscription snapshot:
  - current active subscription pool snapshot
- ARPM:
  - `request_count / active_minutes`
  - active minutes are derived from first/last request time with minimum guard

Frontend changes:

- Added dedicated page and data hook:
  - `web/src/pages/SubscriptionUsageRank/`
  - `web/src/hooks/subscription-usage-rank/`
- Added leaderboard table components:
  - `web/src/components/table/subscription-usage-rank/`
- Added sidebar/menu exposure:
  - `web/src/App.jsx`
  - `web/src/components/layout/SiderBar.jsx`
  - `web/src/hooks/common/useSidebar.js`
  - `web/src/pages/Setting/Operation/SettingsSidebarModulesAdmin.jsx`
  - `web/src/components/settings/personal/cards/NotificationSettings.jsx`
  - `web/src/helpers/render.jsx`

### 11.4 Subscription admin tooling consolidation

This round also continued to fill in the admin-facing subscription/user lookup toolset.

Covered capabilities now include:

- daily subscription usage stats modal
- redemption record reverse lookup and detail modal
- subscription source tracing modal
- subscription usage rank page

Representative files:

- `controller/user_subscription_daily_stat.go`
- `controller/user_redemption.go`
- `controller/user_subscription_source.go`
- `controller/user_subscription_usage_rank.go`
- `model/subscription_daily_stat.go`
- `model/user_redemption.go`
- `model/user_subscription_source.go`
- `model/subscription_usage_rank.go`
- `web/src/components/table/users/modals/UserSubscriptionStatsModal.jsx`
- `web/src/components/table/users/modals/UserRedemptionRecordsModal.jsx`
- `web/src/components/table/users/modals/UserSubscriptionSourcesModal.jsx`

Operational note:

- `/console/user` is now the main entry point for inspecting user subscription state from multiple angles, while the dedicated subscription rank page provides cross-user ranking and ops visibility.

### 11.5 Codex setup script updates

Goal:

- Improve the Windows onboarding path for Codex users and align defaults with the deployed API endpoint.

Changed files:

- `guide/setup-codex.ps1`
- `guide/setup-codex-win.ps1`
- `guide/mac.jpg`

Notable changes:

- Default base URL was updated toward `https://api.jucodex.com` / `https://api.jucodex.com/v1`.
- Added a Windows-focused script variant that avoids common PowerShell `ExecutionPolicy` issues.
- The new script prefers `npm.cmd` / `codex.cmd` or `.exe` resolution to avoid `*.ps1` execution-policy failures.
- The script now explicitly writes Codex configuration files under `~/.codex/`.

### 11.6 Cloudflare Worker and static-site routing adjustments

Goal:

- Split static asset delivery and backend proxying more clearly for the `jucodex` deployment.

Changed files:

- `web/jucodex-worker/src/index.js`
- `web/jucodex-worker/wrangler.toml`

Behavior changes:

- Requests routed to the Worker now behave as:
  - `/api`, `/v1`, `/v1beta`, `/pg`, `/mj`, `/suno`, and realtime paths -> backend proxy
  - all other paths -> static origin proxy
- Static content is no longer fetched from R2 bucket objects directly in code.
- Static requests now proxy to `STATIC_ORIGIN`, preserving path and query string.
- SPA fallback returns `/index.html` for extensionless non-API routes.
- Dynamic backend responses are forced to `no-store, no-cache, must-revalidate`.

Configuration changes:

- Added `STATIC_ORIGIN = "https://static.jucodex.com"`
- Backend host changed to:
  - `alb.jucodex.com`

Important deployment note:

- The Worker code itself does not decide which domains/subdomains are intercepted.
- Actual domain coverage still depends on Cloudflare route / custom-domain bindings configured in Cloudflare.

### 11.7 Miscellaneous cleanup

Changed files:

- `.gitignore`
- `web/public/statics/doc.html`

Summary:

- Added `R2_Cli.md` to ignore rules.
- Removed the floating QQ consultation widget from the static documentation HTML page.

## 12. Session Notes (2026-04-22 to 2026-04-23)

This section summarizes the payment-routing refactor, wallet/subscription console changes, and frontend text/encoding cleanup completed in the most recent round.

### 12.1 Unified payment routing foundation

Goal:

- Stop relying on scattered frontend/backend implicit field checks to decide whether recharge/subscription should route to Epay, Stripe, Creem, or mall.
- Introduce explicit scene-level routing with a structure that can be extended later for official WeChat Pay / Alipay style providers.

Backend structure added:

- `types/payment.go`
- `setting/operation_setting/payment_route.go`
- `service/payment_registry.go`
- `service/payment_router.go`
- `service/payment_checkout_helpers.go`
- `service/payment_provider_epay.go`
- `service/payment_provider_stripe.go`
- `service/payment_provider_creem.go`
- `service/payment_provider_mall.go`

Controller and router additions:

- `controller/payment_checkout.go`
- `controller/payment_checkout_test.go`
- `controller/payment_compat.go`
- `controller/payment_legacy_handlers.go`
- `router/api-router.go`

Behavior changes:

- Added explicit route config:
  - `payment_route.topup_provider`
  - `payment_route.subscription_provider`
- Supported route values currently include:
  - `legacy_auto`
  - `disabled`
  - `epay`
  - `stripe`
  - `creem`
  - `mall`
- Added unified APIs:
  - `GET /api/payment/topup/meta`
  - `POST /api/payment/topup/checkout`
  - `GET /api/payment/subscription/meta`
  - `POST /api/payment/subscription/checkout`
- Existing old payment APIs were kept as compatibility handlers, but their internal dispatch path was redirected to the new router/provider service instead of maintaining separate branching logic.

Operational note:

- `legacy_auto` remains important for compatibility. It preserves the previous field-driven behavior while the frontend/admin console gradually moves to explicit routing.

### 12.2 Payment settings and user payment flow UI consolidation

Goal:

- Surface the new explicit payment-routing controls in admin settings.
- Make `/console/topup` and subscription purchase flow consume unified payment metadata/checkout instead of branching locally.

Representative frontend files:

- `web/src/components/settings/PaymentSetting.jsx`
- `web/src/pages/Setting/Payment/SettingsPaymentRouting.jsx`
- `web/src/helpers/payment.js`
- `web/src/components/topup/index.jsx`
- `web/src/components/topup/RechargeCard.jsx`
- `web/src/components/topup/SubscriptionPlansCard.jsx`
- `web/src/components/topup/modals/SubscriptionPurchaseModal.jsx`
- `web/src/components/topup/modals/PaymentConfirmModal.jsx`

Admin-side changes:

- Added an explicit payment-routing settings card to `/console/setting`.
- The payment settings page now distinguishes between:
  - route selection
  - provider parameters
- `mall` display naming was unified to `商城` and the related strings were wired through frontend i18n.

User-side changes:

- Top-up and subscription purchase flow now request unified `meta`/`checkout` APIs instead of deciding provider routing purely in the page.
- Mall subscription routing was also pulled back into the unified subscription checkout path instead of direct frontend `window.open` shortcut logic.
- Old top-up page branches for `/api/user/pay`, `/api/user/stripe/pay`, and `/api/user/creem/pay` were removed from the main UI path.

### 12.3 Wallet page layout and subscription card UX changes

Goal:

- Reduce visual clutter on `/console/topup`.
- Improve payment visibility and subscription-card readability.

Recharge/top-up page adjustments:

- Removed the top-up quantity / actual payment input block when only fixed preset amounts are intended to be sold.
- Default top-up selection now follows the first configured amount option instead of falling back to `1`.
- Payment methods were moved below the amount option grid for stronger visual emphasis.
- If online recharge is disabled, the amount option list still remains visible and the payment section shows an admin-disabled notice instead of collapsing the whole block.
- The redemption-code card was duplicated under the subscription tab as an additional entry point without changing redemption logic.

Representative files:

- `web/src/components/topup/RechargeCard.jsx`
- `web/src/components/topup/index.jsx`

Subscription card adjustments:

- Removed the implicit “first card = recommended” UI rule.
- Monthly plans were given a gold-accent visual treatment.
- Weekly plans were given a silver-accent visual treatment.
- Added support for `week` duration unit on the admin subscription form and formatting helpers.
- For reset-based plans, benefit lines were reformatted from:
  - reset period + total amount
  - into per-cycle quota + duration max quota style, for example:
    - `日限额`
    - `月度拉满`
    - `周度拉满`

Representative files:

- `web/src/components/topup/SubscriptionPlansCard.jsx`
- `web/src/components/table/subscriptions/modals/AddEditSubscriptionModal.jsx`
- `web/src/components/table/subscriptions/SubscriptionsColumnDefs.jsx`
- `web/src/helpers/subscriptionFormat.js`
- `model/subscription.go`

Additional UX tweak:

- “我的订阅” usage display was changed from plain text into a compact gray-track progress bar with red used section / green remaining section plus hover detail text.

### 12.4 Frontend encoding and i18n cleanup

Goal:

- Reduce recurring mojibake in top-up/subscription pages.
- Normalize frontend source encoding and clean broken Chinese i18n source keys.

Infrastructure changes:

- Added `.editorconfig` with UTF-8 / LF conventions.
- Scanned `web/src` and removed BOM from affected source files.

Top-up/subscription-related cleanup:

- Fixed multiple garbled strings and broken JSX/text fragments in:
  - `web/src/components/topup/index.jsx`
  - `web/src/components/topup/RechargeCard.jsx`
  - `web/src/components/topup/SubscriptionPlansCard.jsx`
  - `web/src/components/topup/modals/PaymentConfirmModal.jsx`
  - `web/src/components/topup/modals/SubscriptionPurchaseModal.jsx`
  - `web/src/components/topup/modals/TopupHistoryModal.jsx`
  - `web/src/components/topup/modals/TransferModal.jsx`
  - `web/src/helpers/subscriptionFormat.js`
- Redeem success/failure texts were moved onto frontend i18n instead of hardcoded messages.
- A directory-wide follow-up pass was performed on `web/src/components/topup` to replace remaining garbled `t('...')` keys in the payment/subscription components with normal Chinese source keys.

Important residual note:

- Frontend i18n in this project uses Chinese source strings as translation keys. This makes the UI especially sensitive to any encoding corruption in source files: once a Chinese key itself becomes garbled, the page will render the garbled text directly. Future edits in text-heavy frontend files should avoid shell-based write paths that may re-encode content incorrectly.

### 12.5 SQLite migration compatibility fix

Goal:

- Resolve local startup failure caused by malformed historical SQLite DDL when running GORM migration.

Changed file:

- `model/main.go`

Behavior change:

- SQLite initialization was hardened with a `users` table compatibility path similar in spirit to the existing `subscription_plans` compatibility handling.
- Instead of directly trusting GORM to parse a malformed historical SQLite table definition, the code now checks schema shape and backfills missing columns through safer SQLite-compatible logic.

Observed symptom before fix:

- Startup could fail with:
  - `failed to initialize database: invalid DDL, unbalanced brackets`

### 12.6 Validation runs in this round

Representative verification commands used during this session:

- `go build ./...`
- `go test ./controller -run Payment`
- `bun run build`

Known warnings still seen during frontend build:

- Vite/Rollup chunk-size warnings
- circular chunk warning around `semi-ui` and `i18n`

These are build warnings, not blockers for the payment/top-up changes described above.

## 13. Session Notes (2026-04-24)

This section summarizes the latest UI-polish milestone for the wallet/subscription console after the unified payment routing foundation had already landed.

### 13.1 Payment display currency decoupled from quota display currency

Goal:

- Keep quota/balance display logic unchanged.
- Allow payment-related amounts to use a separate display currency and rate.

Behavior changes:

- Added a dedicated payment display currency path instead of forcing payment amounts to follow quota display currency.
- Supported display modes now include:
  - `FOLLOW_QUOTA`
  - `USD`
  - `CNY`
  - `CUSTOM`
- This change only affects payment-facing amounts such as:
  - subscription purchase modal payable amount
  - top-up preset actual payment amount
  - payment confirmation modal actual/original/discount amount
- It does not change:
  - quota balance display
  - subscription quota display
  - third-party provider settlement currency

Representative files:

- `setting/operation_setting/payment_setting.go`
- `controller/misc.go`
- `web/src/helpers/data.js`
- `web/src/helpers/render.jsx`
- `web/src/pages/Setting/Payment/SettingsGeneralPayment.jsx`
- `web/src/components/settings/PaymentSetting.jsx`
- `web/src/components/topup/RechargeCard.jsx`
- `web/src/components/topup/modals/SubscriptionPurchaseModal.jsx`
- `web/src/components/topup/modals/PaymentConfirmModal.jsx`

Important debugging note:

- A save/reload issue was traced to frontend form state overwrite rather than database persistence failure.
- Temporary console logging was added to isolate the issue, and the settings form was simplified so the saved value is not overwritten by internal form state after successful PUT requests.

### 13.2 Top-up amount cards redesigned into preset-first purchase UI

Goal:

- Reduce noise in the recharge tab and make the purchase decision more straightforward.

UI changes:

- Preset amount cards no longer show a dense multi-line summary inside each card.
- Cards now present a larger, cleaner payment amount only.
- The detail summary for the selected preset was moved into a separate summary block below the grid.
- The summary block now shows:
  - payment amount
  - received balance
  - current exchange ratio
  - discount
- If no discount is applied, the UI now shows `none` instead of an artificial `10.0` discount label.
- The section title was changed from "select recharge quota" to "select recharge amount".

Design refinements:

- Important numbers in the summary block were visually emphasized.
- The historical discount tag styling was reused instead of showing discount as plain text.
- The selected-preset detail block was adapted for dark mode.

Representative files:

- `web/src/components/topup/RechargeCard.jsx`
- `web/src/helpers/render.jsx`

### 13.3 Subscription purchase modal payment method UX refinement

Goal:

- Reduce friction in the subscription purchase modal.
- Make its payment interaction consistent with the recharge tab.

UI behavior changes:

- The payment method dropdown in the subscription purchase modal was replaced with flat payment method cards/buttons.
- Payment methods now render in a clearer order aligned with the recharge-side mental model:
  - Epay methods first
  - mall after Epay
  - Stripe / Creem afterward when available
- Mall visibility under `legacy_auto` was corrected so plans with a valid mall link surface a mall option in the modal.
- The payment method cards gained explicit black borders and were adapted for dark mode.
- A close-animation flicker caused by clearing modal content too early was fixed by deferring cleanup until after modal close.

Representative files:

- `web/src/components/topup/modals/SubscriptionPurchaseModal.jsx`
- `web/src/components/topup/SubscriptionPlansCard.jsx`

### 13.4 Subscription plan card polish and dark-mode follow-up

Goal:

- Continue polishing subscription cards without changing purchase logic.

Changes retained after iteration:

- Monthly plans keep the gold-accent treatment.
- Weekly plans keep the silver-accent treatment.
- Both month/week variants were adapted for dark mode so they no longer depend on light-only backgrounds.
- Subscription price on the plan card now follows the payment display currency helper, so plan price display matches the configured payment display currency.

Iteration note:

- One experimental attempt to restyle the benefit rows into a more aggressive two-column premium layout was reverted after visual review because it reduced overall quality instead of improving it.
- Current plan benefit rows intentionally remain closer to the previous, simpler presentation.

Representative files:

- `web/src/components/topup/SubscriptionPlansCard.jsx`

### 13.5 Encoding discipline reinforced for frontend-heavy edits

Goal:

- Prevent new mojibake regressions while repeatedly editing text-heavy wallet/subscription UI files on Windows.

Working rule used in this round:

- Before frontend edits: inspect for BOM/history/Chinese-key risk.
- After frontend edits: scan the touched files for suspicious mojibake fragments.
- Re-run frontend production build after each substantial UI pass.

Files frequently rechecked in this round:

- `web/src/components/topup/RechargeCard.jsx`
- `web/src/components/topup/SubscriptionPlansCard.jsx`
- `web/src/components/topup/modals/SubscriptionPurchaseModal.jsx`

Validation runs:

- `bun run build`
- `go build ./...`
