-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
    id VARCHAR(255) PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE profiles (
    user_id VARCHAR(255) PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL
);

CREATE INDEX idx_users_email ON users(email);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS profiles;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
