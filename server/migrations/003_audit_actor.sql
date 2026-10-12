-- 003_audit_actor.sql (MySQL 8)
-- The audit log records WHO acted (Discord username, or "api key"), not just a role.
-- Run this on the live database BEFORE starting the new backend: the new code selects this column.
ALTER TABLE audit_log ADD COLUMN actor VARCHAR(100) NOT NULL DEFAULT '' AFTER role;
