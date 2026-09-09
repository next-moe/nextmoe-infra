# Vendored downstream face specs

Byte-identical mirrors of the two Tier-B downstream faces' OpenAPI documents, kept
here only so the developer portal has a spec file to build from — `apps/developer`
reads these paths from `scripts/faces.mjs` and copies them to `public/specs/` at
build time. They are not the contract.

| Face      | File                    | Single source of truth                                          |
| --------- | ----------------------- | --------------------------------------------------------------- |
| `moyu`    | `moyu-openapi.yaml`     | `kun-galgame-patch/docs/open-api/moyu-openapi.yaml`              |
| `sticker` | `sticker-openapi.yaml`  | `kun-galgame-stickers/docs/open-api/sticker-openapi.yaml`        |

Each face is served by its own repo's service; that repo owns the contract and is
the only place a change may be authored.

## Updating

Re-copy the file from the owning repo — no editing here, ever:

```bash
cp ../kun-galgame-patch/docs/open-api/moyu-openapi.yaml docs/downstream/moyu-openapi.yaml
cp ../kun-galgame-stickers/docs/open-api/sticker-openapi.yaml docs/downstream/sticker-openapi.yaml
pnpm --filter developer sync:specs
```

Drift is caught by the portal build: `EXPECTED_OPERATION_COUNTS` in
`apps/developer/scripts/faces.mjs` pins the operation count per face, the
autoGroup guard rejects a path no group claims, the spec-path coverage guard
rejects a path no face claims, and `assertDocZhCatalog` rejects any English doc
string with no Chinese overlay entry. An upstream change that adds, removes or
renames anything fails `sync:specs` rather than shipping a stale reference page.
