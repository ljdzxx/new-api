# Project Review Notes

Last updated: 2026-04-08

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