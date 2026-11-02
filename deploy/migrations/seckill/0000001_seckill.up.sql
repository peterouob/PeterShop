create table seckill_activity (
    activity_id bigint primary key,
    product_id  varchar(64) not null,
    price       bigint not null,
    start_at    timestamptz(3) not null,
    end_at      timestamptz(3) not null,
    status      smallint not null,
    created_at  timestamptz(3) not null,
    updated_at  timestamptz(3) not null,
    constraint ck_activity_window check (end_at > start_at),
    constraint ck_activity_price_positive check ( price > 0 ),
    constraint ck_activity_status CHECK ( status in (0, 1, 2, 3, 4, 5) )
);

create INDEX  idx_start ON seckill_activity (start_at, status);

create table activity_stock (
    activity_id bigint primary key
        references seckill_activity (activity_id),
    total       int not null,
    available   int not null,
    locked      int not null,
    sold        int not null,

    constraint ck_stock_non_negative check (available >= 0 and locked >= 0 and sold >= 0),
    constraint ck_stock_i2 check (total = available + locked + sold),
    constraint ck_stock_bigger_zero check ( total > 0 )
) with (fillfactor = 70);
