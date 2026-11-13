DROP TABLE gate_events;
DROP TABLE gate_requests;
-- При откате учётные записи охраны отключаются, чтобы не открыть им разделы роли просмотра.
UPDATE users SET role='viewer',active=false,token_version=token_version+1 WHERE role='guard';
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK(role IN('admin','viewer'));
