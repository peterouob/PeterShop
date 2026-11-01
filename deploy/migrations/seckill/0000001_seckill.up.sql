create table seckill_activity (
    id bigint generated always as identity  primary key ,
    activity_id bigint not null ,
    product_id varchar(64) not null ,
    total_stock int not null ,
    price bigint not null ,
    start_at timestamptz(3) not null ,
    end_at timestamptz(3) not null ,
    status smallint not null ,
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null ,
    constraint uk_activity_id UNIQUE (activity_id)
);

create INDEX  idx_start ON seckill_activity (start_at, status);

create table stock (
    id bigint generated always as identity primary key ,
    product_id varchar(64) not null ,
    total int not null ,
    available int not null ,
    locked int not null ,
    sold int not null ,

    constraint uk_product UNIQUE (product_id),
    constraint ck_stock_non_negative CHECK ( available >= 0 and locked >= 0 and sold >= 0 ),
    constraint ck_stock_i2 check ( total = available + locked + sold )
);

create table stock_ledger (
    order_no bigint primary key ,
    product_id varchar(64) not null ,
    activity_id bigint not null ,
    user_id varchar(64) not null ,
    status smallint not null ,
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null
);

create index idx_activity_status on stock_ledger (activity_id, status);

create table if not exists tcc_barrier (
    id bigint generated always as identity not null ,
    gid bigint not null ,
    branch_id varchar(64) not null ,
    op text not null check ( op IN ('try','confirm','cancel')),
    created_at timestamptz(3) not null ,
    constraint uk_barrier unique (gid, branch_id, op)
)