package main

import siteModel "api/internal/platform/site/model"

// The v2 editing cap keys on this, and keying it on owner_user_id alone froze
// the whole editing pipeline: 47 open proposals and not one merge between
// 2026-08-28 and 2026-09-08. owner_user_id means only "the developer portal
// owns this app", and the assumption that a site-bound client therefore has it
// NULL is false in production — the forum, patch, sticker and LetMoe clients
// all carry owner_user_id = 2, so all four first-party sites were capped and
// AllowsReview/AllowsAutomerge returned false before reading any permission,
// admins included. catalog_site is the durable marker instead: devapi never
// writes it, so an app registered through the portal cannot have one.
func isThirdPartyEditClient(cl *siteModel.OAuthClient) bool {
	return cl != nil && cl.CatalogSite == "" && cl.OwnerUserID != nil
}
