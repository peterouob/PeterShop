drop table if exists outbox;
alter table stock_ledger drop constraint if exists uk_ledger_activity_user;