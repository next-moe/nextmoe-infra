package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

// projectionSQL (2026-09-27, plan 13 D5) queues a post for re-projection into
// community_activity whenever anything its activity is derived from changes:
// the post itself, its thread, the thread's anchor presentation, or the site's
// switch and rules. Triggers rather than hooks in the services, because
// moderation, purge and restore, rehome and merge all change these columns
// from several places, and a missed path would leave a stale item in every
// follower's feed. Existing rows are not queued here: a site's posts are
// queued when its community_activity_site row is enabled, and notify_after,
// stamped then, keeps every post written before it from notifying — those
// already raised kind 9. Every statement is CREATE OR REPLACE, so a rerun
// changes nothing.
func projectionSQL(db *gorm.DB) error {
	for _, st := range []struct{ name, stmt string }{
		{"projection enqueue function", `
			CREATE OR REPLACE FUNCTION community_activity_enqueue(ids bigint[])
			RETURNS void LANGUAGE sql AS $$
			    INSERT INTO community_activity_projection (post_id, enqueued_at)
			    SELECT id, clock_timestamp() FROM unnest(ids) AS id ORDER BY id
			    ON CONFLICT (post_id) DO UPDATE SET enqueued_at = clock_timestamp()
			$$`},
		{"post trigger function", `
			CREATE OR REPLACE FUNCTION community_activity_project_post()
			RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
			    IF TG_OP = 'DELETE' THEN
			        PERFORM community_activity_enqueue(ARRAY[OLD.id]);
			    ELSE
			        PERFORM community_activity_enqueue(ARRAY[NEW.id]);
			    END IF;
			    RETURN NULL;
			END
			$$`},
		{"post trigger", `
			CREATE OR REPLACE TRIGGER trg_community_activity_project_post
			    AFTER INSERT OR DELETE OR UPDATE OF status, content_raw, content_rating, thread_id, author_id
			    ON community_post FOR EACH ROW EXECUTE FUNCTION community_activity_project_post()`},
		{"thread trigger function", `
			CREATE OR REPLACE FUNCTION community_activity_project_thread()
			RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
			    PERFORM community_activity_enqueue(
			        ARRAY(SELECT id FROM community_post WHERE thread_id = NEW.id ORDER BY id));
			    RETURN NULL;
			END
			$$`},
		{"thread trigger", `
			CREATE OR REPLACE TRIGGER trg_community_activity_project_thread
			    AFTER UPDATE OF site, kind, anchor_kind, anchor_id, title, content_rating, status, merged_into_id
			    ON community_thread FOR EACH ROW
			    WHEN ((OLD.site, OLD.kind, OLD.anchor_kind, OLD.anchor_id, OLD.title, OLD.content_rating,
			           OLD.status, OLD.merged_into_id)
			          IS DISTINCT FROM
			          (NEW.site, NEW.kind, NEW.anchor_kind, NEW.anchor_id, NEW.title, NEW.content_rating,
			           NEW.status, NEW.merged_into_id))
			    EXECUTE FUNCTION community_activity_project_thread()`},
		{"anchor trigger function", `
			CREATE OR REPLACE FUNCTION community_activity_project_anchor()
			RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
			    IF TG_OP = 'UPDATE' AND
			       (OLD.title, OLD.url, OLD.work_id, OLD.cover_image_hash, OLD.content_limit, OLD.removed_at IS NULL)
			       IS NOT DISTINCT FROM
			       (NEW.title, NEW.url, NEW.work_id, NEW.cover_image_hash, NEW.content_limit, NEW.removed_at IS NULL) THEN
			        RETURN NULL;
			    END IF;
			    PERFORM community_activity_enqueue(ARRAY(
			        SELECT p.id FROM community_thread t JOIN community_post p ON p.thread_id = t.id
			         WHERE t.anchor_kind = NEW.anchor_kind AND t.anchor_id = NEW.anchor_id
			           AND t.site = NEW.site ORDER BY p.id));
			    RETURN NULL;
			END
			$$`},
		{"anchor trigger", `
			CREATE OR REPLACE TRIGGER trg_community_activity_project_anchor
			    AFTER INSERT OR UPDATE ON community_anchor_presentation
			    FOR EACH ROW EXECUTE FUNCTION community_activity_project_anchor()`},
		{"site stamp function", `
			CREATE OR REPLACE FUNCTION community_activity_site_stamp()
			RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
			    IF TG_OP = 'INSERT' OR (NEW.enabled AND NOT OLD.enabled)
			       OR NEW.rules IS DISTINCT FROM OLD.rules OR NEW.thread_url IS DISTINCT FROM OLD.thread_url THEN
			        NEW.notify_after := now();
			    END IF;
			    NEW.updated_at := now();
			    RETURN NEW;
			END
			$$`},
		{"site stamp trigger", `
			CREATE OR REPLACE TRIGGER trg_community_activity_site_stamp
			    BEFORE INSERT OR UPDATE ON community_activity_site
			    FOR EACH ROW EXECUTE FUNCTION community_activity_site_stamp()`},
		{"site trigger function", `
			CREATE OR REPLACE FUNCTION community_activity_project_site()
			RETURNS trigger LANGUAGE plpgsql AS $$
			DECLARE
			    s text;
			BEGIN
			    IF TG_OP = 'INSERT' AND NOT NEW.enabled THEN
			        RETURN NULL;
			    END IF;
			    IF TG_OP = 'UPDATE' AND
			       (OLD.enabled, OLD.thread_url, OLD.rules)
			       IS NOT DISTINCT FROM
			       (NEW.enabled, NEW.thread_url, NEW.rules) THEN
			        RETURN NULL;
			    END IF;
			    IF TG_OP = 'DELETE' THEN s := OLD.site; ELSE s := NEW.site; END IF;
			    PERFORM community_activity_enqueue(ARRAY(
			        SELECT p.id FROM community_thread t JOIN community_post p ON p.thread_id = t.id
			         WHERE t.site = s ORDER BY p.id));
			    RETURN NULL;
			END
			$$`},
		{"site trigger", `
			CREATE OR REPLACE TRIGGER trg_community_activity_project_site
			    AFTER INSERT OR UPDATE OR DELETE ON community_activity_site
			    FOR EACH ROW EXECUTE FUNCTION community_activity_project_site()`},
		// The projection finds a post's earlier activity by key alone: after a
		// rehome it may sit under another site, and a deleted post has no
		// thread left to say which.
		{"own activity key index", `
			CREATE INDEX IF NOT EXISTS idx_community_activity_own_key
			    ON community_activity(key) WHERE key LIKE 'community:%'`},
		{"projection claim index", `
			CREATE INDEX IF NOT EXISTS idx_community_activity_projection_claim
			    ON community_activity_projection(enqueued_at, post_id)`},
	} {
		if err := db.Exec(st.stmt).Error; err != nil {
			return fmt.Errorf("%s: %w", st.name, err)
		}
	}
	return nil
}
