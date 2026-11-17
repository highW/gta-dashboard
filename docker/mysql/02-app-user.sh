#!/bin/bash
# Runs once, when the MySQL container starts on an empty data volume (after
# 01-schema.sql has created the tables). Creates the limited user the API
# connects as: it can read and write rows, but not change or drop tables.
#
# The MySQL image may source this file instead of running it, so it does
# not change shell options or call exit.

if [ -z "${MYSQL_APP_PASSWORD:-}" ]; then
    echo >&2 "02-app-user.sh: MYSQL_APP_PASSWORD is not set"
    false
else
    # Escape for a quoted SQL string: backslashes first, then single quotes.
    app_pw=${MYSQL_APP_PASSWORD//\\/\\\\}
    app_pw=${app_pw//\'/\'\'}

    mysql --protocol=socket -uroot -p"${MYSQL_ROOT_PASSWORD}" <<SQL
CREATE USER IF NOT EXISTS 'dashboard_app'@'%' IDENTIFIED BY '${app_pw}';
GRANT SELECT, INSERT, UPDATE, DELETE ON \`${MYSQL_DATABASE}\`.* TO 'dashboard_app'@'%';
SQL
    echo "02-app-user.sh: created user dashboard_app with row access to ${MYSQL_DATABASE}"
    unset app_pw
fi
