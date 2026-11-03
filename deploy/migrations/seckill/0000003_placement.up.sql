create table outbox (
    id bigint generated always as identity primary key,
    order_no bigint not null ,
    payload jsonb not null ,
    created_at timestamptz(3) not null default now(),
    constraint uk_outbox_order_no unique (order_no)
);

alter table stock_ledger add constraint  uk_ledger_activity_user unique (activity_id, user_id);
