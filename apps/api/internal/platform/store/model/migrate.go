package model

import (
	"fmt"

	"gorm.io/gorm"
)

// RekeyCouponsByOwner moves the reward-coupon tables from applications to
// developer accounts (2026-09-19, the day they shipped): a batch is now split
// among the owners of eligible applications, each owner's applications
// counted together. store_coupons loses client_id (AutoMigrate adds user_id);
// store_coupon_shares is dropped and recreated because its primary key changes
// from (batch_id, client_id) to (batch_id, user_id), which AutoMigrate never
// alters. Production held no published batch at the time, so nothing is
// converted; if a table does hold an application-keyed allocation this refuses
// instead of dropping it. Runs before AutoMigrate; a no-op once client_id is
// gone.
func RekeyCouponsByOwner(db *gorm.DB) error {
	err := db.Exec(`
		DO $$
		BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns
			            WHERE table_schema = current_schema()
			              AND table_name = 'store_coupons' AND column_name = 'client_id') THEN
				IF EXISTS (SELECT 1 FROM store_coupons WHERE client_id IS NOT NULL) THEN
					RAISE EXCEPTION 'store_coupons holds coupons assigned to applications; rekey them to owners by hand';
				END IF;
				ALTER TABLE store_coupons DROP COLUMN client_id;
			END IF;
			IF EXISTS (SELECT 1 FROM information_schema.columns
			            WHERE table_schema = current_schema()
			              AND table_name = 'store_coupon_shares' AND column_name = 'client_id') THEN
				IF EXISTS (SELECT 1 FROM store_coupon_shares) THEN
					RAISE EXCEPTION 'store_coupon_shares holds application-keyed shares; rekey them to owners by hand';
				END IF;
				DROP TABLE store_coupon_shares;
			END IF;
		END $$`).Error
	if err != nil {
		return fmt.Errorf("store migrate: rekey coupons by owner: %w", err)
	}
	return nil
}
