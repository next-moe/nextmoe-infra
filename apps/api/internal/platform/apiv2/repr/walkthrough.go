package repr

type Walkthrough struct {
	_                struct{}                 `json:"-" additionalProperties:"true"`
	Object           string                   `json:"object" enum:"walkthrough" doc:"Type discriminant. Always walkthrough."`
	ID               string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog walkthrough id."`
	WorkID           string                   `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work this walkthrough plays through."`
	WorkContentLimit string                   `json:"work_content_limit" enum:"sfw,nsfw" doc:"content_limit of the work, the display axis GET /v2/catalog/works/{id} reports and the content_limit filter selects on. Independent of nsfw, which gates only on content_rating: most r18 games are sfw here. A reader who has not opted into adult content should be shown this walkthrough only when it is sfw."`
	WorkRouteHints   bool                     `json:"work_route_hints" doc:"true when the game shows route hints itself. A reader may prefer them to this walkthrough, so a site should say so before showing the routes."`
	Source           string                   `json:"source" maxLength:"64" doc:"Open vocabulary sources: the site whose walkthrough page the choice graph was derived from, or nextmoe for a walkthrough written on this platform. Must not be used as a discriminant."`
	SourceURL        *string                  `json:"source_url" format:"uri" maxLength:"2048" doc:"The original walkthrough page. Only its choice graph is carried here, never its prose; link to it for attribution. null for a walkthrough written on this platform (source nextmoe), which has no page."`
	SourceAuthor     *string                  `json:"source_author" maxLength:"256" doc:"Author the source page credits. null if it credits none. Must not be used as a discriminant."`
	SourceUpdatedOn  *string                  `json:"source_updated_on" format:"date" maxLength:"10" doc:"Date the source page says it was last updated. null if it does not say."`
	Lang             string                   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language of every text field in the chart: ja for an imported walkthrough, ja or zh-Hans for one written on this platform. Translations sit in localized maps beside them; a zh-Hans chart has no zh-Hans entries there."`
	Release          *WalkthroughRelease      `json:"release" doc:"The release this walkthrough follows, such as an official or fan translation whose choice texts differ from the original. null when it follows no particular release."`
	Characters       []WalkthroughCharacter   `json:"characters" doc:"Characters the chart routes to, in chart order. Empty array, never null."`
	EndingCount      int                      `json:"ending_count" minimum:"0" doc:"Endings in the chart."`
	SaveCount        int                      `json:"save_count" minimum:"0" doc:"Save points in the chart."`
	SegmentCount     int                      `json:"segment_count" minimum:"0" doc:"Segments in the chart."`
	UpdatedAt        string                   `json:"updated_at" format:"date-time" maxLength:"32" doc:"Last time this walkthrough row changed."`
	Chart            *WalkthroughChart        `json:"chart,omitempty" doc:"The choice graph. Present on GET /v2/catalog/walkthroughs/{id} only; absent on the collection."`
	ImportReview     *WalkthroughImportReview `json:"import_review,omitempty" doc:"What the import found missing from this chart. Present on GET /v2/me/walkthrough-backlog only."`
}

type WalkthroughImportReview struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Missed         []string `json:"missed" doc:"Choice texts on the source page that the chart does not carry, as the page writes them. Empty array, never null."`
	EndMarkers     int      `json:"end_markers" minimum:"0" doc:"Endings the source page marks."`
	EndingsReached int      `json:"endings_reached" minimum:"0" doc:"Segments of the chart that end at an ending. Fewer than end_markers means routes are missing."`
	Coverage       float64  `json:"coverage" minimum:"0" maximum:"1" doc:"Share of the page's choice lines the chart carries."`
}

