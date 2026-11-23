package main

import (
	"fmt"
	"strings"

	"api/pkg/config"
)

// catalogDSN is the legacy catalog database, which canon shares in its own
// schema (D1); the variables are the ones migrate-catalog already reads.
func catalogDSN() string {
	return dsn(func(k, def string) string { return config.Env("KUN_CATALOG_"+k, config.Env("KUN_"+k, def)) },
		config.Env("KUN_CATALOG_PG_DATABASE", "kun_catalog"))
}

// idpDSN is the IdP database, where the permission console keeps its
// override rows.
func idpDSN() string {
	return dsn(func(k, def string) string { return config.Env("KUN_"+k, def) }, config.Env("KUN_PG_DATABASE", "kun_galgame_infra"))
}

func dsn(get func(key, def string) string, dbname string) string {
	parts := []string{
		"host=" + get("PG_HOST", "localhost"),
		"port=" + get("PG_PORT", "5432"),
		"user=" + get("PG_USER", "postgres"),
		"dbname=" + dbname,
		"sslmode=" + get("PG_SSLMODE", "disable"),
	}
	if pw := get("PG_PASSWORD", ""); pw != "" {
		parts = append(parts, fmt.Sprintf("password='%s'", strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(pw)))
	}
	return strings.Join(parts, " ")
}
