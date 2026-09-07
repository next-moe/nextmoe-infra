package config

import (
	"reflect"
	"testing"
)

func TestEveryDatabaseConfigIsBounded(t *testing.T) {
	t.Setenv("KUN_PG_PASSWORD", "test")
	t.Setenv("JWT_SECRET", "test")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	v := reflect.ValueOf(*cfg)
	dbType := reflect.TypeFor[DatabaseConfig]()
	seen := 0
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if f.Type != dbType {
			continue
		}
		seen++
		pool := v.Field(i).Interface().(DatabaseConfig).Pool
		if pool.MaxOpen <= 0 {
			t.Errorf("%s: MaxOpen is %d — an unbounded pool can take every slot on the server",
				f.Name, pool.MaxOpen)
		}
		if pool.MaxIdle <= 0 {
			t.Errorf("%s: MaxIdle is %d — connections closed on release churn through TIME_WAIT",
				f.Name, pool.MaxIdle)
		}
		if pool.MaxLifetime <= 0 || pool.MaxIdleTime <= 0 {
			t.Errorf("%s: lifetime=%v idle=%v — both must be finite so slots come back",
				f.Name, pool.MaxLifetime, pool.MaxIdleTime)
		}
	}
	if seen == 0 {
		t.Fatal("no DatabaseConfig fields found — the walk broke, not the config")
	}
}

func TestPoolBoundsAreOverridable(t *testing.T) {
	t.Setenv("KUN_PG_MAX_OPEN_CONNS", "3")
	t.Setenv("KUN_PG_MAX_IDLE_CONNS", "1")
	t.Setenv("KUN_PG_CONN_MAX_LIFETIME_SECONDS", "60")
	t.Setenv("KUN_PG_CONN_MAX_IDLE_SECONDS", "30")

	got := loadPoolConfig()
	if got.MaxOpen != 3 || got.MaxIdle != 1 {
		t.Fatalf("env ignored: %+v", got)
	}
	if got.MaxLifetime.Seconds() != 60 || got.MaxIdleTime.Seconds() != 30 {
		t.Fatalf("durations ignored: %+v", got)
	}
}

// MaxIdle below MaxOpen makes database/sql destroy a connection on release and
// reopen it on the next checkout; catalog did ~1 reconnect/second in production
// on the old 10/5 default. Both halves matter: the bare default, and the default
// after someone widens MaxOpen alone.
func TestIdleSlotsCoverTheWholePool(t *testing.T) {
	if got := loadPoolConfig(); got.MaxIdle != got.MaxOpen {
		t.Fatalf("default pool churns: MaxOpen=%d MaxIdle=%d", got.MaxOpen, got.MaxIdle)
	}
	t.Setenv("KUN_PG_MAX_OPEN_CONNS", "40")
	got := loadPoolConfig()
	if got.MaxOpen != 40 || got.MaxIdle != 40 {
		t.Fatalf("widening MaxOpen alone reopened the gap: %+v", got)
	}
}
