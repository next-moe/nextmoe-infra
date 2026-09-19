package keys

import (
	"strings"

	"api/internal/platform/settings"
)

const JobSchedulePattern = `^(daily@(?:[01][0-9]|2[0-3]):[0-5][0-9]|every:[1-9][0-9]*[mh])$`

type JobKeys struct {
	Enabled  *settings.Key[bool]
	Schedule *settings.Key[string]
}

var jobSpecs = []struct{ Name, Schedule string }{
	{"image-gc", "daily@03:30"},
	{"galgame-image-refping", "daily@04:00"},
	{"catalog-image-refping", "daily@04:15"},
	{"news-image-refping", "daily@04:20"},
	{"user-avatar-refping", "daily@04:30"},
	{"image-ref-audit", "daily@04:45"},
	{"artifact-gc", "daily@05:30"},
	{"prune-developer-usage", "daily@06:00"},
	{"ymgal-news-poll", "every:10m"},
	{"ymgal-news-sweep", "daily@04:05"},
	{"store-stats-sync", "every:1h"},
	{"store-stats-resync", "daily@05:10"},
	{"news-moderate", "every:5m"},
}

var jobKeys, jobsDomain = buildJobKeys()

func buildJobKeys() (map[string]JobKeys, settings.Domain) {
	byName := make(map[string]JobKeys, len(jobSpecs))
	entries := make([]settings.Entry, 0, len(jobSpecs)*2)
	for _, spec := range jobSpecs {
		n := strings.ReplaceAll(spec.Name, "-", "_")
		jk := JobKeys{
			Enabled: settings.Bool(settings.Meta{
				Name:   "jobs." + n + ".enabled",
				DescEN: "Whether the scheduler runs " + spec.Name + " on its schedule; off leaves it manual-only (run-now from the console still works).",
				DescZH: "调度器是否按计划自动运行 " + spec.Name + ";关闭后只能在控制台手动触发。",
			}, true),
			Schedule: settings.String(settings.Meta{
				Name:    "jobs." + n + ".schedule",
				DescEN:  "When " + spec.Name + " runs: daily@HH:MM (server local time, UTC in production) or every:<N>m / every:<N>h.",
				DescZH:  spec.Name + " 的运行计划:daily@HH:MM(服务器本地时间,生产为 UTC)或 every:<N>m / every:<N>h。",
				Pattern: JobSchedulePattern,
			}, spec.Schedule),
		}
		byName[spec.Name] = jk
		entries = append(entries, jk.Enabled, jk.Schedule)
	}
	return byName, settings.Domain{
		Name:    "jobs",
		TitleZH: "后台任务",
		Keys:    entries,
	}
}

func Job(name string) (JobKeys, bool) {
	jk, ok := jobKeys[name]
	return jk, ok
}

func JobNames() []string {
	out := make([]string, len(jobSpecs))
	for i, spec := range jobSpecs {
		out[i] = spec.Name
	}
	return out
}
