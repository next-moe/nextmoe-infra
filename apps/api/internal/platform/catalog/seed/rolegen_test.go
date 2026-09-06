package seed

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedArtifactsDrift(t *testing.T) {
	generated, err := GenerateBangumiRoles()
	require.NoError(t, err)

	wantRoles, err := RenderRolesYAML(generated.Roles)
	require.NoError(t, err)
	gotRoles, err := dataFS.ReadFile("data/roles.gen.yaml")
	require.NoError(t, err)
	assert.Equal(t, string(wantRoles), string(gotRoles), "data/roles.gen.yaml drifted from generation logic")

	wantMap, err := RenderRoleMapYAML(generated.Mappings)
	require.NoError(t, err)
	gotMap, err := dataFS.ReadFile("data/bangumi_role_map.gen.yaml")
	require.NoError(t, err)
	assert.Equal(t, string(wantMap), string(gotMap), "data/bangumi_role_map.gen.yaml drifted from generation logic")
}

func TestGenerateBangumiRoles(t *testing.T) {
	generated, err := GenerateBangumiRoles()
	require.NoError(t, err)
	roles, mappings := generated.Roles, generated.Mappings

	assert.Greater(t, len(roles), 100)
	assert.Less(t, len(roles), 246)
	assert.Len(t, mappings, 246)

	keys := make(map[string]bool, len(roles))
	for i, r := range roles {
		assert.False(t, keys[r.Key], "duplicate key %q", r.Key)
		keys[r.Key] = true
		assert.Equal(t, roleIDBase+int64(i), r.ID)
		if i > 0 {
			assert.Less(t, roles[i-1].Key, r.Key, "roles not in key order")
		}
	}

	byID := make(map[int64]RoleSeed, len(roles))
	for _, r := range roles {
		byID[r.ID] = r
	}
	seenSourceRoles := make(map[string]bool, len(mappings))
	for _, m := range mappings {
		_, ok := byID[m.RoleID]
		assert.True(t, ok, "mapping %s references unknown role %d", m.SourceRole, m.RoleID)
		assert.False(t, seenSourceRoles[m.SourceRole], "duplicate mapping for %s", m.SourceRole)
		seenSourceRoles[m.SourceRole] = true
		st, pos, found := strings.Cut(m.SourceRole, ":")
		require.True(t, found)
		_, err := strconv.Atoi(st)
		assert.NoError(t, err)
		_, err = strconv.Atoi(pos)
		assert.NoError(t, err)
	}

	m4x1001, ok := findMapping(mappings, "4:1001")
	require.True(t, ok)
	assert.Contains(t, byID[m4x1001.RoleID].Key, "developer")
	assert.Equal(t, "开发", m4x1001.Note)
	assert.True(t, keys["producer"] && keys["producer-2"] && keys["producer-3"])
}

func findMapping(mappings []RoleMapSeed, sourceRole string) (RoleMapSeed, bool) {
	for _, m := range mappings {
		if m.SourceRole == sourceRole {
			return m, true
		}
	}
	return RoleMapSeed{}, false
}

func TestHandSeedsIntegrity(t *testing.T) {
	assert.Len(t, media(), 7)
	assert.Len(t, sources(), 21)
	assert.Len(t, relationTypes(), 16)
	assert.Len(t, platforms(), 48)
	seenPlat := map[string]struct{}{}
	for _, p := range platforms() {
		assert.NotEmpty(t, p.Key)
		assert.NotEmpty(t, p.DisplayName, "%s: display name required", p.Key)
		_, dup := seenPlat[p.Key]
		assert.False(t, dup, "%s: duplicate platform key", p.Key)
		seenPlat[p.Key] = struct{}{}
	}

	var bangumiOK bool
	for _, s := range sources() {
		if s.ID == bangumiSourceID {
			assert.Equal(t, "bangumi", s.Key)
			bangumiOK = true
		}
	}
	assert.True(t, bangumiOK)

	for _, rt := range relationTypes() {
		if rt.IsSymmetric {
			assert.Equal(t, rt.ForwardPhrase, rt.ReversePhrase, "%s: symmetric relation must render one phrase", rt.Key)
		}
	}

	roles, roleMap, err := loadGeneratedRoles()
	require.NoError(t, err)
	assert.Len(t, roleMap, 246)
	assert.NotEmpty(t, roles)
	for _, m := range roleMap {
		assert.Equal(t, bangumiSourceID, m.SourceID)
	}

	roleIDs := make(map[int64]bool, len(roles))
	roleKeys := make(map[string]bool, len(roles))
	for _, r := range roles {
		roleIDs[r.ID] = true
		roleKeys[r.Key] = true
	}
	for _, h := range handRoles() {
		assert.Less(t, h.ID, int64(roleIDBase), "hand role %q must stay in the reserved band", h.Key)
		assert.False(t, roleIDs[h.ID], "hand role id %d collides with a generated role", h.ID)
		assert.False(t, roleKeys[h.Key], "hand role key %q collides with a generated role", h.Key)
		roleIDs[h.ID] = true
		roleKeys[h.Key] = true
	}

	want := map[int64]struct{ key, cat string }{
		roleTranslator: {"translator", "other"},
		roleEditor:     {"text-editor", "other"},
		roleQA:         {"qa", "other"},
	}
	for id, w := range want {
		var found bool
		for _, h := range handRoles() {
			if h.ID == id {
				found = true
				assert.Equal(t, w.key, h.Key)
				assert.Equal(t, w.cat, h.Category)
			}
		}
		assert.True(t, found, "reserved role id %d missing from handRoles", id)
	}

	vm := make(map[string]int64)
	for _, m := range vndbRoleMap() {
		assert.Equal(t, vndbSourceID, m.SourceID)
		assert.True(t, roleIDs[m.RoleID], "vndb map %q → unknown role %d", m.SourceRole, m.RoleID)
		vm[m.SourceRole] = m.RoleID
	}
	assert.Equal(t, roleTranslator, vm["translator"])
	assert.Equal(t, roleEditor, vm["editor"])
	assert.Equal(t, roleQA, vm["qa"])
}
