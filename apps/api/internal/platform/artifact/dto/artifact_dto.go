package dto

type InitUploadRequest struct {
	Name        string `json:"name" validate:"required,max=255"`
	Description string `json:"description,omitempty" validate:"max=2000"`
	FileSize    int64  `json:"file_size" validate:"required,gt=0"`
	MimeType    string `json:"mime_type,omitempty" validate:"max=100"`
	Checksum    string `json:"checksum,omitempty" validate:"omitempty,len=64"`
	Public      bool   `json:"public,omitempty"`
	UploaderSub string `json:"uploader_sub,omitempty" validate:"max=64"`
}

type PartURL struct {
	PartNumber int32  `json:"part_number"`
	URL        string `json:"url"`
}

type InitUploadResponse struct {
	UUID      string    `json:"uuid"`
	Multipart bool      `json:"multipart"`
	UploadURL string    `json:"upload_url,omitempty"`
	UploadID  string    `json:"upload_id,omitempty"`
	PartSize  int64     `json:"part_size,omitempty"`
	PartURLs  []PartURL `json:"part_urls,omitempty"`
	ExpiresAt string    `json:"expires_at"`
}

type CompletedPart struct {
	PartNumber int32  `json:"part_number" validate:"required,gt=0"`
	ETag       string `json:"etag" validate:"required"`
}

type UploadedPart struct {
	PartNumber int32  `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

type ResumeUploadResponse struct {
	UUID          string         `json:"uuid"`
	Multipart     bool           `json:"multipart"`
	UploadURL     string         `json:"upload_url,omitempty"`
	PartSize      int64          `json:"part_size,omitempty"`
	UploadedParts []UploadedPart `json:"uploaded_parts,omitempty"`
	PartURLs      []PartURL      `json:"part_urls,omitempty"`
	ExpiresAt     string         `json:"expires_at"`
}

type ManifestInput struct {
	Executable   string         `json:"executable" validate:"required,max=500"`
	Arguments    string         `json:"arguments,omitempty" validate:"max=1000"`
	WorkingDir   string         `json:"working_dir,omitempty" validate:"max=500"`
	SavePath     string         `json:"save_path,omitempty" validate:"max=500"`
	Requirements map[string]any `json:"requirements,omitempty"`
}

type CompleteUploadRequest struct {
	Parts    []CompletedPart `json:"parts,omitempty"`
	Manifest *ManifestInput  `json:"manifest,omitempty"`
}

type ArtifactResponse struct {
	UUID        string `json:"uuid"`
	SiteKey     string `json:"site_key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	FileSize    int64  `json:"file_size"`
	MimeType    string `json:"mime_type"`
	Checksum    string `json:"checksum"`
	Status      int    `json:"status"`
	Public      bool   `json:"public"`
	CreatedAt   string `json:"created_at"`
}

type DownloadResponse struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type RenameArtifactRequest struct {
	Name string `json:"name" minLength:"1" maxLength:"255" doc:"New download filename. The extension must stay the same; the Content-Disposition of the stored object is rewritten to match."`
}
