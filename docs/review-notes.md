# Project Review Notes

Last updated: 2026-05-14

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
- Routing/API additions:
  - `controller/user_level.go` + `router/api-router.go` add `/api/user/self/user-level`.

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

- `go test ./model -run "UserRegister_DefaultUserLevelIDIsOne|AutoUpgradeByRecharge" -count=1 -v`
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

## 14. 2026-05-05 Channel Forwarding

Goal:

- Add a per-channel forwarding rule after the normal channel routing decision.
- If the request system prompt or the current new message matches configured regex rules on the selected channel, forward the request to another target channel.
- Keep actual billing logic unchanged.

Behavior summary:

- Forwarding happens after the original channel is selected, before upstream relay execution.
- Only one hop is allowed.
- Once forwarding is applied, retry stays locked to the forwarded target channel.
- If the target channel is unavailable, the request returns the target channel's own error directly.
- OpenAI `developer` messages are intentionally ignored.
- Historical chat context is not inspected.

Text matching scope:

- OpenAI `/v1/chat/completions`
  - inspect all `system` messages
  - inspect only the last `user` message
- OpenAI `/v1/completions`
  - inspect `prompt`
- OpenAI `/v1/moderations`
  - inspect `input`
- OpenAI `/v1/edits`
  - inspect `instruction` and `input`
- OpenAI `/v1/responses` and `/v1/responses/compact`
  - inspect `instructions`
  - inspect only the latest current-turn user input
- Claude `/v1/messages`
  - inspect `system`
  - inspect only the last `user` message
- Gemini text requests
  - inspect `systemInstruction`
  - inspect only the last user-side content block
- Multimodal requests are included, but only their text parts participate in matching.

Backend changes:

- Added forwarding config fields and validation to channel settings:
  - `dto/channel_settings.go`
- Reused channel setting validation from model layer:
  - `model/channel.go`
- Added self-target validation when editing an existing channel:
  - `controller/channel.go`
- Added unified forwarding extraction and regex matching service:
  - `service/channel_forward.go`
- Added focused tests for latest-message-only behavior:
  - `service/channel_forward_test.go`
- Hooked forwarding into the relay channel selection path and retry locking:
  - `controller/relay.go`

Frontend changes:

- Added channel forwarding config fields to the channel edit modal:
  - enable switch
  - target channel id
  - regex textarea
- Added frontend validation before save:
  - target channel id required when enabled
  - regex required when enabled
  - target channel cannot be self on edit
  - regex syntax is pre-validated in the browser

Representative file:

- `web/src/components/table/channels/modals/EditChannelModal.jsx`

Validation runs:

- `go test ./service -run TestShouldForwardChannelRequest -count=1`
- `go test ./controller -count=1`
- `bun run build`

Note:

- During validation, broader `go test ./service/...` still had unrelated pre-existing failures in existing affinity/billing tests. The new forwarding-focused tests passed.


---

# 2026-05-08 Xiaomi Claude、渠道转发与计费排查记录

本文记录本轮聊天中围绕 Xiaomi 渠道、Claude Code 工具调用、渠道转发预检、错误拦截、代理抓包、预检日志可见性、全局模型倍率计费等问题完成的排查结论与代码改动。

## 1. Xiaomi 渠道改为 OpenAI + Anthropic 双栈

目标：

- 保留 Xiaomi 现有 OpenAI Compatible 能力，不做硬替换。
- 当请求是 Claude Messages 格式时，走 Xiaomi 官方 Anthropic 兼容 API。
- 尽最大可能兼容 Xiaomi 官方文档，而不假设其完整等价 Anthropic 原生 API。

主要改动：

- `relay/channel/xiaomi/adaptor.go`
  - Claude Messages 请求改走 `/anthropic/v1/messages`。
  - Claude 请求 URL 支持追加 `?beta=true`。
  - Claude 请求使用 Xiaomi 要求的 `api-key` 鉴权头。
  - 补齐 Claude Code 常见非鉴权 headers，例如 `anthropic-version`、`anthropic-beta`、`anthropic-dangerous-direct-browser-access`、`x-app`、`x-stainless-*` 等。
  - OpenAI Compatible、Responses、TTS 等原有能力继续走原路径。
