package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cartabinaria/auth"
	"github.com/cartabinaria/auth/pkg/middleware"
	"github.com/cartabinaria/polleg/models"
	"github.com/cartabinaria/polleg/util"
	"github.com/google/uuid"
)

func TestPostAnswerHandlerContent(t *testing.T) {
	dsn := os.Getenv("POLLEG_TEST_DSN")
	if dsn == "" {
		t.Skip("set POLLEG_TEST_DSN to run PostgreSQL integration tests")
	}
	if err := util.ConnectDb(dsn); err != nil {
		t.Fatal(err)
	}
	db := util.GetDb()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })

	// Keep the test schema on the connection used by the handler.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	schema := "post_answer_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	if err := db.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Proposal{}, &models.Question{}, &models.Answer{}, &models.Vote{}, &models.Image{}, &models.AnswerVersion{}, &models.Report{}); err != nil {
		t.Fatal(err)
	}

	storedUser := models.User{Username: "answer-test-user", Alias: "AnswerTestUser"}
	if err := db.Create(&storedUser).Error; err != nil {
		t.Fatal(err)
	}
	user := auth.User{ID: storedUser.ID, Username: storedUser.Username, Role: auth.RoleUser}
	question := models.Question{Document: "answer-test-document", UserID: user.ID}
	if err := db.Create(&question).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.Answer{Question: question.ID, UserId: user.ID}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AnswerVersion{AnswerID: parent.ID, Content: "Parent answer"}).Error; err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		content   string
		anonymous bool
		parent    *uint
	}{
		{name: "plain text", content: "The answer content"},
		{name: "markdown", content: "# Risposta\n\nÈ una prova con **Markdown** e `fork()`: α ≤ β."},
		{name: "anonymous", content: "An anonymous answer", anonymous: true},
		{name: "reply", content: "A reply to the parent answer", parent: &parent.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(models.PostAnswerRequest{
				Question: question.ID, Parent: tc.parent, Content: tc.content, Anonymous: tc.anonymous,
			})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/answers", bytes.NewReader(body))
			req = req.WithContext(context.WithValue(req.Context(), middleware.AuthContextKey, user))
			res := httptest.NewRecorder()
			PostAnswerHandler(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
			}

			var response Answer
			if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			var version models.AnswerVersion
			if err := db.Where("answer_id = ?", response.ID).First(&version).Error; err != nil {
				t.Fatal(err)
			}
			if version.Content != tc.content {
				t.Errorf("stored content = %q, want %q", version.Content, tc.content)
			}
			if response.Content != version.Content {
				t.Errorf("response content = %q, want stored content %q", response.Content, version.Content)
			}
			wantUser := user.Username
			if tc.anonymous {
				wantUser = storedUser.Alias
			}
			if response.User != wantUser {
				t.Errorf("response user = %q, want %q", response.User, wantUser)
			}
		})
	}
}
