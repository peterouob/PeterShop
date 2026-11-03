CREATE TABLE orders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_no BIGINT NOT NULL ,
    user_id VARCHAR(64) NOT NULL ,
    product_id VARCHAR(64) NOT NULL ,
    activity_id BIGINT NOT NULL ,
    amount BIGINT NOT NULL ,
    status SMALLINT NOT NULL , -- 1=Pending, 2=Paying, 3=Paid, 4=Canceled, 5=Refunding, 6= Refunded
    expire_at timestamptz(3) NOT NULL ,
    created_at timestamptz(3) NOT NULL ,
    updated_at timestamptz(3) NOT NULL,

    CONSTRAINT uk_order_no UNIQUE (order_no),
    CONSTRAINT uk_user_activity UNIQUE (user_id, activity_id),

    constraint ck_order_status check ( status in (1,2,3,4,5,6)),
    constraint ck_order_amount check ( amount > 0 )
);

create index idx_status_expire on orders (status, expire_at);

create table tcc_transaction(
    id            bigint generated always as identity primary key,
    gid           bigint         not null,
    business      varchar(32)    not null,
    order_no      bigint         not null,
    amount        bigint         not null,
    status        smallint       not null, -- 1=Trying, 2=Confirming, 3=cancelling, 4=confirmed, 5=canceled, 6=failed
    decision      text           ,
    retry_count   int            not null default 0,
    next_retry_at timestamptz(3) not null,
    created_at    timestamptz(3) not null,
    updated_at    timestamptz(3) not null,
    constraint uk_gid unique (gid),

    constraint ck_tcc_transaction_status check ( status in (1, 2, 3, 4, 5, 6)),
    constraint ck_tcc_transaction_amount check ( amount > 0 ),
    constraint ck_tcc_transaction_business check ( business in ('order_pay', 'order_refund') ),
    constraint ck_tcc_transaction_decision check ( decision in ('CONFIRM', 'CANCEL') )
);

create index idx_order on tcc_transaction (order_no);
create index idx_recovery on tcc_transaction (status, next_retry_at) where status in (1, 2, 3);

create table tcc_branch (
    id bigint generated always as identity primary key ,
    gid bigint not null ,
    branch_id varchar(64) not null ,
    action varchar(16) not null , -- 'try'/'confirm'/'cancel'
    status smallint not null , -- 0=Init, 1=Succeed, 2=Failed
    last_error varchar(255) null ,
    created_at timestamptz(3) not null ,
    updated_at timestamptz(3) not null ,
    constraint uk_branch_action unique (gid, branch_id, action),
    constraint ck_tcc_branch_status check ( status in (0, 1, 2)),
    constraint ck_tcc_branch_action check ( action in ('try', 'confirm', 'cancel') )
);