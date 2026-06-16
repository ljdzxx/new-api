SET @cutoff := 1781436600;

DROP TEMPORARY TABLE IF EXISTS ops_user_last_recharge_20260615;
CREATE TEMPORARY TABLE ops_user_last_recharge_20260615 AS
SELECT
  user_id,
  MAX(CASE WHEN complete_time > 0 THEN complete_time ELSE create_time END) AS last_recharge_time
FROM top_ups
WHERE status = 'success'
  AND amount > 0
GROUP BY user_id;

-- 预览将被处理的用户
SELECT
  u.id AS `用户id`,
  u.username AS `用户名`,
  FROM_UNIXTIME(lr.last_recharge_time) AS `最后充值时间`,
  u.quota AS `当前余额`,
  CASE WHEN lr.last_recharge_time < @cutoff THEN 0.15 ELSE 0.2 END AS `系数`,
  CAST(ROUND(u.quota * CASE WHEN lr.last_recharge_time < @cutoff THEN 0.15 ELSE 0.2 END) AS SIGNED) AS `调整后余额`
FROM users u
JOIN ops_user_last_recharge_20260615 lr ON lr.user_id = u.id
WHERE u.deleted_at IS NULL
ORDER BY lr.last_recharge_time DESC, u.id;

-- 备份
CREATE TABLE ops_backup_users_quota_20260615 AS
SELECT
  u.id AS user_id,
  u.username,
  u.quota AS old_quota,
  lr.last_recharge_time,
  CASE WHEN lr.last_recharge_time < @cutoff THEN 0.15 ELSE 0.2 END AS factor,
  CAST(ROUND(u.quota * CASE WHEN lr.last_recharge_time < @cutoff THEN 0.15 ELSE 0.2 END) AS SIGNED) AS new_quota,
  UNIX_TIMESTAMP() AS backed_up_at
FROM users u
JOIN ops_user_last_recharge_20260615 lr ON lr.user_id = u.id
WHERE u.deleted_at IS NULL;

START TRANSACTION;

UPDATE users u
JOIN ops_user_last_recharge_20260615 lr ON lr.user_id = u.id
SET u.quota = CAST(ROUND(u.quota * CASE WHEN lr.last_recharge_time < @cutoff THEN 0.15 ELSE 0.2 END) AS SIGNED)
WHERE u.deleted_at IS NULL;

-- 如启用 Redis，生成需删除的用户缓存 key
SELECT CONCAT('DEL user:', user_id) AS redis_cmd
FROM ops_user_last_recharge_20260615;

COMMIT;