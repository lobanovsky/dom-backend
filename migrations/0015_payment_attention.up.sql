-- Флаг «обратить внимание» у входящего платежа: например, оплата капремонта пришла на обычный счёт.
-- Выставляется при загрузке реестра автоматически и вручную; при включённом флаге комментарий обязателен (проверяет приложение).
ALTER TABLE incoming_payments ADD COLUMN attention BOOLEAN NOT NULL DEFAULT false;
CREATE INDEX incoming_payments_attention_idx ON incoming_payments (payment_date) WHERE attention;

-- Ранее загруженные платежи с пометкой о несоответствии типа счёта (она была только в комментарии).
UPDATE incoming_payments SET attention = true
 WHERE comment IN ('оплата капремонта поступила на обычный (не специальный) банковский счёт',
                   'оплата по лицевому счёту не за капремонт поступила на специальный счёт капремонта');
