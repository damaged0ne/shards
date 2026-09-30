package db

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := NewSqlite(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { _ = database.DB().Close() })
	return database
}

func TestCommentCRUD(t *testing.T) {
	database := newTestDB(t)
	p := &Project{Name: "test"}
	require.NoError(t, database.SaveProject(p))

	c1 := &Comment{
		ProjectId:  p.Id,
		TargetType: CommentTargetIncident,
		TargetId:   "abc123",
		Author:     "Alice",
		AuthorId:   1,
		Body:       "Looking into it",
	}
	require.NoError(t, database.AddComment(c1))
	assert.NotZero(t, c1.Id)
	assert.Equal(t, CommentKindComment, c1.Kind)
	assert.Equal(t, CommentAuthorUser, c1.AuthorKind)
	assert.NotZero(t, c1.CreatedAt)

	c2 := &Comment{
		ProjectId:  p.Id,
		TargetType: CommentTargetIncident,
		TargetId:   "abc123",
		Author:     "triage-bot",
		AuthorId:   2,
		AuthorKind: CommentAuthorAgent,
		Kind:       CommentKindAction,
		Body:       "Restarted the pod",
		Meta:       map[string]string{"action": "resolved", "via": "mcp"},
	}
	require.NoError(t, database.AddComment(c2))
	assert.Greater(t, c2.Id, c1.Id)

	// another target must not leak into the timeline
	require.NoError(t, database.AddComment(&Comment{ProjectId: p.Id, TargetType: CommentTargetAlert, TargetId: "abc123", Author: "x", Body: "other"}))

	list, err := database.GetComments(p.Id, CommentTargetIncident, "abc123")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, c1.Id, list[0].Id)
	assert.Equal(t, "Looking into it", list[0].Body)
	assert.Equal(t, CommentAuthorAgent, list[1].AuthorKind)
	assert.Equal(t, CommentKindAction, list[1].Kind)
	assert.Equal(t, map[string]string{"action": "resolved", "via": "mcp"}, list[1].Meta)
	assert.Nil(t, list[0].Meta)

	require.NoError(t, database.UpdateCommentBody(p.Id, c1.Id, "Found the cause"))
	got, err := database.GetComment(p.Id, c1.Id)
	require.NoError(t, err)
	assert.Equal(t, "Found the cause", got.Body)
	assert.NotZero(t, got.EditedAt)

	require.NoError(t, database.DeleteComment(p.Id, c1.Id))
	_, err = database.GetComment(p.Id, c1.Id)
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.True(t, errors.Is(database.DeleteComment(p.Id, c1.Id), ErrNotFound))
	assert.True(t, errors.Is(database.UpdateCommentBody(p.Id, c1.Id, "x"), ErrNotFound))

	// wrong project
	_, err = database.GetComment("nope", c2.Id)
	assert.True(t, errors.Is(err, ErrNotFound))

	// validation
	assert.True(t, errors.Is(database.AddComment(&Comment{ProjectId: p.Id, TargetType: "bogus", TargetId: "1"}), ErrInvalid))
	assert.True(t, errors.Is(database.AddComment(&Comment{ProjectId: p.Id, TargetType: CommentTargetAlert}), ErrInvalid))

	// empty timeline is an empty slice, not nil (serialized as [])
	empty, err := database.GetComments(p.Id, CommentTargetAlertingRule, "none")
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Len(t, empty, 0)

	// comments are removed with the project
	require.NoError(t, database.DeleteProject(p.Id))
	list, err = database.GetComments(p.Id, CommentTargetIncident, "abc123")
	require.NoError(t, err)
	assert.Len(t, list, 0)
}

func TestUserApiKeyName(t *testing.T) {
	database := newTestDB(t)
	id, err := database.AddServiceAccount("bot", "Bot", "Editor")
	require.NoError(t, err)
	require.NoError(t, database.AddUserApiKey(id, "secret-key", "triage-agent"))
	u, err := database.GetUserByApiKey("secret-key")
	require.NoError(t, err)
	assert.Equal(t, "triage-agent", u.ApiKey)
	assert.Equal(t, "Bot", u.Name)
}
