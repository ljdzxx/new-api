-- 1. 预览当前生效订户套餐
SELECT
  us.user_id AS "用户id",
  u.username AS "用户名",
  us.plan_id AS "套餐id",
  datetime(us.start_time, 'unixepoch', 'localtime') AS "套餐生效时间",
  datetime(us.end_time, 'unixepoch', 'localtime') AS "截止时间",
  us.amount_total AS "总额度"
FROM user_subscriptions us
JOIN users u ON u.id = us.user_id
WHERE us.status = 'active'
  AND us.end_time > strftime('%s', 'now')
ORDER BY us.user_id, us.plan_id, us.start_time;

BEGIN TRANSACTION;

-- 备份本次会修改的记录
CREATE TABLE IF NOT EXISTS ops_backup_user_subscriptions_amount_total_20260615 AS
SELECT us.*
FROM user_subscriptions us
WHERE us.status = 'active'
  AND us.end_time > strftime('%s', 'now');

-- 2. 将当前生效订阅的重置额度乘以 0.15
UPDATE user_subscriptions
SET
  amount_total = CAST(ROUND(amount_total * 15.0 / 100.0) AS INTEGER),
  updated_at = CAST(strftime('%s', 'now') AS INTEGER)
WHERE status = 'active'
  AND end_time > strftime('%s', 'now');

-- 复查更新后结果
SELECT
  us.user_id AS "用户id",
  u.username AS "用户名",
  us.plan_id AS "套餐id",
  datetime(us.start_time, 'unixepoch', 'localtime') AS "套餐生效时间",
  datetime(us.end_time, 'unixepoch', 'localtime') AS "截止时间",
  us.amount_total AS "总额度"
FROM user_subscriptions us
JOIN users u ON u.id = us.user_id
WHERE us.status = 'active'
  AND us.end_time > strftime('%s', 'now')
ORDER BY us.user_id, us.plan_id, us.start_time;

COMMIT;