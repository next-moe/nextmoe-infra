package repr

import "time"

type ListRanking struct {
	List[RankingEntry]
	By        string      `json:"by" enum:"consensus,vndb,bangumi,dlsite,erogamescape,nextmoe,howlongtobeat" doc:"What the list ranks by, as by= asked."`
	Metric    string      `json:"metric" enum:"lower,upper,mean,median,native,percentile,spread,votes,sources,divergence" doc:"The statistic ranked on, as metric= asked or defaulted."`
	Direction string      `json:"direction" enum:"desc,asc" doc:"The order= in effect. desc puts the highest value first."`
	Unit      string      `json:"unit" enum:"deviation,percentile,native,votes,count,points" doc:"How to read each entry's value. deviation: a 偏差値, 50 is the typical work rated by two or more sources and every 10 points is one standard deviation. percentile: 0-100, the share of rated works below. native: the source's own scale (model.sources[].scale_min/scale_max). votes: a vote count. count: a number of sources. points: percentile points between the sources that place the work highest and lowest."`
	Model     RatingModel `json:"model" doc:"The fitted model every derived number on this page comes from."`
}

type RankingEntry struct {
	_         struct{}          `json:"-" additionalProperties:"true"`
	Object    string            `json:"object" enum:"ranking_entry" doc:"Type discriminant. Always ranking_entry."`
	Rank      int               `json:"rank" minimum:"1" doc:"1-based position in the whole ranking, not in the page. Equal values keep distinct ranks, ordered by popularity then id."`
	Value     float64           `json:"value" minimum:"0" doc:"The value ranked on, in the list's unit."`
	Work      Work              `json:"work" doc:"The ranked work, shaped by view= and include= as on GET /v2/catalog/works."`
	Consensus *RankingConsensus `json:"consensus" doc:"The cross-source consensus, over sources= when given. null when none of those sources rated the work."`
	PerSource []RankingSource   `json:"per_source" doc:"Every source that rated the work, in a fixed source order. Never null."`
}

type RankingConsensus struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Score       float64  `json:"score" minimum:"0" maximum:"100" doc:"Deviation score (偏差値) of the consensus: 50 is the typical work rated by two or more sources, 60 one standard deviation above it."`
	Lower       float64  `json:"lower" minimum:"0" maximum:"100" doc:"Lower end of the 90% interval of score. Few votes or one source widen the interval; ranking by it puts works that are good with confidence first."`
	Upper       float64  `json:"upper" minimum:"0" maximum:"100" doc:"Upper end of the 90% interval of score."`
	Percentile  float64  `json:"percentile" minimum:"0" maximum:"100" doc:"Share of all works with a consensus that score lower, in percent."`
	SourceCount int      `json:"source_count" minimum:"1" doc:"Sources that contributed."`
	VoteCount   int      `json:"vote_count" minimum:"0" doc:"Votes across the contributing sources."`
	Divergence  *float64 `json:"divergence" minimum:"0" maximum:"100" doc:"How far apart the sources place the work, in percentile points between the highest and the lowest; a source counts once it has four times its prior_votes in votes, so the estimate is mostly the work's own. null when fewer than two do."`
}

type RankingSource struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Source     string   `json:"source" maxLength:"64" doc:"Source key."`
	Score      float64  `json:"score" minimum:"0" doc:"The source's own published score, unchanged: vndb's Bayesian rating, bangumi's and dlsite's mean, erogamescape's median, nextmoe's smoothed mean, howlongtobeat's review score."`
	VoteCount  int      `json:"vote_count" minimum:"0" doc:"Votes behind score."`
	Mean       float64  `json:"mean" minimum:"0" doc:"Estimated true mean on the source's scale: the work's own mean pulled toward what works with as many votes usually score, by prior_votes. Few votes pull hard; many barely move it."`
	Lower      float64  `json:"lower" minimum:"0" doc:"Lower end of the 90% interval of mean."`
	Upper      float64  `json:"upper" minimum:"0" doc:"Upper end of the 90% interval of mean."`
	Median     *float64 `json:"median" minimum:"0" doc:"Median vote with the same pull applied; erogamescape's starts from its published median. null when the source publishes no histogram."`
	Spread     *float64 `json:"spread" minimum:"0" doc:"Standard deviation of the votes, on the source's scale. null below two votes or when the source publishes no spread or histogram."`
	Percentile float64  `json:"percentile" minimum:"0" maximum:"100" doc:"Share of the works this source rated whose mean is lower, in percent."`
}

type RatingModel struct {
	_        struct{}            `json:"-" additionalProperties:"true"`
	Object   string              `json:"object" enum:"rating_model" doc:"Type discriminant. Always rating_model."`
	ID       string              `json:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Fit id. Changes when the model is refitted, nightly."`
	Version  int                 `json:"version" minimum:"1" doc:"Method version. A new version can change every derived number."`
	FittedAt time.Time           `json:"fitted_at" maxLength:"40" doc:"When the model was fitted."`
	Interval float64             `json:"interval" minimum:"0" maximum:"1" doc:"Coverage of every lower/upper pair. Always 0.9."`
	Sources  []RatingModelSource `json:"sources" doc:"Per-source parameters, in the fixed source order."`
}

type RatingModelSource struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Source      string   `json:"source" maxLength:"64" doc:"Source key."`
	ScaleMin    float64  `json:"scale_min" minimum:"0" doc:"Lowest score the source's scale allows."`
	ScaleMax    float64  `json:"scale_max" minimum:"0" doc:"Highest score the source's scale allows."`
	PriorVotes  float64  `json:"prior_votes" minimum:"0" doc:"How many votes the prior is worth: a work with this many votes has its mean pulled halfway. Estimated from the data."`
	Works       int      `json:"works" minimum:"0" doc:"Works this source rated."`
	InConsensus bool     `json:"in_consensus" doc:"Whether this source contributes to the consensus. A source needs enough works shared with others to be calibrated."`
}
