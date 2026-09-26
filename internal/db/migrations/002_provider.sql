ALTER TABLE cards ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN provider TEXT NOT NULL DEFAULT '';
UPDATE cards SET provider = CASE WHEN card_id LIKE 'chkr\_%' ESCAPE '\' THEN 'onramp' ELSE 'kripi' END WHERE provider = '';
UPDATE requests SET provider = (SELECT c.provider FROM cards c WHERE c.card_id = requests.card_id)
WHERE provider = '' AND card_id IS NOT NULL
  AND EXISTS (SELECT 1 FROM cards c WHERE c.card_id = requests.card_id);
