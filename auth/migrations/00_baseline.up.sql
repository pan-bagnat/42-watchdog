-- USERS --

CREATE TABLE users (
  id TEXT PRIMARY KEY, -- user_ULID
  ft_login TEXT NOT NULL UNIQUE,
  ft_id BIGINT NOT NULL,
  ft_is_staff BOOLEAN NOT NULL,
  photo_url TEXT NOT NULL,
  last_seen TIMESTAMP WITH TIME ZONE NOT NULL,
  is_staff BOOLEAN NOT NULL,
  is_blacklisted BOOLEAN NOT NULL
);

CREATE TABLE sessions (
  session_id   TEXT PRIMARY KEY,
  ft_login     TEXT REFERENCES users(ft_login),
  created_at   TIMESTAMP NOT NULL DEFAULT NOW(),
  expires_at   TIMESTAMP NOT NULL,
  user_agent   TEXT      NOT NULL,
  ip           TEXT      NOT NULL,
  device_label TEXT      NOT NULL,
  last_seen    TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_login_expires   ON sessions (ft_login, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_login_useragent ON sessions (ft_login, user_agent);
CREATE INDEX IF NOT EXISTS idx_sessions_last_seen       ON sessions (last_seen DESC);
