-- 002_users_memberships.sql (MySQL 8). Also appended to schema.sql, which creates
-- these tables on the next backend start; kept here as the change record.

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