- `relay/channel/xiaomi/adaptor_test.go`
  - 增加 Xiaomi Claude URL、beta query、header、tool type 归一化相关测试。
- `relay/channel/xiaomi/claude_tools.go`
  - 对 Xiaomi 文档支持的 custom function tool 做兼容处理。
  - 普通自定义工具缺少 `type` 时补 `custom`。
  - 已显式声明 `type` 的工具保持原样，避免误改 Claude hosted/server tools。

结论：

- Xiaomi 官方 Anthropic 兼容文档明确支持的是 `type: custom` 的函数工具。
- Claude Code 主会话中的 `WebSearch`、`WebFetch` 等客户端工具可以作为 custom tools 被 Xiaomi 触发。
- Anthropic hosted/server tool，例如 `{"type":"web_search_20250305","name":"web_search"}`，目前没有证据表明 Xiaomi 官方接口支持。

## 2. Claude Code 工具调用失败排查结论

排查目标：

- 判断工具调用失败到底是网关没有正确映射请求，还是 Xiaomi 上游没有返回 `tool_use`。

已确认事实：

- 网关最终发给 Xiaomi 的 helper 请求中，`web_search_20250305` 没有被删除、改名或改写成 `custom`。
- 最终出站 URL 是 Xiaomi Anthropic 路径。
- `api-key` 存在，`Authorization` 和 `x-api-key` 不参与 Xiaomi Claude 鉴权。
- 原始 SSE chunk 抓到后显示：
  - 主会话 custom `WebSearch` 能返回 `tool_use`。
  - helper 请求中的 `web_search_20250305` 没有返回 `tool_use`。
  - Xiaomi 直接返回普通文本，语义类似“当前没有 web search tool”。

关键结论：

- 失败点不是本项目把 `web_search_20250305` 映射错了。
- 失败点也不是响应里有 `tool_use` 但被网关吃掉。
- 真正原因是 Xiaomi 当前 Anthropic 兼容接口没有执行 Claude hosted/server web search tool，至少在当前文档和实测请求范围内不支持。

相关日志增强：

- `relay/claude_handler.go`
  - 在 Claude 主链路早期为 Xiaomi Claude 打开 trace 标记。
  - 避免 trace 只放在 adaptor 内部导致真实响应链路抓不到 chunk。
- `relay/channel/claude/relay-claude.go`
  - 对 Xiaomi Claude 原始非流式响应体和流式 SSE chunk 增加诊断日志。
- `relay/helper/stream_scanner.go`
  - 增加 stream chunk 级别辅助日志，用于判断上游是否返回 `tool_use`、`message_delta`、`stop_reason` 等。
- `relay/channel/api_request.go`
  - 增加最终上游 HTTP 请求 URL、host、关键 headers 的 trace。

## 3. Claude Code 直连 Xiaomi 与本项目代理调用差异对比

通过代理抓包对比后处理的差异：

- 本项目 Xiaomi Claude URL 改为带 `?beta=true`，对齐 Claude Code 直连场景。
- 本项目补齐 Claude Code 常带的非鉴权 headers。
- 鉴权仍以渠道密钥生成 Xiaomi 要求的 `api-key`，不透传用户侧 `Authorization` 或 `x-api-key` 到 Xiaomi。

辅助工具：

- 新增 `proxy.py`
  - 本地 HTTP/HTTPS 抓包代理。
  - 自动生成本地 CA 和站点证书。
  - 记录请求头、请求体、响应头、响应体。
  - 用于 VS Code / Claude Code 设置代理后还原直连 Xiaomi 的真实请求和响应。

## 4. 渠道转发功能重构

原逻辑：

