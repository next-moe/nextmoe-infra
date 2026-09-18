package handler

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/settings/keys"
	"api/internal/platform/store/price"

	"github.com/danielgtaylor/huma/v2"
)

const pricesStaleDoc = "Quotes are cached observations; read fetched_at / expires_at / stale. pending means a fetch is in flight, retry shortly."

type getStoreWorkPricesInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Decimal catalog work id."`
}

type getStoreWorkPricesOutput struct {
	Body repr.WorkPrices
}

type listStorePricesInput struct {
	IDs string `query:"ids" required:"true" maxLength:"2100" doc:"Comma-separated decimal catalog work ids, at most 100. Duplicates are collapsed. Unknown ids are listed in missing[]."`
}

type listStorePricesOutput struct {
	Body repr.List[repr.WorkPrices]
}

func registerStorePrices(api huma.API, cat *Catalog) {
	tags := []string{"store"}
	huma.Register(api, huma.Operation{
		OperationID: "getStoreWorkPrices", Method: http.MethodGet, Path: "/v2/store/prices/{id}",
		Summary:     "Get storefront prices for one work",
		Description: "Cached storefront price quotes for one catalog work. Unauthenticated. " + pricesStaleDoc,
		Tags:        tags, Errors: collectionErrors(http.StatusNotFound, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getStoreWorkPrices(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listStorePrices", Method: http.MethodGet, Path: "/v2/store/prices",
		Summary:     "Get storefront prices for many works",
		Description: "Cached storefront price quotes for up to 100 catalog works. Unauthenticated. " + pricesStaleDoc + " The batch lane never waits on a cold miss.",
		Tags:        tags, Errors: collectionErrors(http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listStorePrices(cat))
}

func pricesBound(ctx context.Context, cat *Catalog) (*price.Service, error) {
	if !keys.StorePriceEnabled.Get() {
		return nil, withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "the store price face is disabled."))
	}
	if cat == nil || cat.Prices == nil {
		return nil, withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "",
			"the store price service is not configured."))
	}
	return cat.Prices, nil
}

func getStoreWorkPrices(cat *Catalog) func(context.Context, *getStoreWorkPricesInput) (*getStoreWorkPricesOutput, error) {
	return func(ctx context.Context, in *getStoreWorkPricesInput) (*getStoreWorkPricesOutput, error) {
		if in == nil {
			in = &getStoreWorkPricesInput{}
		}
		svc, err := pricesBound(ctx, cat)
		if err != nil {
			return nil, err
		}
		id, perr := strconv.ParseInt(in.ID, 10, 64)
		if perr != nil || id <= 0 {
			p := problem.New(problem.CodeInvalidParameter, "", "", "id must be a positive decimal catalog id.")
			p.Errors = []problem.FieldError{{Parameter: "id", Reason: problem.ReasonInvalidFormat, Detail: in.ID}}
			return nil, withIdent(ctx, p)
		}
		anchors, visible, aerr := cat.Public.StoreAnchorsFor(ctx, []int64{id})
		if aerr != nil {
			return nil, catalogErr(ctx, aerr)
		}
		if !visible[id] {
			return nil, cat.missWork(ctx, id)
		}
		quotes, qerr := svc.Quotes(ctx, toPriceAnchors(anchors[id]), time.Duration(keys.StorePriceWaitOnMissMs.Get())*time.Millisecond)
		if qerr != nil {
			return nil, catalogErr(ctx, qerr)
		}
		return &getStoreWorkPricesOutput{Body: workPricesFrom(id, anchors[id], quotes)}, nil
	}
}

