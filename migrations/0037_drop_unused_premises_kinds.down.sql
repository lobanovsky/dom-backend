UPDATE premises_kinds SET deleted_at = NULL, updated_at = now() WHERE code IN ('storage', 'non_residential') AND deleted_at IS NOT NULL;
