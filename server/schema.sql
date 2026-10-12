-- schema.sql: full multi-tenant schema for FRESH installs and tests.
-- Existing databases were upgraded with migrations/001_multi_tenant.sql;
-- every statement here is IF NOT EXISTS / INSERT IGNORE, so running it on
-- an already-migrated database changes nothing.
--
-- server_id has no DEFAULT here on purpose: a query that forgets it fails
-- loudly instead of silently landing in server 1.

CREATE TABLE IF NOT EXISTS servers (
  id                  INT AUTO_INCREMENT PRIMARY KEY,
  name                VARCHAR(100) NOT NULL,
  slug                VARCHAR(50)  NOT NULL,
  status              VARCHAR(20)  NOT NULL DEFAULT 'pending',
  api_key_hash        CHAR(64)     NULL,
  api_key_created_at  DATETIME     NULL,
  last_heartbeat_at   DATETIME     NULL,
  created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
                                   ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_servers_slug (slug),
  UNIQUE KEY uq_servers_api_key_hash (api_key_hash)
) ENGINE=InnoDB;

INSERT IGNORE INTO servers (id, name, slug, status)
VALUES (1, 'Default community', 'default', 'active');

CREATE TABLE IF NOT EXISTS players (
  id             INT AUTO_INCREMENT PRIMARY KEY,
  server_id      INT NOT NULL,
  name           VARCHAR(255) NOT NULL,
  ping           INT NOT NULL,
  status         VARCHAR(255) NOT NULL,
  playtime_hours INT NOT NULL,
  vehicle        VARCHAR(255) NOT NULL,
  last_seen      VARCHAR(255) NOT NULL,
  UNIQUE KEY uq_players_id_server (id, server_id),
  KEY idx_players_server_name (server_id, name),
  CONSTRAINT fk_players_server FOREIGN KEY (server_id) REFERENCES servers(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS arrests (
  id        INT AUTO_INCREMENT PRIMARY KEY,
  server_id INT NOT NULL,
  player_id INT NOT NULL,
  charge    VARCHAR(255) NOT NULL,
  officer   VARCHAR(255) NOT NULL,
  date      VARCHAR(255) NOT NULL,
  status    VARCHAR(255) NOT NULL,
  KEY idx_arrests_server (server_id),
  KEY idx_arrests_player_server (player_id, server_id),
  CONSTRAINT fk_arrests_player FOREIGN KEY (player_id, server_id) REFERENCES players(id, server_id),
  CONSTRAINT fk_arrests_server FOREIGN KEY (server_id) REFERENCES servers(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS warrants (
  id          INT AUTO_INCREMENT PRIMARY KEY,
  server_id   INT NOT NULL,
  player_id   INT NOT NULL,
  reason      VARCHAR(255) NOT NULL,
  issued_by   VARCHAR(255) NOT NULL,
  date_issued VARCHAR(255) NOT NULL,
  active      TINYINT(1) NOT NULL,
  KEY idx_warrants_server_active (server_id, active),
  KEY idx_warrants_player_server (player_id, server_id),
  CONSTRAINT fk_warrants_player FOREIGN KEY (player_id, server_id) REFERENCES players(id, server_id),
  CONSTRAINT fk_warrants_server FOREIGN KEY (server_id) REFERENCES servers(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS audit_log (
  id        INT AUTO_INCREMENT PRIMARY KEY,
  server_id INT NOT NULL,
  timestamp VARCHAR(255) NOT NULL,
  role      VARCHAR(255) NOT NULL,
  actor     VARCHAR(100) NOT NULL DEFAULT '',
  method    VARCHAR(255) NOT NULL,
  path      VARCHAR(255) NOT NULL,
  ip        VARCHAR(255) NOT NULL,
  KEY idx_audit_server_id (server_id, id),
  CONSTRAINT fk_audit_server FOREIGN KEY (server_id) REFERENCES servers(id)
) ENGINE=InnoDB;

-- Accounts: who someone is (users) and what they may do on each server
-- (memberships). A Discord sign-in creates a user with NO membership;
-- a staff member grants a role afterwards.
CREATE TABLE IF NOT EXISTS users (
  id            INT AUTO_INCREMENT PRIMARY KEY,
  discord_id    VARCHAR(32)  NOT NULL,
  username      VARCHAR(100) NOT NULL,
  display_name  VARCHAR(100) NOT NULL,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_login_at DATETIME NULL,
  UNIQUE KEY uq_users_discord_id (discord_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS memberships (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  user_id    INT NOT NULL,
  server_id  INT NOT NULL,
  role       VARCHAR(32) NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_memberships_user_server (user_id, server_id),
  KEY idx_memberships_server (server_id),
  CONSTRAINT fk_memberships_user   FOREIGN KEY (user_id)   REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_memberships_server FOREIGN KEY (server_id) REFERENCES servers(id)
) ENGINE=InnoDB;
