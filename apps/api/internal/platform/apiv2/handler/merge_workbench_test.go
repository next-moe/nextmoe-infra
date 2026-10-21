package handler

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"
)

// The editor labels every kind the spec enum lists; this keeps the enum
// covering every table a merge can record.
func TestMovementKeysCoverEveryRecordedTable(t *testing.T) {
	field, _ := reflect.TypeFor[repr.MergeMovedCount]().FieldByName("Key")
	enum := strings.Split(field.Tag.Get("enum"), ",")
	for _, table := range catsvc.MovementTables() {
		key := catsvc.MovementKey(table)
		if !slices.Contains(enum, key) {
			t.Errorf("table %s records as %q, which MergeMovedCount.Key's enum does not list", table, key)
		}
	}
}
