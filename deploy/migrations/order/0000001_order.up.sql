CREATE TABLE orders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_no BIGINT NOT NULL ,
    user_id VARCHAR(64) NOT NULL ,
    product_id VARCHAR(64) NOT NULL ,
    activity_id BIGINT NOT NULL ,
    amount BIGINT NOT NULL ,
    status SMALLINT NOT NULL ,
    expire_at timestamptz(3) NOT NULL ,
    created_at timestamptz(3) NOT NULL ,
    updated_at timestamptz(3) NOT NULL,

    CONSTRAINT uk_order_no UNIQUE (order_no),
    CONSTRAINT uk_user_product UNIQUE (user_id, product_id)
);

create index idx_status_expire on orders (status, expire_at);

create table tcc_transaction (
    id bigint generated always as identity primary key ,
    gid bigint not null ,
    business varchar(32) not null ,
    order_no bigint not null ,
    amount bigint not null ,
    status smallint not null ,

    retry_count int not null default 0,
    next_retry_at timestamptz(3) not null ,
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null ,
    constraint uk_grid unique (gid)
);

create index idx_order on tcc_transaction (order_no);
create index idx_recovery on tcc_transaction (status, next_retry_at);

create table tcc_branch (
    id bigint generated always as identity primary key ,
    gid bigint not null ,
    branch_id varchar(32) not null ,
    action varchar(16) not null ,
    status smallint not null ,
    last_error varchar(255) null ,
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null ,
    constraint uk_branch_action unique (gid, branch_id, action)
);