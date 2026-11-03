create table account (
    id bigint generated always as identity primary key ,
    user_id varchar(64) not null ,
    available bigint not null ,
    frozen bigint not null ,
    constraint uk_user unique (user_id),
    constraint ck_account_non_negative check ( available >= 0 and frozen >= 0 )
);

create table payment_freeze (
    gid bigint primary key ,
    order_no bigint not null ,
    user_id varchar(64) not null ,
    amount bigint not null ,
    status smallint not null , -- 1=FROZEN 2=CONFIRMED 3=RELEASED
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null,
    constraint ck_freeze_status check ( status in (1, 2, 3)),
    constraint ck_freeze_amount check ( amount > 0 )
);

create table payment_flow (
    id bigint generated always as identity primary key ,
    flow_no bigint not null ,
    gid bigint not null ,
    order_no bigint not null ,
    user_id varchar(64) not null ,
    amount bigint not null ,
    direction smallint not null , -- 1=debit 2=refund
    created_at timestamptz(3) not null ,
    constraint uk_flow unique (flow_no),
    constraint uk_order_direction unique (order_no, direction),
    constraint ck_flow_direction check ( direction in (1, 2)),
    constraint ck_flow_amount check ( amount > 0 )
);

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