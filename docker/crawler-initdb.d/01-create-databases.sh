#!/bin/sh
# Runs once, on first Postgres init (empty data dir). The entrypoint already
# created POSTGRES_DB (dlsite); create the other three staging databases.
# Schema itself is built by each crawler's `migrate` phase, not here.
#
# A database added to this file later will NOT appear on a server that has
# already initialised — initdb only runs against an empty data dir. Create it by
# hand then, the way kun_news had to be on the prod server.
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-'EOSQL'
	CREATE DATABASE erogamescape;
	CREATE DATABASE getchu;
	CREATE DATABASE howlongtobeat;
EOSQL
