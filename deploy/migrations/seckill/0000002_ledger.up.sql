create table stock_ledger (
    order_no bigint primary key ,
    activity_id bigint not null ,
    user_id varchar(64) not null ,
    status smallint not null , -- 1=Locked, 2=Sold, 3=Released
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null ,
    constraint ck_ledger_status check ( status in (1, 2, 3))
);

create index idx_activity_status on stock_ledger (activity_id, status);

create table tcc_barrier (
    id bigint generated always as identity primary key ,
    gid bigint not null ,
    branch_id varchar(64) not null ,
    op text not null,
    created_at timestamptz(3) not null ,
    constraint uk_barrier unique (gid, branch_id, op),
    constraint ck_barrier_op check ( op in ('try','confirm','cancel'))
);

create index idx_barrier_created on tcc_barrier (created_at);