func listStorePrices(cat *Catalog) func(context.Context, *listStorePricesInput) (*listStorePricesOutput, error) {
	return func(ctx context.Context, in *listStorePricesInput) (*listStorePricesOutput, error) {
		if in == nil {
			in = &listStorePricesInput{}
		}
		svc, err := pricesBound(ctx, cat)
		if err != nil {
			return nil, err
		}
		ids, perr := parsePriceIDs(in.IDs)
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		byWork, visible, aerr := cat.Public.StoreAnchorsFor(ctx, ids)
		if aerr != nil {
			return nil, catalogErr(ctx, aerr)
		}
		var all []price.Anchor
		var missing []string
		var order []int64
		for _, id := range ids {
			if !visible[id] {
				missing = append(missing, repr.ID(id))
				continue
			}
			order = append(order, id)
			all = append(all, toPriceAnchors(byWork[id])...)
		}
		quotes, qerr := svc.Quotes(ctx, all, 0)
		if qerr != nil {
			return nil, catalogErr(ctx, qerr)
		}
		items := make([]repr.WorkPrices, 0, len(order))
		for _, id := range order {
			items = append(items, workPricesFrom(id, byWork[id], quotes))
		}
		out := repr.NewList(items, nil)
		if missing == nil {
			missing = []string{}
		}
		out.Missing = &missing
		return &listStorePricesOutput{Body: out}, nil
	}
}

func parsePriceIDs(raw string) ([]int64, *problem.Problem) {
	var tokens []string
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		tokens = append(tokens, t)
	}
	if len(tokens) == 0 {
		p := problem.New(problem.CodeInvalidParameter, "", "", "ids= is required.")
		p.Errors = []problem.FieldError{{
			Parameter: "ids", Reason: problem.ReasonInvalidFormat,
			Detail: "comma-separated decimal catalog ids, at most 100",
		}}
		return nil, p
	}
	if len(tokens) > collect.MaxBatchItems {
		p := problem.New(problem.CodeTooManyIDs, "", "", "ids= accepts at most 100 values.")
		p.Errors = []problem.FieldError{{
			Parameter: "ids", Reason: problem.ReasonTooManyItems, Detail: "maximum 100",
			Params: &problem.FieldParams{MaxItems: problem.Ptr(collect.MaxBatchItems)},
		}}
		return nil, p
	}
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(tokens))
	for _, t := range tokens {
		id, ok := repr.ParseID(t)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "ids= values must be decimal catalog ids.")
			p.Errors = []problem.FieldError{{Parameter: "ids", Reason: problem.ReasonInvalidFormat, Detail: t}}
			return nil, p
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func toPriceAnchors(in []catsvc.StoreAnchor) []price.Anchor {
	out := make([]price.Anchor, 0, len(in))
	for _, a := range in {
		out = append(out, price.Anchor{Source: a.Source, ExternalID: a.ExternalID})
	}
	return out
}

func workPricesFrom(workID int64, anchors []catsvc.StoreAnchor, quotes []price.Quote) repr.WorkPrices {
	out := repr.WorkPrices{Object: "work_prices", WorkID: repr.ID(workID), Quotes: []repr.PriceQuote{}}
	for _, a := range anchors {
		for _, q := range quotes {
			if q.Key.Source == a.Source && q.Key.ExternalID == a.ExternalID {
				out.Quotes = append(out.Quotes, priceQuoteFrom(q))
			}
		}
	}
	return out
}

func priceQuoteFrom(q price.Quote) repr.PriceQuote {
	out := repr.PriceQuote{
		Object:          "price_quote",
		Source:          q.Key.Source,
		ExternalID:      q.Key.ExternalID,
		Region:          q.Key.Region,
		QuoteState:      q.State,
		URL:             q.URL,
		DiscountPercent: q.DiscountPercent,
		Converted:       moneyList(q.Converted),
		Stale:           q.Stale,
	}
	if q.State == "priced" {
		out.List = &repr.Money{Currency: q.Currency, Amount: price.FormatMinor(q.ListMinor)}
		out.Current = &repr.Money{Currency: q.Currency, Amount: price.FormatMinor(q.CurrentMinor)}
	}
	out.SaleEndsAt = rfc3339Ptr(q.SaleEndsAt)
	out.FetchedAt = rfc3339Ptr(q.FetchedAt)
	out.ExpiresAt = rfc3339Ptr(q.ExpiresAt)
	return out
}

func moneyList(m map[string]int64) []repr.Money {
	if len(m) == 0 {
		return []repr.Money{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]repr.Money, 0, len(keys))
	for _, k := range keys {
		out = append(out, repr.Money{Currency: k, Amount: price.FormatMinor(m[k])})
	}
	return out
}

func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format("2006-01-02T15:04:05Z")
	return &s
}
