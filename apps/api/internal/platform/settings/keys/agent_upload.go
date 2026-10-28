package keys

import "api/internal/platform/settings"

var AgentUploadMaxFiles = settings.Int(settings.Meta{
	Name:       "agent.upload.max_files",
	DescEN:     "Files one assistant message may carry. 0 hides the upload button.",
	DescZH:     "一条助手消息最多可带的文件数。0 表示不显示上传按钮。",
	SiteScoped: true,
	Public:     true,
	Min:        settings.F(0), Max: settings.F(10),
}, 4)

var AgentUploadImageEnabled = settings.Bool(settings.Meta{
	Name:       "agent.upload.image.enabled",
	DescEN:     "Lets users attach pictures. Also needs a ready agent.vision scene in the LLM gateway.",
	DescZH:     "允许用户上传图片。还需要 LLM 网关里的 agent.vision 场景可用。",
	SiteScoped: true,
	Public:     true,
}, true)

var AgentUploadImageMaxBytes = settings.Int(settings.Meta{
	Name:       "agent.upload.image.max_bytes",
	DescEN:     "Largest picture a user may attach, in bytes. Pictures are scaled down before the model sees them.",
	DescZH:     "可上传图片的最大字节数。图片在交给模型前会缩小。",
	SiteScoped: true,
	Public:     true,
	Min:        settings.F(1024), Max: settings.F(30 << 20),
}, 10<<20)

var AgentUploadImageTypes = settings.StringList(settings.Meta{
	Name:       "agent.upload.image.types",
	DescEN:     "Picture media types users may attach.",
	DescZH:     "允许上传的图片类型(MIME)。",
	SiteScoped: true,
	Public:     true,
	Pattern:    `^image/(png|jpeg|webp|gif)$`,
}, []string{"image/png", "image/jpeg", "image/webp", "image/gif"})

var AgentUploadTextEnabled = settings.Bool(settings.Meta{
	Name:       "agent.upload.text.enabled",
	DescEN:     "Lets users attach plain-text and code files.",
	DescZH:     "允许用户上传纯文本和代码文件。",
	SiteScoped: true,
	Public:     true,
}, true)

var AgentUploadTextMaxBytes = settings.Int(settings.Meta{
	Name:       "agent.upload.text.max_bytes",
	DescEN:     "Largest text file a user may attach, in bytes.",
	DescZH:     "可上传文本文件的最大字节数。",
	SiteScoped: true,
	Public:     true,
	Min:        settings.F(1024), Max: settings.F(10 << 20),
}, 512<<10)

var AgentUploadTextTypes = settings.StringList(settings.Meta{
	Name:       "agent.upload.text.types",
	DescEN:     "File extensions accepted as text, read as UTF-8.",
	DescZH:     "按文本(UTF-8)读取的文件扩展名。",
	SiteScoped: true,
	Public:     true,
	Pattern:    `^\.[a-z0-9]{1,10}$`,
}, []string{".txt", ".md", ".markdown", ".json", ".yaml", ".yml", ".csv", ".tsv", ".log", ".srt", ".ass", ".ini", ".toml", ".xml", ".html", ".css", ".js", ".ts", ".py", ".go", ".java", ".c", ".cpp", ".rs", ".lua", ".sh"})

var AgentUploadDocumentEnabled = settings.Bool(settings.Meta{
	Name:       "agent.upload.document.enabled",
	DescEN:     "Lets users attach documents; their text is extracted and the layout dropped.",
	DescZH:     "允许用户上传文档;只提取其中的文字,版式不保留。",
	SiteScoped: true,
	Public:     true,
}, true)

var AgentUploadDocumentMaxBytes = settings.Int(settings.Meta{
	Name:       "agent.upload.document.max_bytes",
	DescEN:     "Largest document a user may attach, in bytes.",
	DescZH:     "可上传文档的最大字节数。",
	SiteScoped: true,
	Public:     true,
	Min:        settings.F(1024), Max: settings.F(30 << 20),
}, 20<<20)

var AgentUploadDocumentTypes = settings.StringList(settings.Meta{
	Name:       "agent.upload.document.types",
	DescEN:     "Document extensions accepted.",
	DescZH:     "允许上传的文档扩展名。",
	SiteScoped: true,
	Public:     true,
	Pattern:    `^\.(pdf|docx)$`,
}, []string{".pdf", ".docx"})