- 按用户新消息是否匹配渠道配置的正则表达式决定是否转发。
- 所有转发只能去一个固定目标渠道。

新逻辑：

- 去掉正则匹配。
- 新消息到达后，使用本渠道配置的预检模型和系统提示词先发起一轮 JSON 预检。
- 根据 JSON 指标分值和策略判断是否转发。
- 支持按原始模型映射到不同目标渠道。

配置能力：

- `forward_precheck_model`
  - 预检使用的大模型。
- `forward_precheck_prompt`
  - 预检系统提示词。
- `forward_max_message_chars`
  - 新消息长度大于该值时直接不转发。
- `forward_metric_rules`
  - 每个 JSON 指标的比较条件。
- `forward_metric_logic`
  - 指标之间支持 `AND` / `OR`。
- `forward_model_targets`
  - 按原始模型决定目标渠道，例如 `gpt-5.4>=3`。

重要语义：

- 转发匹配模型使用映射前的原始模型，不使用上游映射后的模型。
- 如果有模型映射，最终转发也是按原始模型匹配目标渠道。
- 预检请求只用于路由决策，不污染主请求正文。
- Claude Code 的 `<system-reminder>...</system-reminder>` 块不作为用户新消息参与预检，避免污染用户真实输入。
- 主请求转发后仍发送完整原始请求，只有预检决策使用提取后的新消息。

主要改动文件：

- `dto/channel_settings.go`
- `model/channel.go`
- `controller/channel.go`
- `controller/relay.go`
- `service/channel_forward.go`
- `service/channel_forward_test.go`
- `web/src/components/table/channels/modals/EditChannelModal.jsx`

已修问题：

- `forward_min_message_chars` 语义改为 `forward_max_message_chars`。
- 前端模型目标渠道配置支持 `gpt-5.4>=3`、`gpt-5.4 => 3`、`gpt-5.4 3`。
- 预检选中渠道时，如果 `ChannelMeta` 不完整，会重新读取完整 channel，避免 key 为空导致 401。
- 预检请求 URL 由相对路径修正为完整上游 URL。

## 5. 渠道转发预检请求隔离与错误处理

预检请求约束：

- 不再触发渠道转发。
- 不进入普通请求重试链路。
- 不计入用户主请求费用。
- 记录系统侧消耗日志，避免隐形成本不可见。
- 预检日志使用常态 info 日志。
- 预检失败时返回 HTTP 511。
- 预检失败错误信息固定为：`预请求失败，请重试。`

预检计费日志可见性：

- 预检消耗日志标记 `channel_forward_precheck=true`。
- 用户侧日志查询过滤该类记录。
- token 维度日志查询过滤该类记录。
- 用户使用量求和过滤该类记录。
- 管理侧全量日志保留系统审计入口。

主要改动文件：

- `service/channel_forward.go`
- `model/log.go`
- `controller/relay.go`

## 6. 渠道报错拦截

目标：

- 对已选中渠道发起后的上游错误做统一拦截。
- 用户可见错误可替换为渠道配置的模板。
- 日志仍保留真实上游错误，便于排查。

支持模板变量：

- `{request_id}`
- `{response_code}`
- `{error_code}`

主要改动：

- `dto/channel_settings.go`
  - 增加错误拦截配置。
- `service/error.go`
  - 增加错误拦截模板处理。
- `types/error.go`
  - 保留上游真实 HTTP 状态码。
  - `SetMessage` 同步更新嵌套 OpenAI / Claude 错误体。
- `service/error_test.go`
  - 增加错误模板替换和状态码保持相关测试。

约束：

- 拦截的是上游调用返回的错误。
- 本地请求 JSON 无效、敏感词、余额不足、获取渠道失败、预检配置错误等仍保留各自原有处理语义。
- 预检失败按专用 511 和固定文案处理。

## 7. Claude Code 新消息提取修正

问题：

