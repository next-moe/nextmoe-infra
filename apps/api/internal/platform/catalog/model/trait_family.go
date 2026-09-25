package model

const traitSexualFamilyCTE = `
WITH RECURSIVE fam(id) AS (
    SELECT id FROM catalog_character_trait WHERE sexual
    UNION
    SELECT p.trait_id FROM catalog_character_trait_parent p JOIN fam ON p.parent_id = fam.id
)
`

const TraitSexualFamilySQL = traitSexualFamilyCTE + `
UPDATE catalog_character_trait t
   SET sexual_family = (t.id IN (SELECT id FROM fam))
 WHERE t.sexual_family IS DISTINCT FROM (t.id IN (SELECT id FROM fam))
`

const TraitSexualFamilyPendingSQL = traitSexualFamilyCTE + `
SELECT count(*) FROM catalog_character_trait t
 WHERE t.sexual_family IS DISTINCT FROM (t.id IN (SELECT id FROM fam))
`
