CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS cards(
  card_id TEXT PRIMARY KEY, last4 TEXT NOT NULL, bin TEXT NOT NULL,
  name_on_card TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active',
  claimed INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS card_service_stats(
  card_id TEXT NOT NULL, service TEXT NOT NULL,
  ok_count INTEGER NOT NULL DEFAULT 0, fail_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT, updated_at TEXT NOT NULL,
  PRIMARY KEY(card_id, service)
);
CREATE TABLE IF NOT EXISTS requests(
  id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, service TEXT NOT NULL DEFAULT 'x',
  key_quantity INTEGER NOT NULL, status TEXT NOT NULL, card_id TEXT,
  error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS keys(
  id TEXT PRIMARY KEY, request_id TEXT NOT NULL, key_value TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS inbound_emails(
  message_id TEXT PRIMARY KEY, recipient TEXT NOT NULL, sender TEXT,
  subject TEXT, body_text TEXT, body_html TEXT, received_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS webhook_events(
  event_id TEXT PRIMARY KEY, event_type TEXT NOT NULL, payload TEXT NOT NULL,
  received_at TEXT NOT NULL
);
