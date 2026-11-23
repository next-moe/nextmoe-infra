package walkthrough

func (e Ending) classified() bool {
	return e.RouteType != nil || e.Outcome != nil || e.After || e.Other
}

func ClassifyEndings(c *Chart) {
	for i := range c.Endings {
		e := &c.Endings[i]
		if !e.classified() {
			classifyImported(e)
		}
		if e.classified() {
			e.ImportType = ""
		}
	}
}

func classifyImported(e *Ending) {
	byCount := RouteCommon
	switch {
	case len(e.Characters) == 1:
		byCount = RouteHeroine
	case len(e.Characters) > 1:
		byCount = RouteHarem
	}
	switch e.ImportType {
	case "heroine":
		e.RouteType, e.Outcome = ptr(RouteHeroine), ptr("happy")
		if len(e.Characters) > 1 {
			e.RouteType = ptr(RouteHarem)
		}
	case "harem":
		e.RouteType, e.Outcome = ptr(RouteHarem), ptr("happy")
	case "true", "normal", "bad":
		e.RouteType, e.Outcome = ptr(byCount), ptr(e.ImportType)
	case "after":
		e.RouteType, e.After = ptr(byCount), true
	case "other":
		e.Other = true
	}
}

func EndingType(e Ending) string {
	switch {
	case e.After:
		return "after"
	case e.Other:
		return "other"
	case e.Outcome != nil && *e.Outcome != "happy":
		return *e.Outcome
	case e.RouteType != nil && (*e.RouteType == RouteHeroine || *e.RouteType == RouteHarem):
		return *e.RouteType
	}
	return "other"
}

func ptr(s string) *string { return &s }
