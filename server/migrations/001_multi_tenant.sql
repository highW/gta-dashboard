-- 001_multi_tenant.sql  (MySQL 8)
-- Adds a servers table and scopes players, arrests, warrants and audit_log to it.
-- Existing rows are backfilled into one default server (id 1). Nothing is lost.
--
-- BEFORE RUNNING:
--   1. Run server/backup.ps1 and confirm a fresh dump exists.
--   2. Try it on a throwaway database restored from that dump first.
--   3. Verify FK names:  SHOW CREATE TABLE arrests;  SHOW CREATE TABLE warrants;
--      (MySQL names them arrests_ibfk_1 / warrants_ibfk_1 unless told otherwise)

-- ---------------------------------------------------------------
-- 1. servers
-- ---------------------------------------------------------------
CREATE TABLE servers (
  id                  INT AUTO_INCREMENT PRIMARY KEY,
  name                VARCHAR(100) NOT NULL,
  slug                VARCHAR(50)  NOT NULL,
  status              VARCHAR(20)  NOT NULL DEFAULT 'pending',
  api_key_hash        CHAR(64)     NULL,      -- SHA-256 hex; plaintext key is never stored
  api_key_created_at  DATETIME     NULL,
  last_heartbeat_at   DATETIME     NULL,
  created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
                                   ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_servers_slug (slug),
  UNIQUE KEY uq_servers_api_key_hash (api_key_hash)
) ENGINE=InnoDB;

INSERT INTO servers (id, name, slug, status)
VALUES (1, 'Default community', 'default', 'active');

-- ---------------------------------------------------------------
-- 2. players
--    UNIQUE (id, server_id) lets child tables enforce "same server"
--    through a composite foreign key.
-- ---------------------------------------------------------------
ALTER TABLE players
  ADD COLUMN server_id INT NOT NULL DEFAULT 1 AFTER id;

ALTER TABLE players
  ADD UNIQUE KEY uq_players_id_server (id, server_id),
  ADD KEY idx_players_server_name (server_id, name),
  ADD CONSTRAINT fk_players_server
      FOREIGN KEY (server_id) REFERENCES servers(id);

-- ---------------------------------------------------------------
-- 3. arrests
-- ---------------------------------------------------------------
ALTER TABLE arrests DROP FOREIGN KEY arrests_ibfk_1;

ALTER TABLE arrests
  ADD COLUMN server_id INT NOT NULL DEFAULT 1 AFTER id;

ALTER TABLE arrests
  ADD KEY idx_arrests_server (server_id),
  ADD KEY idx_arrests_player_server (player_id, server_id),
  ADD CONSTRAINT fk_arrests_player
      FOREIGN KEY (player_id, server_id) REFERENCES players(id, server_id),
  ADD CONSTRAINT fk_arrests_server
      FOREIGN KEY (server_id) REFERENCES servers(id);

-- ---------------------------------------------------------------
-- 4. warrants
-- ---------------------------------------------------------------
ALTER TABLE warrants DROP FOREIGN KEY warrants_ibfk_1;

ALTER TABLE warrants
  ADD COLUMN server_id INT NOT NULL DEFAULT 1 AFTER id;

ALTER TABLE warrants
  ADD KEY idx_warrants_server_active (server_id, active),
  ADD KEY idx_warrants_player_server (player_id, server_id),
  ADD CONSTRAINT fk_warrants_player
      FOREIGN KEY (player_id, server_id) REFERENCES players(id, server_id),
  ADD CONSTRAINT fk_warrants_server
      FOREIGN KEY (server_id) REFERENCES servers(id);

-- ---------------------------------------------------------------
-- 5. audit_log
--    (server_id, id) matches your keyset pagination:
--    WHERE server_id = ? AND id < ? ORDER BY id DESC LIMIT ?
-- ---------------------------------------------------------------
ALTER TABLE audit_log
  ADD COLUMN server_id INT NOT NULL DEFAULT 1 AFTER id;

ALTER TABLE audit_log
  ADD KEY idx_audit_server_id (server_id, id),
  ADD CONSTRAINT fk_audit_server
      FOREIGN KEY (server_id) REFERENCES servers(id);

-- ---------------------------------------------------------------
-- 6. LATER, once every Go query and INSERT passes server_id explicitly:
--    remove the safety-net default so any forgotten INSERT fails loudly
--    instead of silently landing in server 1.
-- ---------------------------------------------------------------
-- ALTER TABLE players   ALTER COLUMN server_id DROP DEFAULT;
-- ALTER TABLE arrests   ALTER COLUMN server_id DROP DEFAULT;
-- ALTER TABLE warrants  ALTER COLUMN server_id DROP DEFAULT;
-- ALTER TABLE audit_log ALTER COLUMN server_id DROP DEFAULT;
