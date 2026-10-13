package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	storesvc "api/internal/platform/store/service"

	"github.com/danielgtaylor/huma/v2"
)

type getPurchaseLinksInput struct {
	ProductID string `path:"product_id" maxLength:"10" pattern:"^(RJ|VJ)[0-9]{6,8}$" example:"RJ01234567" doc:"DLsite product number. RJ is doujin, VJ is commercial."`
}

type getPurchaseLinksOutput struct {
	CacheControl string `header:"Cache-Control" doc:"Always private: every link is keyed to the calling application, so a shared cache would hand one site's clicks to another."`
	Body         repr.StorePurchaseLinks
}

type getStoreStatsInput struct {
	From string `query:"from" maxLength:"10" doc:"First JST day to cover, YYYY-MM-DD. Optional; defaults to 29 days before to."`
	To   string `query:"to" maxLength:"10" doc:"Last JST day to cover, YYYY-MM-DD. Optional; defaults to today. from and to must be at most 92 days apart."`
}

type getStoreStatsOutput struct {
	CacheControl string `header:"Cache-Control" doc:"Always private: the numbers belong to the calling application alone."`
	Body         repr.StoreStats
}

func registerStore(api huma.API, cat *Catalog) {
	tags := []string{"store"}
	// 502 is minted only by storeErr's ErrShortenerDown arm, which only the
	// link-minting path can reach; getStoreStats reused this list and published
	// an error it can never answer.
	statsErrs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden,
		http.StatusUnprocessableEntity, http.StatusServiceUnavailable)
	errs := append(append([]int{}, statsErrs...), http.StatusBadGateway)
	huma.Register(api, huma.Operation{
		OperationID: "getStorePurchaseLinks", Method: http.MethodGet, Path: "/v2/store/purchase-links/{product_id}",
		// G4 wants a 404 on any path-parameter operation. Nothing here mints one:
		// product_id is a DLsite number this service never resolves against a
		// catalog, so an unknown-but-well-formed id still gets a link.
		Summary:     "Get purchase and coupon links for one product",
		Description: "Mints (or returns) the short links this application sends readers to. The purchase link belongs to your application alone — use it verbatim, because the click counter behind it is what settlement reads. coupon_url and campaign are present only while a campaign is running. Requires an application key with the store:read scope.",
		Tags:        tags, Errors: append(errs, http.StatusNotFound), SkipValidateParams: true,
	}, getStorePurchaseLinks(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getStoreStats", Method: http.MethodGet, Path: "/v2/store/stats",
		Summary:     "Click statistics for my links",
		Description: "Daily clicks on the links this application minted, over a JST-day range of at most 92 days. The bearer application is the subject — this replaces v1's /v1/store/me/stats. Requires an application key with the store:read scope.",
		Tags:        tags, Errors: statsErrs, SkipValidateParams: true,
	}, getStoreStats(cat))
	registerStorePrices(api, cat)
}

func getStorePurchaseLinks(cat *Catalog) func(context.Context, *getPurchaseLinksInput) (*getPurchaseLinksOutput, error) {
	return func(ctx context.Context, in *getPurchaseLinksInput) (*getPurchaseLinksOutput, error) {
		if in == nil {
			in = &getPurchaseLinksInput{}
		}
		svc, err := storeMinting(ctx, cat)
		if err != nil {
			return nil, err
		}
		clientID, cerr := requireAppClient(ctx)
		if cerr != nil {
			return nil, withIdent(ctx, cerr)
		}
		links, lerr := svc.PurchaseLinks(ctx, clientID, in.ProductID)
		if lerr != nil {
			return nil, storeErr(ctx, lerr)
		}
		return &getPurchaseLinksOutput{CacheControl: storeCachePrivate, Body: purchaseLinksFrom(links)}, nil
	}
}

func getStoreStats(cat *Catalog) func(context.Context, *getStoreStatsInput) (*getStoreStatsOutput, error) {
	return func(ctx context.Context, in *getStoreStatsInput) (*getStoreStatsOutput, error) {
		if in == nil {
			in = &getStoreStatsInput{}
		}
		svc, err := storeBound(ctx, cat)
		if err != nil {
			return nil, err
		}
		clientID, cerr := requireAppClient(ctx)
		if cerr != nil {
			return nil, withIdent(ctx, cerr)
		}
		from, to, rerr := storesvc.ResolveRange(time.Now(), in.From, in.To)
		if rerr != nil {
			p := problem.New(problem.CodeValidationFailed, "", "", storesvc.ErrInvalidRange.Error())
			p.Errors = []problem.FieldError{
				{Parameter: "from", Reason: problem.ReasonOutOfRange, Detail: "YYYY-MM-DD JST days, from <= to, at most 92 days apart"},
				{Parameter: "to", Reason: problem.ReasonOutOfRange, Detail: "YYYY-MM-DD JST days, from <= to, at most 92 days apart"},
			}
			return nil, withIdent(ctx, p)
		}
		stats, serr := svc.MyStats(ctx, clientID, from, to)
		if serr != nil {
			return nil, storeErr(ctx, serr)
		}
		return &getStoreStatsOutput{CacheControl: storeCachePrivate, Body: storeStatsFrom(stats)}, nil
	}
}

