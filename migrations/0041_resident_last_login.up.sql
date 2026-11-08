-- Успешный вход хранится независимо от срока жизни и удаления сессий.
ALTER TABLE resident_accounts ADD COLUMN last_login_at TIMESTAMPTZ;
-- Сохраняем доступные сведения об уже созданных сессиях, без предположений о прочих входах.
UPDATE resident_accounts a SET last_login_at=s.last_login_at
FROM (SELECT account_id,max(created_at) AS last_login_at FROM resident_sessions GROUP BY account_id) s
WHERE a.id=s.account_id;
