-- Migration 004: browserFlow job state + per-request account secrets.
-- request_accounts holds one row per browserFlow run: slug links OTP,
-- cards, and keys to the request email. Password is AES-256-GCM sealed
-- with CUSTOM_CARD_KEY (same envelope as custom_cards).
CREATE TABLE IF NOT EXISTS request_accounts(
  request_id TEXT PRIMARY KEY REFERENCES requests(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  password_enc TEXT NOT NULL DEFAULT '',
  salt TEXT NOT NULL DEFAULT '',
  iterations INTEGER NOT NULL DEFAULT 0,
  dob TEXT NOT NULL DEFAULT '',
  country TEXT NOT NULL DEFAULT 'US',
  otp_used INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
-- request_steps is the dashboard-visible per-step run log.
CREATE TABLE IF NOT EXISTS request_steps(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL REFERENCES requests(id) ON DELETE CASCADE,
  step TEXT NOT NULL,
  ms INTEGER NOT NULL DEFAULT 0,
  ok INTEGER NOT NULL DEFAULT 0,
  detail TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_request_steps_req ON request_steps(request_id);
-- Country picker for user-supplied cards (billing default USA).
ALTER TABLE custom_cards ADD COLUMN country TEXT NOT NULL DEFAULT 'US';
