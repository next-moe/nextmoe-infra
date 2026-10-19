package editspec

import (
	"api/internal/platform/catalog/perm"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

// RegisterAll is the single list of editable catalog entity types. It exists
// because there were two: cmd/catalog wired one and MergeService built its own
// for IdentityFollowStmts, so adding a type to one of them would have left every
// suppression on the other silently unfollowed by the next merge.
func RegisterAll(reg *editing.Registry, db *gorm.DB) error {
	reg.SetPermissionValidator(perm.Known)
	for _, register := range []func(*editing.Registry, *gorm.DB) error{
		RegisterWork, RegisterTaxonomy, RegisterCharacter, RegisterRelease,
	} {
		if err := register(reg, db); err != nil {
			return err
		}
	}
	return nil
}
