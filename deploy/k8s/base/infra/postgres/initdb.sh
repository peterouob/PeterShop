#!/bin/sh
set -eu
for svc in user seckill order payment; do
  eval "pw=\${PGPASSWORD_${svc}}"
  psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres \
    -v role="svc_${svc}" -v db="db_${svc}" -v pw="$pw" <<'SQL'
CREATE ROLE :"role" LOGIN PASSWORD :'pw';
CREATE DATABASE :"db" OWNER :"role";
REVOKE CONNECT ON DATABASE :"db" FROM PUBLIC;
SQL
done