- Claude Code 会在用户消息附近插入 `<system-reminder>...</system-reminder>`。
- 如果直接拿最后一个 user 内容参与预检，会把客户端提醒当成用户真实输入。

处理：

- Claude Messages 预检只提取最后一个真实用户文本块。
- 遇到完整 `<system-reminder>...</system-reminder>` 块时跳过。
- 如果最后一段是提醒块，则向前寻找同一轮或前一轮中的真实用户文本。

主要改动：

- `service/channel_forward.go`
- `service/channel_forward_test.go`

验证点：

- 预检只使用用户说的“你好”等真实新消息。
- 实际转发主请求仍保留完整原请求，不做内容裁剪。

## 8. Xiaomi Claude 请求与响应诊断日志

新增或增强的日志范围：

- Claude Messages 原始入口 headers。
- Xiaomi Claude 最终出站 URL、headers、body 摘要。
- Xiaomi Claude 原始 upstream stream chunk。
- Xiaomi Claude parsed chunk summary。
- tool 列表摘要，包括 `web_search_20250305` 形态。
- 上游非 200 响应体。

目的：

- 直接判断问题发生在请求映射、上游模型选择、上游响应还是网关响应转换。
- 避免靠猜判断 Xiaomi 是否支持某类工具。

主要改动文件：

- `relay/claude_handler.go`
- `relay/channel/api_request.go`
- `relay/channel/xiaomi/adaptor.go`
- `relay/channel/claude/relay-claude.go`
- `relay/helper/stream_scanner.go`

## 9. 全局模型倍率和用户倍率最终结算修复

发现问题：

- test 用户 `#2` 的 `users.global_model_ratio=2` 已正确写入数据库。
- 最后两笔真实扣费走订阅额度：
  - `log id=199`，扣费 `36183`
  - `log id=200`，扣费 `52733`
- 两笔扣费合计 `88916`，等于订阅 `amount_used=88916`。
- 日志中只有 `model_ratio=1.25`、`group_ratio=1`、`user_group_ratio=-1`，实际扣费没有体现 2 倍。

根因：

- `relay/helper/price.go` 的预扣阶段已经计算了：
  - `系统全局模型倍率 * 用户 global_model_ratio`
- 该合成值存入 `PriceData.GlobalModelRatio`。
- 但 `service/text_quota.go` 的文本最终结算只乘了：
  - `model_ratio * group_ratio`
- 最终 `actualQuota` 漏乘 `PriceData.GlobalModelRatio`。
- 结果表现为预扣可能按 2 倍，最终结算又按 1 倍实际用量退回。

修复：

- `service/text_quota.go`
  - `textQuotaSummary` 增加 `GlobalModelRatio`。
  - token 计费最终结算改为：
    - `model_ratio * group_ratio * global_model_ratio`
  - 固定价格模型最终结算也乘：
    - `model_price * quota_per_unit * group_ratio * global_model_ratio`
- `service/log_info_generate.go`
  - 日志 `other` 增加 `global_model_ratio`，方便后续查账。
- `service/text_quota_test.go`
  - 增加 `TestCalculateTextQuotaSummaryUsesGlobalModelRatio`。
  - 覆盖 `GlobalModelRatio=2` 时最终结算翻倍。
  - 旧测试显式设置 `GlobalModelRatio=1`，避免零值语义误判。

结论：

- 系统设置里的“全局模型倍率”和用户级 `global_model_ratio` 是合成到同一个 `PriceData.GlobalModelRatio` 中的。
- 因此这次修复同时修复系统全局模型倍率和用户单独倍率在文本最终结算中漏乘的问题。
- 本地 `options` 表未查询到 `GlobalModelRatio`，当前环境系统全局倍率应走默认值 `1`。

验证：

- `go test ./service -run "TestCalculateTextQuotaSummary"` 通过。
- `go test ./relay/helper` 通过。
- `go test ./service` 全量仍有既有失败，失败点在 channel affinity / task subscription 测试，与本次文本最终结算修复不在同一路径。

