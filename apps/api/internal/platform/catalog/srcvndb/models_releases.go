package srcvndb

type Release struct {
	ID          string `gorm:"primaryKey" json:"id"`
	GTIN        int64  `gorm:"column:gtin;not null" json:"gtin"`
	OLang       string `gorm:"column:olang;not null" json:"olang"`
	Released    int    `gorm:"not null" json:"released"`
	Voiced      int16  `gorm:"not null" json:"voiced"`
	ResoX       int16  `gorm:"column:reso_x;not null" json:"reso_x"`
	ResoY       int16  `gorm:"column:reso_y;not null" json:"reso_y"`
	MinAge      *int16 `gorm:"column:minage" json:"minage"`
	AniStory    int16  `gorm:"column:ani_story;not null" json:"ani_story"`
	AniEro      int16  `gorm:"column:ani_ero;not null" json:"ani_ero"`
	AniStorySp  *int16 `gorm:"column:ani_story_sp" json:"ani_story_sp"`
	AniStoryCg  *int16 `gorm:"column:ani_story_cg" json:"ani_story_cg"`
	AniCutscene *int16 `gorm:"column:ani_cutscene" json:"ani_cutscene"`
	AniEroSp    *int16 `gorm:"column:ani_ero_sp" json:"ani_ero_sp"`
	AniEroCg    *int16 `gorm:"column:ani_ero_cg" json:"ani_ero_cg"`
	AniBg       *bool  `gorm:"column:ani_bg" json:"ani_bg"`
	AniFace     *bool  `gorm:"column:ani_face" json:"ani_face"`
	HasEro      bool   `gorm:"column:has_ero;not null" json:"has_ero"`
	Patch       bool   `gorm:"not null" json:"patch"`
	Freeware    bool   `gorm:"not null" json:"freeware"`
	Uncensored  *bool  `gorm:"column:uncensored" json:"uncensored"`
	Official    bool   `gorm:"not null" json:"official"`
	Catalog     string `gorm:"not null" json:"catalog"`
	Notes       string `gorm:"not null" json:"notes"`
	Engine      string `gorm:"not null" json:"engine"`
}

func (Release) TableName() string { return "src_vndb.releases" }

type ReleaseVN struct {
	ID    string `gorm:"primaryKey;column:id" json:"id"`
	VID   string `gorm:"primaryKey;column:vid;index" json:"vid"`
	RType string `gorm:"column:rtype;not null" json:"rtype"`
}

func (ReleaseVN) TableName() string { return "src_vndb.releases_vn" }

type ReleaseProducer struct {
	ID        string `gorm:"primaryKey;column:id" json:"id"`
	PID       string `gorm:"primaryKey;column:pid;index" json:"pid"`
	Developer bool   `gorm:"not null" json:"developer"`
	Publisher bool   `gorm:"not null" json:"publisher"`
}

func (ReleaseProducer) TableName() string { return "src_vndb.releases_producers" }

type ReleasePlatform struct {
	ID       string `gorm:"primaryKey;column:id" json:"id"`
	Platform string `gorm:"primaryKey;column:platform" json:"platform"`
}

func (ReleasePlatform) TableName() string { return "src_vndb.releases_platforms" }

type ReleaseTitle struct {
	ID    string `gorm:"primaryKey;column:id" json:"id"`
	Lang  string `gorm:"primaryKey;column:lang" json:"lang"`
	MTL   bool   `gorm:"column:mtl;not null" json:"mtl"`
	Title string `gorm:"not null" json:"title"`
	Latin string `gorm:"not null" json:"latin"`
}

func (ReleaseTitle) TableName() string { return "src_vndb.releases_titles" }

type Producer struct {
	ID          string `gorm:"primaryKey" json:"id"`
	Type        string `gorm:"not null" json:"type"`
	Lang        string `gorm:"not null" json:"lang"`
	Name        string `gorm:"not null" json:"name"`
	Latin       string `gorm:"not null" json:"latin"`
	Alias       string `gorm:"not null" json:"alias"`
	Description string `gorm:"not null" json:"description"`
}

func (Producer) TableName() string { return "src_vndb.producers" }

type Extlink struct {
	ID    int    `gorm:"primaryKey;column:id" json:"id"`
	Site  string `gorm:"not null;index" json:"site"`
	Value string `gorm:"not null" json:"value"`
}

func (Extlink) TableName() string { return "src_vndb.extlinks" }

type ReleaseExtlink struct {
	ID   string `gorm:"primaryKey;column:id" json:"id"`
	Link int    `gorm:"primaryKey;column:link;index" json:"link"`
}

func (ReleaseExtlink) TableName() string { return "src_vndb.releases_extlinks" }

type Engine struct {
	ID          string `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"not null" json:"name"`
	Description string `gorm:"not null" json:"description"`
}

func (Engine) TableName() string { return "src_vndb.engines" }
