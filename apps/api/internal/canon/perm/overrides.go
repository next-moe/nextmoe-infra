package perm

import (
	"context"
	"database/sql"
	"log/slog"
	"slices"
	"time"

	"api/internal/platform/authz"
)

type Override struct {
	Role, Permission, Effect string
}

// Refresh folds the console's override rows for canon keys into Holder. The
// rows live in the IdP database's role_permission_overrides, where every
// service reads its own domain's rows.
func Refresh(ctx context.Context, idp *sql.DB) error {
	rows, err := idp.QueryContext(ctx, `SELECT role, permission, effect FROM role_permission_overrides WHERE permission LIKE 'canon.%'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var all []Override
	for rows.Next() {
		var o Override
		if err := rows.Scan(&o.Role, &o.Permission, &o.Effect); err != nil {
			return err
		}
		all = append(all, o)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	Holder.Swap(authz.NewResolver(Merged(Bundles, all)))
	return nil
}

// Merged is the platform's overlay rule, kept in step with
// permissions.Merge: grants first, then denies; ren is never denied, and a
// row naming a key outside canon does nothing.
func Merged(base authz.Bundles, rows []Override) authz.Bundles {
	out := make(authz.Bundles, len(base))
	for role, ps := range base {
		out[role] = slices.Clone(ps)
	}
	for _, o := range rows {
		p := authz.Permission(o.Permission)
		if o.Effect == "grant" && slices.Contains(All, p) && !slices.Contains(out[o.Role], p) {
			out[o.Role] = append(out[o.Role], p)
		}
	}
	for _, o := range rows {
		p := authz.Permission(o.Permission)
		if o.Effect == "deny" && o.Role != "ren" && slices.Contains(All, p) {
			out[o.Role] = slices.DeleteFunc(slices.Clone(out[o.Role]), func(q authz.Permission) bool { return q == p })
		}
	}
	return out
}

// Poll refreshes now and then every interval until ctx ends; a failed
// refresh keeps the last set.
func Poll(ctx context.Context, idp *sql.DB, every time.Duration) {
	refresh := func() {
		if err := Refresh(ctx, idp); err != nil && ctx.Err() == nil {
			slog.Warn("canon permissions: override refresh failed; keeping the last set", "err", err)
		}
	}
	refresh()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			refresh()
		}
	}
}
