package dto

type CompositeSourceInput struct {
	ArtifactUUID string `json:"artifact_uuid,omitempty" format:"uuid" doc:"A ready, public, undeleted artifact of the caller's own site. Exactly one of artifact_uuid and data."`
	Data         []byte `json:"data,omitempty" doc:"Inline bytes (standard base64), stored with the composite instead of as an artifact — the zip glue. All inline sources together are capped at 8 MiB. Exactly one of artifact_uuid and data."`
	Size         *int64 `json:"size,omitempty" minimum:"1" doc:"Optional check: the source's exact byte length. A mismatch refuses the composite."`
}

type CompositeSegment struct {
	Source int   `json:"source" minimum:"0" doc:"Index into sources"`
	Offset int64 `json:"offset" minimum:"0" doc:"Byte offset inside that source"`
	Length int64 `json:"length" minimum:"1" doc:"Bytes taken from that offset"`
}

type CreateCompositeRequest struct {
	Filename    string                 `json:"filename" minLength:"1" maxLength:"255" doc:"Download filename, sent as an attachment Content-Disposition"`
	ContentType string                 `json:"content_type" minLength:"1" maxLength:"100" doc:"Media type of the assembled file, e.g. application/zip"`
	Size        int64                  `json:"size" minimum:"1" doc:"Exact byte length of the assembled file; must equal the sum of the segment lengths"`
	BLAKE3      string                 `json:"blake3,omitempty" required:"false" pattern:"^[0-9a-f]{64}$" doc:"Optional BLAKE3 of the assembled file, lowercase hex, served as the ETag; not recomputed by the service. Without it the ETag is the composite id, which already hashes the plan."`
	Sources     []CompositeSourceInput `json:"sources" minItems:"1" maxItems:"64"`
	Segments    []CompositeSegment     `json:"segments" minItems:"1" maxItems:"100000" doc:"The assembled file, in order"`
}

type CompositeResponse struct {
	ID           string   `json:"id" doc:"Content-addressed: the same plan from the same site always yields the same id"`
	SiteKey      string   `json:"site_key"`
	URL          string   `json:"url,omitempty" doc:"Public download URL; absent while the site has no artifact_cdn_base"`
	Filename     string   `json:"filename"`
	ContentType  string   `json:"content_type"`
	Size         int64    `json:"size"`
	BLAKE3       string   `json:"blake3,omitempty" doc:"Absent when the plan was created without one"`
	Artifacts    []string `json:"artifacts" doc:"UUIDs of the artifacts this composite pins, in source order"`
	SegmentCount int      `json:"segment_count"`
	InlineBytes  int64    `json:"inline_bytes"`
	CreatedAt    string   `json:"created_at"`
}
