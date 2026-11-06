-- Возвращает правило «один собственник»; миграция вниз упадёт, если у машиноместа уже несколько собственников.
UPDATE premises_kinds SET single_owner = true WHERE code = 'parking_space';