type WalkthroughCharacter struct {
	_           struct{}                 `json:"-" additionalProperties:"true"`
	Key         string                   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local key that sections[].character_keys and endings[].character_keys refer to."`
	Name        string                   `json:"name" maxLength:"512" doc:"The name as the source page writes it. Must not be used as a discriminant."`
	CharacterID *string                  `json:"character_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog character this name is pinned to. null if the pin is gone."`
	DisplayName *string                  `json:"display_name" maxLength:"512" doc:"display_name of the pinned catalog character. null when character_id is. Must not be used as a discriminant."`
	Localized   map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: the pinned catalog character's preferred names. Empty object if none or unpinned. Must not be used as a discriminant."`
}

type WalkthroughChart struct {
	_        struct{}             `json:"-" additionalProperties:"true"`
	Sections []WalkthroughSection `json:"sections" doc:"Headings of the source page that group segments, usually one per route. Empty array, never null."`
	Entries  []WalkthroughEntry   `json:"entries" doc:"Places a playthrough starts from: the title screen, or a later unlock of it. Empty array, never null."`
	Saves    []WalkthroughSave    `json:"saves" doc:"Save points the chart creates and resumes from. Empty array, never null."`
	Endings  []WalkthroughEnding  `json:"endings" doc:"Endings, one per occurrence in the source: repeated names are separate endings. Empty array, never null."`
	Segments []WalkthroughSegment `json:"segments" doc:"Runs of steps from one node to the next, in source order. Empty array, never null."`
}

type WalkthroughAlternative struct {
	_         struct{}                 `json:"-" additionalProperties:"true"`
	Text      string                   `json:"text" maxLength:"1024" doc:"The alternative choice as the source page writes it, in the walkthrough's lang. Must not be used as a discriminant."`
	Localized map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: machine translations of text. Personal names without a sourced catalog translation stay in the original script. Empty object if none. Must not be used as a discriminant."`
}

type WalkthroughSection struct {
	_             struct{}                 `json:"-" additionalProperties:"true"`
	Key           string                   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local key that segments[].section_key refers to."`
	Title         string                   `json:"title" maxLength:"512" doc:"The heading as the source page writes it. Must not be used as a discriminant."`
	Localized     map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: machine translations of title. Personal names without a sourced catalog translation stay in the original script. Empty object if none. Must not be used as a discriminant."`
	CharacterKeys []string                 `json:"character_keys" doc:"characters[].key of the characters this heading is about. Empty array, never null."`
}

type WalkthroughEntry struct {
	_                   struct{}                 `json:"-" additionalProperties:"true"`
	Key                 string                   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local key that a segment's from node refers to."`
	Label               string                   `json:"label" maxLength:"256" doc:"The entry as the source page names it, such as START or RESTART. Must not be used as a discriminant."`
	Localized           map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: machine translations of label. Personal names without a sourced catalog translation stay in the original script. Empty object if none. Must not be used as a discriminant."`
	UnlockedByEndingKey *string                  `json:"unlocked_by_ending_key" pattern:"^[A-Za-z0-9_]+$" maxLength:"32" doc:"endings[].key whose completion unlocks this entry. null if it is open from the start."`
}

type WalkthroughSave struct {
	_         struct{}                 `json:"-" additionalProperties:"true"`
	Key       string                   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local key that save steps and save nodes refer to."`
	Label     string                   `json:"label" maxLength:"256" doc:"The save point as the source page labels it. Must not be used as a discriminant."`
	Localized map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: machine translations of label, such as 存档12 for セーブ１２. Empty object if none. Must not be used as a discriminant."`
}

type WalkthroughEnding struct {
	_             struct{}                 `json:"-" additionalProperties:"true"`
	Key           string                   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local key that ending nodes refer to."`
	Name          string                   `json:"name" maxLength:"512" doc:"The ending's name as the source page writes it. Must not be used as a discriminant."`
	Localized     map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: machine translations of name. Personal names without a sourced catalog translation stay in the original script. Empty object if none. Must not be used as a discriminant."`
	EndingType    string                   `json:"ending_type" enum:"heroine,harem,true,normal,bad,after,other" doc:"heroine concludes one character's route, harem several; after is an after-story reached from another ending."`
	CharacterKeys []string                 `json:"character_keys" doc:"characters[].key of the characters whose route this ending concludes. Empty array, never null."`
}

