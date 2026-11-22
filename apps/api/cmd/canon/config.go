package main

import (
	"fmt"
	"strings"

	"api/pkg/config"
)

// catalogDSN is the legacy catalog database, which canon shares in its own
// schema (D1); the variables are the ones migrate-catalog already reads.
func catalogDSN() string {
	get := func(k, fallback, def string) string { return config.Env(k, config.Env(fallback, def)) }
	parts := []string{
		"host=" + get("KUN_CATALOG_PG_HOST", "KUN_PG_HOST", "localhost"),
		"port=" + get("KUN_CATALOG_PG_PORT", "KUN_PG_PORT", "5432"),
		"user=" + get("KUN_CATALOG_PG_USER", "KUN_PG_USER", "postgres"),
		"dbname=" + config.Env("KUN_CATALOG_PG_DATABASE", "kun_catalog"),
		"sslmode=" + get("KUN_CATALOG_PG_SSLMODE", "KUN_PG_SSLMODE", "disable"),
	}
	if pw := get("KUN_CATALOG_PG_PASSWORD", "KUN_PG_PASSWORD", ""); pw != "" {
		parts = append(parts, fmt.Sprintf("password='%s'", strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(pw)))
	}
	return strings.Join(parts, " ")
}
