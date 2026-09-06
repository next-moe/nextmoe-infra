package repr

import "api/pkg/imageclient"

type Image struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	URL       string   `json:"url" format:"uri" maxLength:"512" doc:"Absolute image URL. Never a bare hash."`
	Hash      string   `json:"hash" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"Image-service content hash."`
	Width     *int     `json:"width" minimum:"0" maximum:"65535" doc:"Pixel width. null if unknown."`
	Height    *int     `json:"height" minimum:"0" maximum:"65535" doc:"Pixel height. null if unknown."`
	Thumbhash *string  `json:"thumbhash" maxLength:"128" pattern:"^[A-Za-z0-9+/=_-]+$" doc:"Thumbhash. null if unknown."`
	Sexual    *string  `json:"sexual" enum:"safe,suggestive,explicit" doc:"Sexual depiction. null means not assessed."`
	Violence  *string  `json:"violence" enum:"tame,violent,brutal" doc:"Violent depiction. null means not assessed. Currently no catalog row has an assessment; the value is always null."`
	Source    string   `json:"source" maxLength:"64" doc:"Open vocabulary sources. Must not be used as a discriminant."`
}

type CoverSlot struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	URL       string   `json:"url" format:"uri" maxLength:"512" doc:"Absolute image URL. Never a bare hash."`
	Hash      string   `json:"hash" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"Image-service content hash."`
	Width     *int     `json:"width" minimum:"0" maximum:"65535" doc:"Pixel width. null if unknown."`
	Height    *int     `json:"height" minimum:"0" maximum:"65535" doc:"Pixel height. null if unknown."`
	Thumbhash *string  `json:"thumbhash" maxLength:"128" pattern:"^[A-Za-z0-9+/=_-]+$" doc:"Thumbhash. null if unknown."`
	Sexual    *string  `json:"sexual" enum:"safe,suggestive,explicit" doc:"Sexual depiction. null means not assessed."`
	Violence  *string  `json:"violence" enum:"tame,violent,brutal" doc:"Violent depiction. null means not assessed. Currently no catalog row has an assessment; the value is always null."`
	Source    string   `json:"source" maxLength:"64" doc:"Open vocabulary sources. Must not be used as a discriminant."`
	Origin    string   `json:"origin" enum:"cover,screenshot,censored" doc:"Which pool the image was elected from. screenshot only ever appears on banner, as the fallback for a work with no landscape cover. censored is a first-party blurred stand-in derived from explicit official art, served on the portrait slot when nothing else may fill it."`
}

type Cover struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	ID             string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"catalog_work_cover row id, not the image hash."`
	VoteCount      int      `json:"vote_count" minimum:"0" doc:"Net votes on this cover row."`
	PortraitPinned bool     `json:"portrait_pinned" doc:"Whether this row is pinned as the portrait cover."`
	Kind           string   `json:"cover_kind" maxLength:"32" doc:"Upstream cover class from the pinning ladder: main, dig, pkgfront, pkgback and friends. Open vocabulary. Empty if unrecorded. Must not be used as a discriminant."`
	URL            string   `json:"url" format:"uri" maxLength:"512" doc:"Absolute image URL. Never a bare hash."`
	Hash           string   `json:"hash" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"Image-service content hash."`
	Width          *int     `json:"width" minimum:"0" maximum:"65535" doc:"Pixel width. null if unknown."`
	Height         *int     `json:"height" minimum:"0" maximum:"65535" doc:"Pixel height. null if unknown."`
	Thumbhash      *string  `json:"thumbhash" maxLength:"128" pattern:"^[A-Za-z0-9+/=_-]+$" doc:"Thumbhash. null if unknown."`
	Sexual         *string  `json:"sexual" enum:"safe,suggestive,explicit" doc:"Sexual depiction. null means not assessed."`
	Violence       *string  `json:"violence" enum:"tame,violent,brutal" doc:"Violent depiction. null means not assessed. Currently always null."`
	Source         string   `json:"source" maxLength:"64" doc:"Open vocabulary sources. Must not be used as a discriminant."`
}

func NewImage(cdnBase, hash, source string, width, height *int, thumbhash *string, sexual, violence *int16) (*Image, bool) {
	if hash == "" {
		return nil, true
	}
	url := imageclient.MainURL(cdnBase, hash, "webp")
	if url == "" {
		return nil, true
	}
	sx, ok := Sexual(sexual)
	if !ok {
		return nil, false
	}
	vx, ok := Violence(violence)
	if !ok {
		return nil, false
	}
	return &Image{
		URL: url, Hash: hash, Width: width, Height: height,
		Thumbhash: thumbhash, Sexual: sx, Violence: vx, Source: source,
	}, true
}

func NewCover(cdnBase, hash, source string, rowID int64, voteCount int, portraitPinned bool, width, height *int, thumbhash *string, sexual, violence *int16) (*Cover, bool) {
	img, ok := NewImage(cdnBase, hash, source, width, height, thumbhash, sexual, violence)
	if !ok {
		return nil, false
	}
	if img == nil {
		return nil, true
	}
	if rowID <= 0 {
		return nil, false
	}
	return &Cover{
		ID: ID(rowID), VoteCount: voteCount, PortraitPinned: portraitPinned,
		URL: img.URL, Hash: img.Hash, Width: img.Width, Height: img.Height,
		Thumbhash: img.Thumbhash, Sexual: img.Sexual, Violence: img.Violence, Source: img.Source,
	}, true
}
