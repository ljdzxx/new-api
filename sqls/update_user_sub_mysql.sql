-- 1. 预览当前生效订户套餐
SELECT
  us.user_id AS `用户id`,
  u.username AS `用户名`,
  us.plan_id AS `套餐id`,
  FROM_UNIXTIME(us.start_time) AS `套餐生效时间`,
  FROM_UNIXTIME(us.end_time) AS `截止时间`,
  us.amount_total AS `总额度`
FROM user_subscriptions us
JOIN users u ON u.id = us.user_id
WHERE us.status = 'active'
  AND us.end_time > UNIX_TIMESTAMP()
ORDER BY us.user_id, us.plan_id, us.start_time;

START TRANSACTION;

-- 备份本次会修改的记录
CREATE TABLE IF NOT EXISTS ops_backup_user_subscriptions_amount_total_20260615 AS
SELECT us.*
FROM user_subscriptions us
WHERE us.status = 'active'
  AND us.end_time > UNIX_TIMESTAMP();

-- 2. 将当前生效订阅的重置额度乘以 0.15
UPDATE user_subscriptions
SET
  amount_total = CAST(ROUND(amount_total * 15 / 100) AS SIGNED),
  updated_at = UNIX_TIMESTAMP()
WHERE status = 'active'
  AND end_time > UNIX_TIMESTAMP();

-- 复查更新后结果
SELECT
  us.user_id AS `用户id`,
  u.username AS `用户名`,
  us.plan_id AS `套餐id`,
  FROM_UNIXTIME(us.start_time) AS `套餐生效时间`,
  FROM_UNIXTIME(us.end_time) AS `截止时间`,
  us.amount_total AS `总额度`
FROM user_subscriptions us
JOIN users u ON u.id = us.user_id
WHERE us.status = 'active'
  AND us.end_time > UNIX_TIMESTAMP()
ORDER BY us.user_id, us.plan_id, us.start_time;

COMMIT;