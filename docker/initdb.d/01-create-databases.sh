#!/bin/sh
# Runs once, on first Postgres init (empty data dir). The entrypoint already
# created POSTGRES_DB (kun_galgame_infra); create the other databases the
# services connect to. Schema itself is built by the migrate jobs, not here.
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-'EOSQL'
	CREATE DATABASE kun_images;
	CREATE DATABASE kun_artifacts;
	-- Cross-media catalog registry (identity + graph + credits), see cmd/migrate catalog.
	CREATE DATABASE kun_catalog;
	-- Community primitive (threads/posts), see cmd/migrate community. Added late
	-- (a historical gap): existing deployments already created it out of band, so
	-- this only affects a fresh local init.
	CREATE DATABASE kun_community;
	-- Direct messages shared by every site, see cmd/migrate chat.
	CREATE DATABASE kun_chat;
	-- Trust & Safety platform (reports + review inbox), see cmd/migrate trust.
	CREATE DATABASE kun_trust;
	-- AI-gateway semantic layer (usage ledger + budget fuse), see cmd/migrate ai.
	CREATE DATABASE kun_ai;
	-- Galgame news feed republished from partner sites, see cmd/migrate news.
	CREATE DATABASE kun_news;
	-- App telemetry (events, sessions, symbols, issues, alerts), see cmd/migrate telemetry.
	CREATE DATABASE kun_telemetry;
	-- Downstream repos' databases live on this shared server too (the hub owns
	-- the single Postgres). kungal → kungalgame, moyu → kungalgame_patch,
	-- sticker → kungalgame_sticker, letmoe → kun_letmoe.
	CREATE DATABASE kungalgame;
	CREATE DATABASE kungalgame_patch;
	CREATE DATABASE kungalgame_sticker;
	CREATE DATABASE kun_letmoe;
EOSQL
