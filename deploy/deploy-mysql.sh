#!/bin/sh
set -eu

# 部署前修改以下占位符；这些初始化参数仅在数据目录为空时生效。
MYSQL_DATABASE='YOUR_DATABASE_NAME'
MYSQL_USER='YOUR_DATABASE_USER'
MYSQL_PASSWORD='YOUR_DATABASE_PASSWORD'
MYSQL_ROOT_PASSWORD='YOUR_ROOT_PASSWORD'

mkdir -p /mnt/mysql8/data /mnt/mysql8/log /mnt/mysql8/conf.d

# 确保文件挂载源存在，并保留已有配置。
if [ ! -e /mnt/mysql8/conf.d/my.cnf ]; then
  printf '[mysqld]\n' > /mnt/mysql8/conf.d/my.cnf
fi
if [ ! -f /mnt/mysql8/conf.d/my.cnf ]; then
  echo '错误：/mnt/mysql8/conf.d/my.cnf 必须是普通文件。' >&2
  exit 1
fi

docker run -d \
  --name mysql8 \
  --restart unless-stopped \
  -p 4810:3306 \
  -e TZ=Asia/Shanghai \
  -e MYSQL_DATABASE="$MYSQL_DATABASE" \
  -e MYSQL_USER="$MYSQL_USER" \
  -e MYSQL_PASSWORD="$MYSQL_PASSWORD" \
  -e MYSQL_ROOT_PASSWORD="$MYSQL_ROOT_PASSWORD" \
  -v /mnt/mysql8/data:/var/lib/mysql \
  -v /mnt/mysql8/log:/var/log/mysql \
  -v /mnt/mysql8/conf.d/my.cnf:/etc/mysql/conf.d/my.cnf \
  mysql:8.0.28
