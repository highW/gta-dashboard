-- 005_warrant_workflow.sql  (MySQL 8)
-- Warrants get a life cycle instead of an on/off switch:
--
--   requested ──approve──► active ──clear──► cleared
--       └───────deny─────► denied
--
-- Police, supervisors and prosecutors request; a judge approves or denies
-- (and can still issue a warrant directly, which starts as active).
--
--   warrants.status         new: requested / active / denied / cleared
--   warrants.active         now derived from status (never written)
--   warrants.decided_by     new: the account that approved or denied
--   warrants.decided_at     new: when (UTC)
--   warrants.decision_note  new: optional reason
--
-- Existing rows: active=1 becomes "active", active=0 becomes "cleared".
--
-- New roles (supervisor, prosecutor) need no database change: memberships
-- store the role by name.
--
-- BEFORE RUNNING: take a dump, stop the API instances, run this as root,
-- then start the NEW backend.
--
-- Safe to run again, including after a run that stopped half way: ALTER
-- TABLE can't be rolled back in MySQL, so every step checks whether its own
-- change is already there instead of trusting one check at the start.

SET SESSION sql_mode = CONCAT_WS(',', NULLIF(@@SESSION.sql_mode, ''), 'STRICT_TRANS_TABLES');

DROP PROCEDURE IF EXISTS migrate_005_warrant_workflow;

DELIMITER //
CREATE PROCEDURE migrate_005_warrant_workflow()
BEGIN
  DECLARE has_status, has_active, active_generated, n INT DEFAULT 0;

  SELECT COUNT(*) INTO has_status FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'warrants' AND column_name = 'status';
  SELECT COUNT(*), COALESCE(MAX(extra LIKE '%GENERATED%'), 0) INTO has_active, active_generated
    FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'warrants' AND column_name = 'active';

  -- 1. The status column, filled from the old on/off switch.
  IF has_status = 0 THEN
    ALTER TABLE warrants ADD COLUMN status VARCHAR(20) NULL AFTER date_issued;
  END IF;
  IF has_active = 1 AND active_generated = 0 THEN
    UPDATE warrants SET status = IF(active, 'active', 'cleared') WHERE status IS NULL;
  END IF;

  -- 2. An index on (server_id, status) BEFORE the old one goes: the server_id
  --    foreign key needs some index that starts with server_id at all times.
  SELECT COUNT(*) INTO n FROM information_schema.statistics
   WHERE table_schema = DATABASE() AND table_name = 'warrants' AND index_name = 'idx_warrants_server_status';
  IF n = 0 THEN
    ALTER TABLE warrants ADD KEY idx_warrants_server_status (server_id, status);
  END IF;

  -- 3. Replace the old stored switch with one derived from status.
  IF has_active = 1 AND active_generated = 0 THEN
    SELECT COUNT(*) INTO n FROM information_schema.statistics
     WHERE table_schema = DATABASE() AND table_name = 'warrants' AND index_name = 'idx_warrants_server_active';
    IF n > 0 THEN
      ALTER TABLE warrants DROP INDEX idx_warrants_server_active;
    END IF;
    ALTER TABLE warrants DROP COLUMN active;
    SET has_active = 0;
  END IF;

  ALTER TABLE warrants MODIFY status VARCHAR(20) NOT NULL;

  IF has_active = 0 THEN
    ALTER TABLE warrants ADD COLUMN active TINYINT(1) AS (status = 'active') STORED NOT NULL AFTER status;
  END IF;

  SELECT COUNT(*) INTO n FROM information_schema.statistics
   WHERE table_schema = DATABASE() AND table_name = 'warrants' AND index_name = 'idx_warrants_server_active';
  IF n = 0 THEN
    ALTER TABLE warrants ADD KEY idx_warrants_server_active (server_id, active);
  END IF;

  -- 4. Who decided a request, when, and why.
  SELECT COUNT(*) INTO n FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'warrants' AND column_name = 'decided_by';
  IF n = 0 THEN
    ALTER TABLE warrants ADD COLUMN decided_by VARCHAR(255) NULL AFTER active;
  END IF;
  SELECT COUNT(*) INTO n FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'warrants' AND column_name = 'decided_at';
  IF n = 0 THEN
    ALTER TABLE warrants ADD COLUMN decided_at DATETIME NULL AFTER decided_by;
  END IF;
  SELECT COUNT(*) INTO n FROM information_schema.columns
   WHERE table_schema = DATABASE() AND table_name = 'warrants' AND column_name = 'decision_note';
  IF n = 0 THEN
    ALTER TABLE warrants ADD COLUMN decision_note VARCHAR(500) NULL AFTER decided_at;
  END IF;

  SELECT status, COUNT(*) AS warrants FROM warrants GROUP BY status ORDER BY status;
END//
DELIMITER ;

CALL migrate_005_warrant_workflow();

DROP PROCEDURE IF EXISTS migrate_005_warrant_workflow;
