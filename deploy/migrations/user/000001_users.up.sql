CREATE TABLE users (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    VARCHAR(64)    NOT NULL,
    username   VARCHAR(64)    NOT NULL,
    password   VARCHAR(128)   NOT NULL,
    created_at TIMESTAMPTZ(3) NULL,
    updated_at TIMESTAMPTZ(3) NULL,
    deleted_at TIMESTAMPTZ(3) NULL,
    CONSTRAINT uk_user_id UNIQUE (user_id),
    CONSTRAINT uk_username UNIQUE (username)
);
CREATE INDEX idx_users_deleted_at ON users (deleted_at);
