package handler

import (
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
)

func creditNameFromDetail(rec dto.PublicName, include []string, photoURL string) repr.CreditName {
	var personID *string
	if rec.PersonID > 0 {
		s := repr.ID(rec.PersonID)
		personID = &s
	}
	out := repr.CreditName{
		Object: "credit_name", ID: repr.ID(rec.ID), DisplayName: rec.DisplayName,
		Latin: optString(rec.Latin), Lang: optString(rec.Lang),
		Localized: localizedFrom(rec.Localized), PersonID: personID,
		BirthYear: intFromI16(rec.BirthY), BirthMonth: intFromI16(rec.BirthM), BirthDay: intFromI16(rec.BirthD),
	}
	out.Gender, _ = repr.Gender(rec.Gender)
	for _, t := range include {
		switch t {
		case "aliases":
			out.Aliases = ptrSlice(entityNamesFrom(rec.Aliases))
		case "photo":
			if rec.PhotoHash != "" {
				out.Photo = imageFromPublicMeta(photoURL, rec.PhotoMeta, "")
			}
		case "siblings":
			out.Siblings = ptrSlice(siblingNamesFrom(rec.Siblings, personID))
		case "intros":
			out.Intros = ptrSlice(introsFrom(rec.Intros))
		case "links":
			out.Links = ptrSlice(linksFrom(rec.Links))
		case "refs":
			out.Refs = ptrSlice(refsFrom(rec.Refs))
		}
	}
	return out
}

func siblingNamesFrom(in []dto.PublicSiblingName, personID *string) []repr.CreditName {
	out := make([]repr.CreditName, 0, len(in))
	for _, s := range in {
		out = append(out, repr.CreditName{
			Object: "credit_name", ID: repr.ID(s.ID), DisplayName: s.DisplayName,
			Latin: optString(s.Latin), Lang: optString(s.Lang),
			Localized: localizedFrom(s.Localized), PersonID: personID,
		})
	}
	return out
}
