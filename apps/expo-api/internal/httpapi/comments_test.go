package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func jsonBody(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCommentEndpoints(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	entry := createTestEntry(t, handler) // anonymous, public
	comments := "/api/v1/entries/" + entry.ID + "/comments"

	if code := serve(handler, authedRequest(http.MethodPost, comments, "", jsonBody(t, map[string]string{"body": "hi"}))).Code; code != http.StatusUnauthorized {
		t.Fatalf("anonymous comment status = %d, want 401", code)
	}

	alice, _ := registerTestUser(t, handler, "comment-alice")
	bob, _ := registerTestUser(t, handler, "comment-bob")

	created := serve(handler, authedRequest(http.MethodPost, comments, alice, jsonBody(t, map[string]string{"body": "nice [item=iron-ore]"})))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body)
	}
	var comment catalog.Comment
	if err := json.Unmarshal(created.Body.Bytes(), &comment); err != nil || comment.Number != 1 || comment.Author != "comment-alice" {
		t.Fatalf("created comment = %+v (%v)", comment, err)
	}

	if code := serve(handler, authedRequest(http.MethodPost, comments, alice, jsonBody(t, map[string]string{"body": " "}))).Code; code != http.StatusBadRequest {
		t.Fatalf("empty comment status = %d, want 400", code)
	}
	if code := serve(handler, authedRequest(http.MethodPatch, comments+"/1", bob, jsonBody(t, map[string]string{"body": "mine now"}))).Code; code != http.StatusForbidden {
		t.Fatalf("editing someone else's comment status = %d, want 403", code)
	}
	if code := serve(handler, authedRequest(http.MethodPatch, comments+"/1", alice, jsonBody(t, map[string]string{"body": "nicer"}))).Code; code != http.StatusOK {
		t.Fatalf("edit status = %d, want 200", code)
	}
	if code := serve(handler, authedRequest(http.MethodPost, comments+"/1/report", bob, jsonBody(t, map[string]string{"reason": "spam"}))).Code; code != http.StatusNoContent {
		t.Fatalf("report status = %d, want 204", code)
	}
	if code := serve(handler, authedRequest(http.MethodDelete, comments+"/x", alice, nil)).Code; code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed number status = %d, want 422", code)
	}
	if code := serve(handler, authedRequest(http.MethodDelete, comments+"/1", bob, nil)).Code; code != http.StatusForbidden {
		t.Fatalf("stranger remove status = %d, want 403", code)
	}
	// The anonymous uploader moderates with the edit token.
	if code := serve(handler, authedRequest(http.MethodDelete, comments+"/1?token="+entry.EditToken, "", nil)).Code; code != http.StatusNoContent {
		t.Fatalf("edit-token remove status = %d, want 204", code)
	}

	listed := serve(handler, authedRequest(http.MethodGet, comments, "", nil))
	var page catalog.CommentPage
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil || listed.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listed.Code, listed.Body)
	}
	if len(page.Comments) != 1 || page.Comments[0].RemovedBy != "owner" || page.Viewer.Blocked != "signed_out" {
		t.Fatalf("page = %+v", page)
	}
}

func TestUploadBanBlocksUploads(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	token, userID := registerTestUser(t, handler, "banned-uploader")

	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by)
		VALUES ('uploadBan001', $1, ARRAY['upload'], 'spam', 'admin@example.test')`, userID); err != nil {
		t.Fatalf("insert ban: %v", err)
	}
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for _, path := range []string{"/api/v1/entries", "/api/v1/previews"} {
		recorder := serve(handler, authedRequest(http.MethodPost, path, token, jsonBody(t, map[string]string{"blueprint": string(exchange)})))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("banned upload to %s status = %d, body = %s", path, recorder.Code, recorder.Body)
		}
	}
}
