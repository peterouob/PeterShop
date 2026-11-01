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
    status smallint not null ,
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null
);

create table payment_flow (
    id bigint generated always as identity primary key ,
    flow_no bigint not null ,
    gid bigint not null ,
    order_no bigint not null ,
    user_id varchar(64) not null ,
    amount bigint not null ,
    direction smallint not null ,
    created_at timestamptz(3) not null ,
    constraint uk_flow unique (flow_no),
    constraint uk_order_direction unique (order_no, direction)
);