package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cartabinaria/auth"
	"github.com/cartabinaria/auth/pkg/middleware"
	"github.com/cartabinaria/polleg/api"
	"github.com/cartabinaria/polleg/models"
	"github.com/cartabinaria/polleg/util"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestLogsHandlerAccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		role   auth.Role
		status int
	}{
		{name: "unauthenticated", method: http.MethodGet, status: http.StatusForbidden},
		{name: "user", method: http.MethodGet, role: auth.RoleUser, status: http.StatusForbidden},
		{name: "member", method: http.MethodGet, role: auth.RoleMember, status: http.StatusForbidden},
		{name: "invalid method", method: http.MethodPost, role: auth.RoleAdmin, status: http.StatusMethodNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/logs", nil)
			if tc.role != "" {
				req = req.WithContext(context.WithValue(req.Context(), middleware.AuthContextKey, auth.User{ID: 123, Role: tc.role}))
			}
			res := httptest.NewRecorder()
			api.LogsHandler(res, req)
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", res.Code, tc.status, res.Body.String())
			}
		})
	}
}

func TestLogsHandlerRetainedHistory(t *testing.T) {
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
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	schema := "logs_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

	createdAt := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)
	owner := models.User{Username: "history-owner", Alias: "HistoryOwner", CreatedAt: createdAt}
	activeUser := models.User{Username: "active-user", Alias: "ActiveUser", CreatedAt: createdAt.Add(time.Second)}
	for _, user := range []*models.User{&owner, &activeUser} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	question := models.Question{Document: "history-document", UserID: owner.ID}
	if err := db.Create(&question).Error; err != nil {
		t.Fatal(err)
	}
	answer := models.Answer{Question: question.ID, UserId: owner.ID, CreatedAt: createdAt.Add(2 * time.Second)}
	image := models.Image{ID: uuid.NewString(), UserID: owner.ID, Size: 8, CreatedAt: createdAt.Add(3 * time.Second)}
	proposal := models.Proposal{DocumentID: "history-proposal", UserID: owner.ID, CreatedAt: createdAt.Add(4 * time.Second)}
	items := []struct {
		record    any
		itemType  string
		itemID    string
		createdAt time.Time
		deletedAt *gorm.DeletedAt
	}{
		{&owner, "user", fmt.Sprint(owner.ID), owner.CreatedAt, &owner.DeletedAt},
		{&answer, "answer", "", answer.CreatedAt, &answer.DeletedAt},
		{&image, "image", image.ID, image.CreatedAt, &image.DeletedAt},
		{&proposal, "proposal", "", proposal.CreatedAt, &proposal.DeletedAt},
	}
	for _, item := range items[1:] {
		if err := db.Create(item.record).Error; err != nil {
			t.Fatal(err)
		}
	}
	items[1].itemID = fmt.Sprint(answer.ID)
	items[3].itemID = fmt.Sprint(proposal.ID)
	versions := []models.AnswerVersion{
		{AnswerID: answer.ID, Content: "Original retained content", CreatedAt: createdAt.Add(5 * time.Second)},
		{AnswerID: answer.ID, Content: "Edited retained content", CreatedAt: createdAt.Add(6 * time.Second)},
	}
	if err := db.Create(&versions).Error; err != nil {
		t.Fatal(err)
	}

	want := []api.Log{{Timestamp: activeUser.CreatedAt, Action: "created", ItemType: "user", ItemID: fmt.Sprint(activeUser.ID), Username: activeUser.Username, UserAvatarURL: util.GetPublicAvatarURL(activeUser.ID)}}
	for _, item := range items {
		want = append(want, api.Log{Timestamp: item.createdAt, Action: "created", ItemType: item.itemType, ItemID: item.itemID, Username: owner.Username, UserAvatarURL: util.GetPublicAvatarURL(owner.ID)})
	}
	for _, version := range versions {
		want = append(want, api.Log{Timestamp: version.CreatedAt, Action: "modified", ItemType: "answer-content", ItemID: fmt.Sprint(answer.ID), Username: owner.Username, UserAvatarURL: util.GetPublicAvatarURL(owner.ID)})
	}
	assertLogs := func(t *testing.T) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/logs", nil)
		req = req.WithContext(context.WithValue(req.Context(), middleware.AuthContextKey, auth.User{ID: 123, Role: auth.RoleAdmin}))
		res := httptest.NewRecorder()
		api.LogsHandler(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", res.Code, res.Body.String())
		}
		var got []api.Log
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		for i := range got {
			got[i].Timestamp = got[i].Timestamp.UTC()
		}
		for i := range want {
			want[i].Timestamp = want[i].Timestamp.UTC()
		}
		slices.SortFunc(want, func(a, b api.Log) int { return b.Timestamp.Compare(a.Timestamp) })
		if !reflect.DeepEqual(got, want) {
			t.Errorf("logs = %+v, want %+v", got, want)
		}
		if strings.Contains(res.Body.String(), "retained content") || strings.Contains(res.Body.String(), `"user_id"`) {
			t.Errorf("logs exposed content or internal user IDs: %s", res.Body.String())
		}
	}
	t.Run("before deletion", assertLogs)

	// These are GORM retention fixtures, not answer/user HTTP deletion semantics.
	for _, item := range items {
		if err := db.Delete(item.record).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Unscoped().First(item.record).Error; err != nil {
			t.Fatal(err)
		}
		if !item.deletedAt.Valid {
			t.Fatalf("%s %s was not retained with deleted_at", item.itemType, item.itemID)
		}
		var visible int64
		if err := db.Model(item.record).Where("id = ?", item.itemID).Count(&visible).Error; err != nil {
			t.Fatal(err)
		}
		if visible != 0 {
			t.Fatalf("%s %s remains in the normal scope", item.itemType, item.itemID)
		}
		username, avatar := "system", ""
		if item.itemType == "answer" {
			username, avatar = owner.Username, util.GetPublicAvatarURL(owner.ID)
		}
		want = append(want, api.Log{Timestamp: item.deletedAt.Time, Action: "deleted", ItemType: item.itemType, ItemID: item.itemID, Username: username, UserAvatarURL: avatar})
	}
	var retained []models.AnswerVersion
	if err := db.Where("answer_id = ?", answer.ID).Order("id").Find(&retained).Error; err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(retained, versions, func(a, b models.AnswerVersion) bool {
		return a.ID == b.ID && a.AnswerID == b.AnswerID && a.Content == b.Content && a.CreatedAt.Equal(b.CreatedAt)
	}) {
		t.Errorf("retained versions = %+v, want %+v", retained, versions)
	}
	t.Run("after deletion", assertLogs)
}
