CREATE TABLE IF NOT EXISTS `user` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `user_id`    VARCHAR(64)     NOT NULL,
    `username`   VARCHAR(64)     NOT NULL,
    `password`   VARCHAR(128)    NOT NULL,
    `created_at` DATETIME(3)     NULL,
    `updated_at` DATETIME(3)     NULL,
    `deleted_at` DATETIME(3)     NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_user_id` (`user_id`),
    UNIQUE KEY `uk_username` (`username`),
    KEY `idx_deleted_at` (`deleted_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `product` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `product_id`  VARCHAR(64)     NOT NULL,
    `name`        VARCHAR(128)    NOT NULL,
    `price`       BIGINT          NOT NULL DEFAULT 0,
    `description` VARCHAR(512)    NOT NULL DEFAULT '',
    `img`         VARCHAR(512)    NOT NULL DEFAULT '',
    `created_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_product_id` (`product_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `stock` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `product_id` VARCHAR(64)     NOT NULL,
    `stock`      INT             NOT NULL DEFAULT 0,
    `updated_at` DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_stock_product_id` (`product_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `orders` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `order_id`   VARCHAR(128)    NOT NULL,
    `user_id`    VARCHAR(64)     NOT NULL,
    `product_id` VARCHAR(64)     NOT NULL,
    `create_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_order_id` (`order_id`),
    UNIQUE KEY `uk_user_product` (`user_id`, `product_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