type WalkthroughSegment struct {
	_                  struct{}                 `json:"-" additionalProperties:"true"`
	Key                string                   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local segment key."`
	SectionKey         *string                  `json:"section_key" pattern:"^[A-Za-z0-9_]+$" maxLength:"32" doc:"sections[].key this segment is listed under. null if the source lists it under no heading."`
	FromNode           WalkthroughNode          `json:"from_node" doc:"Where the segment starts: an entry, a save, or the ending an after-story follows."`
	Condition          *string                  `json:"condition" maxLength:"1024" doc:"Precondition the source states for this segment, as it writes it. null if none. Must not be used as a discriminant."`
	ConditionLocalized map[string]LocalizedText `json:"condition_localized" doc:"BCP-47 keys: machine translations of condition. Personal names without a sourced catalog translation stay in the original script. Empty object if none. Must not be used as a discriminant."`
	RequiresEndingKeys []string                 `json:"requires_ending_keys" doc:"endings[].key of the endings the player must have seen before this segment. An imported chart fills it from condition only when every ending the condition names is one of this chart's; condition stays the full statement, so a condition with this empty is a prerequisite that was not resolved. Empty array, never null."`
	Steps              []WalkthroughStep        `json:"steps" doc:"Steps in play order. Empty array, never null."`
	ToNode             WalkthroughNode          `json:"to_node" doc:"Where the segment ends: an ending, a collection stop, or open when it stops without either."`
}

type WalkthroughNode struct {
	_        struct{} `json:"-" additionalProperties:"true"`
	NodeType string   `json:"node_type" enum:"entry,save,ending,collect,open" doc:"collect is a stop after collecting CG or scenes; open is a segment that simply stops."`
	Key      *string  `json:"key" pattern:"^[A-Za-z0-9_]+$" maxLength:"32" doc:"entries[], saves[] or endings[] key for those node types. null for collect and open."`
}

type WalkthroughStep struct {
	_            struct{}                 `json:"-" additionalProperties:"true"`
	StepType     string                   `json:"step_type" enum:"choice,action,save,heading,note" doc:"choice picks text from an in-game menu; action is something else the player does, such as a minigame or a route across a map; save creates save_key here. heading is a line the source groups the following steps under, such as a date, an in-game day, a chapter or a floor, up to the next heading or the end of the segment; note is an instruction or remark for the reader, usually marked ※. Neither heading nor note is picked, so a site numbering choices skips them. A site that meets an unknown step_type should show its text without numbering it."`
	Text         *string                  `json:"text" maxLength:"1024" doc:"The step as the source page writes it. null for a save step. Must not be used as a discriminant."`
	Localized    map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: machine translations of text. Personal names without a sourced catalog translation stay in the original script. Empty object if none. Must not be used as a discriminant."`
	Alternatives []WalkthroughAlternative `json:"alternatives" doc:"Other choices the source keeps for collecting CG differences; the main path picks text. Empty array, never null."`
	Achievement  bool                     `json:"achievement" doc:"The source marks this step as needed for an achievement only."`
	SaveKey      *string                  `json:"save_key" pattern:"^[A-Za-z0-9_]+$" maxLength:"32" doc:"saves[].key this save step creates. null for every other step_type."`
}

type WalkthroughRelease struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	ID          string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog release id, a release of this walkthrough's work."`
	Title       *string  `json:"title" maxLength:"512" doc:"The release's title. null if it has none. Must not be used as a discriminant."`
	ReleaseKind string   `json:"release_kind" enum:"default,digital,physical,trial,patch" doc:"Kind of the release; patch includes fan translation patches."`
	Lang        *string  `json:"lang" maxLength:"32" format:"bcp47" doc:"Main language of the release. null if unrecorded."`
	Companies   []string `json:"companies" doc:"Display names of the companies and groups credited on the release, such as the translation group of a patch. Empty array, never null. Must not be used as a discriminant."`
}
