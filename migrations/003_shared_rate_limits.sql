-- +goose Up
CREATE TABLE IF NOT EXISTS rate_limit_windows (
    identity text NOT NULL,
    window_start timestamptz NOT NULL,
    request_count bigint NOT NULL CHECK (request_count > 0),
    PRIMARY KEY (identity, window_start)
);

-- +goose Down
DROP TABLE IF EXISTS rate_limit_windows;
