CREATE TABLE IF NOT EXISTS custom_cards(
  card_id TEXT PRIMARY KEY REFERENCES cards(card_id) ON DELETE CASCADE,
  enc_blob TEXT NOT NULL,
  salt TEXT NOT NULL,
  iterations INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
