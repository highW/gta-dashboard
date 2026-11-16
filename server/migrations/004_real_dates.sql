-- 004_real_dates.sql  (MySQL 8)
-- Turns the four text columns that hold dates into real date types:
--
--   audit_log.timestamp    VARCHAR -> DATETIME NOT NULL   (UTC)
--   players.last_seen      VARCHAR -> DATETIME NULL       (UTC)
--   arrests.date           VARCHAR -> DATE NULL
--   warrants.date_issued   VARCHAR -> DATE NULL
--
-- Why: text can't be sorted or compared as time ("audit entries older than
-- 30 days"), and nothing stopped a non-date from being stored.
--
-- What happens to the values already there:
--   * audit_log.timestamp ("2026-10-13T19:17:45-04:00") is converted to UTC.
--     If even one row is in a format this file doesn't know, step 1 fails
--     and NOTHING is changed, in any table.
--   * last_seen placeholders ("Now", "3 min ago", "2 hours ago") become real
--     times, counted back from the moment this runs.
--   * arrest and warrant dates ("2026-08-14") stay exactly as they are.
--   * anything unreadable in those three columns becomes NULL. The summary
--     printed at the end counts them; the original text is in your dump.
--
-- BEFORE RUNNING:
--   1. Take a fresh dump, and try this file on a copy restored from it first
--      (the same routine as migration 001).
--   2. Stop the API instances. Start the NEW backend only after this has
--      run: it reads and writes these columns as dates.
--
-- Safe to run twice: on columns that are already converted, every step
-- leaves the data as it is.
-- Needs a user that may ALTER tables and create a procedure (root), not the
-- app's limited user.
--
-- The steps live in a temporary stored procedure for one reason: inside a
-- procedure the first error ends everything. Run as loose statements, some
-- tools (and the mysql prompt's SOURCE command) would carry on with the next
-- statement after an error.

-- Strict mode makes a bad value an error instead of a silent guess. The
-- procedure created below keeps this setting; nothing else is affected.
SET SESSION sql_mode = CONCAT_WS(',', NULLIF(@@SESSION.sql_mode, ''), 'STRICT_TRANS_TABLES');

DROP PROCEDURE IF EXISTS migrate_004_real_dates;

DELIMITER //
CREATE PROCEDURE migrate_004_real_dates()
BEGIN
  -- ---------------------------------------------------------------
  -- 1. audit_log.timestamp  (first on purpose: the only step that can refuse)
  --    The text is first rewritten as 'YYYY-MM-DD HH:MM:SS' in UTC. A row in
  --    an unknown format would become NULL, which this NOT NULL column
  --    rejects, undoing the statement and ending the procedure.
  -- ---------------------------------------------------------------
  UPDATE audit_log SET timestamp = DATE_FORMAT(
    CASE
      -- already 'YYYY-MM-DD HH:MM:SS' (a second run, or written by the new backend)
      WHEN timestamp REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$'
        THEN STR_TO_DATE(timestamp, '%Y-%m-%d %H:%i:%s')
      -- 2026-10-13T23:17:45Z
      WHEN timestamp REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}([.][0-9]+)?Z$'
        THEN STR_TO_DATE(LEFT(timestamp, 19), '%Y-%m-%dT%H:%i:%s')
      -- 2026-10-13T19:17:45-04:00  (what the old backend wrote: local time plus offset)
      WHEN timestamp REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}([.][0-9]+)?[+-][0-9]{2}:[0-9]{2}$'
        THEN CONVERT_TZ(STR_TO_DATE(LEFT(timestamp, 19), '%Y-%m-%dT%H:%i:%s'), RIGHT(timestamp, 6), '+00:00')
    END,
    '%Y-%m-%d %H:%i:%s');

  ALTER TABLE audit_log MODIFY timestamp DATETIME NOT NULL;

  -- ---------------------------------------------------------------
  -- 2. players.last_seen
  -- ---------------------------------------------------------------
  ALTER TABLE players MODIFY last_seen VARCHAR(255) NULL;

  UPDATE players SET last_seen = DATE_FORMAT(
    CASE
      WHEN last_seen REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$'
        THEN STR_TO_DATE(last_seen, '%Y-%m-%d %H:%i:%s')
      WHEN LOWER(TRIM(last_seen)) = 'now'
        THEN UTC_TIMESTAMP()
      WHEN last_seen REGEXP '^[0-9]{1,6} (min|mins|minute|minutes) ago$'
        THEN UTC_TIMESTAMP() - INTERVAL CAST(SUBSTRING_INDEX(last_seen, ' ', 1) AS UNSIGNED) MINUTE
      WHEN last_seen REGEXP '^[0-9]{1,6} (hour|hours) ago$'
        THEN UTC_TIMESTAMP() - INTERVAL CAST(SUBSTRING_INDEX(last_seen, ' ', 1) AS UNSIGNED) HOUR
      WHEN last_seen REGEXP '^[0-9]{1,6} (day|days) ago$'
        THEN UTC_TIMESTAMP() - INTERVAL CAST(SUBSTRING_INDEX(last_seen, ' ', 1) AS UNSIGNED) DAY
    END,
    '%Y-%m-%d %H:%i:%s');

  ALTER TABLE players MODIFY last_seen DATETIME NULL;

  -- ---------------------------------------------------------------
  -- 3. arrests.date
  --    Kept when the text starts with a real calendar day (the inner CASE
  --    rejects days a month doesn't have, such as 2026-02-30).
  -- ---------------------------------------------------------------
  ALTER TABLE arrests MODIFY date VARCHAR(255) NULL;

  UPDATE arrests SET date =
    CASE WHEN date REGEXP '^[12][0-9]{3}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])([T ].*)?$'
         THEN CASE WHEN CAST(SUBSTRING(date, 9, 2) AS UNSIGNED) <= DAY(LAST_DAY(CONCAT(LEFT(date, 7), '-01')))
                   THEN LEFT(date, 10) END
    END;

  ALTER TABLE arrests MODIFY date DATE NULL;

  -- ---------------------------------------------------------------
  -- 4. warrants.date_issued
  -- ---------------------------------------------------------------
  ALTER TABLE warrants MODIFY date_issued VARCHAR(255) NULL;

  UPDATE warrants SET date_issued =
    CASE WHEN date_issued REGEXP '^[12][0-9]{3}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])([T ].*)?$'
         THEN CASE WHEN CAST(SUBSTRING(date_issued, 9, 2) AS UNSIGNED) <= DAY(LAST_DAY(CONCAT(LEFT(date_issued, 7), '-01')))
                   THEN LEFT(date_issued, 10) END
    END;

  ALTER TABLE warrants MODIFY date_issued DATE NULL;

  -- ---------------------------------------------------------------
  -- 5. Summary, printed only when everything above succeeded.
  --    "unreadable_now_null" should be 0, or only the test rows you know
  --    had junk in them.
  -- ---------------------------------------------------------------
  SELECT 'audit_log.timestamp' AS converted_column, COUNT(*) AS total_rows, 0 AS unreadable_now_null FROM audit_log
  UNION ALL
  SELECT 'players.last_seen', COUNT(*), COALESCE(SUM(last_seen IS NULL), 0) FROM players
  UNION ALL
  SELECT 'arrests.date', COUNT(*), COALESCE(SUM(date IS NULL), 0) FROM arrests
  UNION ALL
  SELECT 'warrants.date_issued', COUNT(*), COALESCE(SUM(date_issued IS NULL), 0) FROM warrants;
END//
DELIMITER ;

CALL migrate_004_real_dates();

DROP PROCEDURE IF EXISTS migrate_004_real_dates;
