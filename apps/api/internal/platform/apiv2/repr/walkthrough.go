package repr

type Walkthrough struct {
	_                struct{}               `json:"-" additionalProperties:"true"`
	Object           string                 `json:"object" enum:"walkthrough" doc:"Type discriminant. Always walkthrough."`
	ID               string                 `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog walkthrough id."`
	WorkID           string                 `json:"work_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog work this walkthrough plays through."`
	WorkContentLimit string                 `json:"work_content_limit" enum:"sfw,nsfw" doc:"content_limit of the work, the display axis GET /v2/catalog/works/{id} reports and the content_limit filter selects on. Independent of nsfw, which gates only on content_rating: most r18 games are sfw here. A reader who has not opted into adult content should be shown this walkthrough only when it is sfw."`
	Source           string                 `json:"source" maxLength:"64" doc:"Open vocabulary sources: the site whose walkthrough page the choice graph was derived from. Must not be used as a discriminant."`
	SourceURL        string                 `json:"source_url" format:"uri" maxLength:"2048" doc:"The original walkthrough page. Only its choice graph is carried here, never its prose; link to it for attribution."`
	SourceAuthor     *string                `json:"source_author" maxLength:"256" doc:"Author the source page credits. null if it credits none. Must not be used as a discriminant."`
	SourceUpdatedOn  *string                `json:"source_updated_on" format:"date" maxLength:"10" doc:"Date the source page says it was last updated. null if it does not say."`
	Lang             string                 `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language of every text field in the chart. Translations sit in localized maps beside them."`
	Characters       []WalkthroughCharacter `json:"characters" doc:"Characters the chart routes to, in chart order. Empty array, never null."`
	EndingCount      int                    `json:"ending_count" minimum:"0" doc:"Endings in the chart."`
	SaveCount        int                    `json:"save_count" minimum:"0" doc:"Save points in the chart."`
	SegmentCount     int                    `json:"segment_count" minimum:"0" doc:"Segments in the chart."`
	UpdatedAt        string                 `json:"updated_at" format:"date-time" maxLength:"32" doc:"Last time this walkthrough row changed."`
	Chart            *WalkthroughChart      `json:"chart,omitempty" doc:"The choice graph. Present on GET /v2/catalog/walkthroughs/{id} only; absent on the collection."`
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
	_                   struct{} `json:"-" additionalProperties:"true"`
	Key                 string   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local key that a segment's from node refers to."`
	Label               string   `json:"label" maxLength:"256" doc:"The entry as the source page names it, such as START or RESTART. Must not be used as a discriminant."`
	UnlockedByEndingKey *string  `json:"unlocked_by_ending_key" pattern:"^[A-Za-z0-9_]+$" maxLength:"32" doc:"endings[].key whose completion unlocks this entry. null if it is open from the start."`
}

type WalkthroughSave struct {
	_     struct{} `json:"-" additionalProperties:"true"`
	Key   string   `json:"key" pattern:"^[A-Za-z0-9_]+$" minLength:"1" maxLength:"32" doc:"Chart-local key that save steps and save nodes refer to."`
	Label string   `json:"label" maxLength:"256" doc:"The save point as the source page labels it. Must not be used as a discriminant."`
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
	StepType     string                   `json:"step_type" enum:"choice,action,save" doc:"choice picks text; action is a non-choice step such as a minigame; save creates save_key here."`
	Text         *string                  `json:"text" maxLength:"1024" doc:"The choice or action as the source page writes it. null for a save step. Must not be used as a discriminant."`
	Localized    map[string]LocalizedText `json:"localized" doc:"BCP-47 keys: machine translations of text. Personal names without a sourced catalog translation stay in the original script. Empty object if none. Must not be used as a discriminant."`
	Alternatives []WalkthroughAlternative `json:"alternatives" doc:"Other choices the source keeps for collecting CG differences; the main path picks text. Empty array, never null."`
	Achievement  bool                     `json:"achievement" doc:"The source marks this step as needed for an achievement only."`
	SaveKey      *string                  `json:"save_key" pattern:"^[A-Za-z0-9_]+$" maxLength:"32" doc:"saves[].key this save step creates. null for choice and action steps."`
}