## 10. 其他相关改动

认证与日志：

- `middleware/auth.go`
  - 增强 token auth 调试日志，记录 token 来源、选中 key、token id、用户 id、group 等。
- `constant/context_key.go`
  - 增加 Xiaomi Claude trace、渠道转发预检等上下文 key。

Responses / OpenAI compatible 辅助：

- `service/openaicompat/responses_chat_compat.go`
- `service/openaicompat/responses_chat_compat_test.go`
- `service/openai_chat_responses_compat.go`
- `relay/channel/xiaomi/responses_compat.go`

用途：

- 支撑 Claude / OpenAI Compatible / Responses 之间的兼容链路。
- 记录 `request_conversion`，便于从日志判断最终请求协议。

前端日志展示：

- `web/src/components/table/usage-logs/UsageLogsColumnDefs.jsx`
  - 配合新增日志字段展示计费来源、订阅扣费、倍率等信息。

## 11. 本轮关键验证命令

已跑过并通过：

```bash
go test ./service -run "TestCalculateTextQuotaSummary"
go test ./relay/helper
go test ./model -run 'TestNonExistent'
go test ./controller ./dto
```

曾跑过但存在无关既有失败：

```bash
go test ./service
```

失败点集中在既有 channel affinity / task subscription 测试，不是 Xiaomi Claude、渠道转发预检或文本全局倍率修复引入的主路径失败。

## 12. 当前已知风险与后续建议

Xiaomi hosted/server tools：

- `custom` tools 已确认可触发。
- `web_search_20250305` 这类 Anthropic hosted/server tool 当前实测不通。
- 若要让 Claude Code 的实时搜索真正可用，需要额外接入真实搜索后端，并由网关模拟 hosted tool 的执行和返回。

渠道转发预检：

- 预检依赖模型稳定输出 JSON。
- 如果模型输出非 JSON 或指标缺失，应继续按 511 固定错误处理。
- 预检消耗目前不向用户扣费，但需要持续保留系统侧成本记录。

错误拦截：

- 用户侧错误可被模板替换。
- 日志必须继续保留真实上游错误，否则后续排障会失去证据。

倍率计费：

- 文本主路径已修复最终结算漏乘 `GlobalModelRatio`。
- 后续如扩展到更多附加费用项，应明确它们是否也应受全局倍率影响，例如 hosted web search、file search、image generation call 等附加单价。

---

# 2026-05-14 钱包充值页金额展示调整

目标：

- `/console/topup` 的“额度充值”预设卡片面额改回美元额度展示，例如 `$20`。
- 用户选定面额后的提示卡继续区分实际支付金额和到账美元余额。
- “当前倍率”文案方向从 `1 CNY = 6.67 USD` 改为 `1 USD = 0.15 CNY`。

主要改动：

- `web/src/components/topup/RechargeCard.jsx`
  - 新增 `formatUsdAmount()` 用于充值额度美元展示。
  - 预设卡片从实际支付币种金额改为显示美元充值额度。
  - `到账余额` 改为美元额度，例如 `$20`。
  - `支付金额` 继续使用 `convertTopupBaseToPaymentCurrency(...)` 显示实际支付币种金额。
  - `当前倍率` 改为使用 `priceRatio` 直接展示 `1 USD = {priceRatio} {支付币种}`。

历史追踪：

- `git log -- web/src/components/topup/RechargeCard.jsx` 显示相关 UI 大改集中在 `b38b7def`。
- 当时卡面金额接入 `convertTopupBaseToPaymentCurrency(...)`，导致卡片从美元额度改成了实际支付币种金额。

验证：

- 使用临时输出目录验证前端构建，避免覆盖 `web/dist`：
  - `bunx vite build --outDir ../.vite-topup-usd-check --emptyOutDir=true`
- 构建通过，仅保留项目已有的 chunk size / circular chunk 警告。
- 临时目录 `.vite-topup-usd-check` 已清理。