// Every response is keyed to the calling application; a shared cache holding one
// site's short links and serving them to another hands the clicks to the wrong
// site. Carried over verbatim from the v1 face.
const storeCachePrivate = "private"

func storeBound(ctx context.Context, cat *Catalog) (*storesvc.Service, error) {
	if cat == nil || cat.Store == nil {
		return nil, withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "",
			"the store link service is not configured."))
	}
	return cat.Store, nil
}

// Minting needs the shortener; reading stats does not — MyStats only touches the
// database. The first cut of this face gated BOTH ops on Configured(), which
// took the stats read away from any deployment without shortener credentials
// even though v1 answered it there. Review caught it before it shipped.
func storeMinting(ctx context.Context, cat *Catalog) (*storesvc.Service, error) {
	svc, err := storeBound(ctx, cat)
	if err != nil {
		return nil, err
	}
	if !svc.Configured() {
		return nil, withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "",
			"the store link service is not configured."))
	}
	return svc, nil
}

func storeErr(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, storesvc.ErrInvalidProductID):
		p := problem.New(problem.CodeValidationFailed, "", "", storesvc.ErrInvalidProductID.Error())
		p.Errors = []problem.FieldError{{Parameter: "product_id", Reason: problem.ReasonInvalidFormat,
			Detail: "expected ^(RJ|VJ)[0-9]{6,8}$"}}
		return withIdent(ctx, p)
	case errors.Is(err, storesvc.ErrQuotaExceeded):
		return withIdent(ctx, problem.New(problem.CodeStoreQuotaExceeded, "", "",
			"the application has minted the maximum number of purchase links."))
	case errors.Is(err, storesvc.ErrShortenerDown):
		return withIdent(ctx, problem.New(problem.CodeStoreLinkUnavailable, "", "",
			"the link shortener is unavailable; no link was issued."))
	case errors.Is(err, storesvc.ErrNotConfigured):
		return withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "",
			"the store link service is not configured."))
	}
	return catalogErr(ctx, err)
}

func purchaseLinksFrom(l *storesvc.PurchaseLinks) repr.StorePurchaseLinks {
	if l == nil {
		return repr.StorePurchaseLinks{Object: "purchase_links"}
	}
	out := repr.StorePurchaseLinks{
		Object: "purchase_links", ProductID: l.ProductID,
		PurchaseURL: l.PurchaseURL, CouponURL: l.CouponURL,
	}
	if l.Campaign != nil {
		out.Campaign = &repr.StoreCampaign{
			Object: "campaign", ID: repr.ID(l.Campaign.ID), Name: l.Campaign.Name,
		}
	}
	return out
}

func storeStatsFrom(s *storesvc.MyStats) repr.StoreStats {
	out := repr.StoreStats{Object: "store_stats", Rows: []repr.StoreStatRow{}, ByKind: []repr.StoreStatTotal{}}
	if s == nil {
		return out
	}
	out.FromDate, out.ToDate = s.From, s.To
	for _, r := range s.Rows {
		row := repr.StoreStatRow{
			Object: "store_stat", LinkKind: r.Kind, ProductID: r.ProductID,
			Date: r.Date, Total: int(r.Total), Uniques: int(r.Uniques),
		}
		if r.CampaignID != nil {
			id := strconv.FormatInt(*r.CampaignID, 10)
			row.CampaignID = &id
		}
		out.Rows = append(out.Rows, row)
	}
	out.Totals = storeTotalFrom(s.Totals)
	for _, t := range s.ByKind {
		out.ByKind = append(out.ByKind, storeTotalFrom(t))
	}
	return out
}

func storeTotalFrom(t storesvc.StatTotal) repr.StoreStatTotal {
	out := repr.StoreStatTotal{Total: int(t.Total), Uniques: int(t.Uniques)}
	if t.Kind != "" {
		kind := t.Kind
		out.LinkKind = &kind
	}
	return out
}
