create publication seckill_outbox for table outbox;
grant select on outbox to svc_cdc;