package proposal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cartabinaria/auth"
	"github.com/cartabinaria/auth/pkg/middleware"
	"github.com/cartabinaria/polleg/api"
	"github.com/cartabinaria/polleg/models"
	"github.com/cartabinaria/polleg/util"
	"github.com/google/uuid"
	"github.com/kataras/muxie"
)

func TestUpdateProposalCoordinates(t *testing.T) {
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
	// Keep the schema on the connection used by the handlers.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	schema := "proposal_coords_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	owner := models.User{Username: "proposal-owner", Alias: "ProposalOwner"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	other := models.Proposal{DocumentID: "other-document", Start: 50, End: 60, UserID: owner.ID}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&other, other.ID).Error; err != nil {
		t.Fatal(err)
	}

	mux := muxie.NewMux()
	mux.Handle("/proposals/:id", muxie.Methods().
		Handle("GET", http.HandlerFunc(GetProposalByIdHandler)).
		Handle("PATCH", http.HandlerFunc(UpdateProposalByIdHandler)))
	for _, tc := range []struct {
		name       string
		role       auth.Role
		start, end uint32
	}{
		{name: "member changes both coordinates", role: auth.RoleMember, start: 30, end: 40},
		{name: "admin sets zero start", role: auth.RoleAdmin, start: 0, end: 15},
		{name: "member changes only end", role: auth.RoleMember, start: 10, end: 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := models.Proposal{DocumentID: "coordinate-document", DocumentPath: "fixture/exam.pdf", Start: 10, End: 20, UserID: owner.ID}
			if err := db.Create(&original).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&original, original.ID).Error; err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(UpdateProposalRequest{Coords: api.Coord{Start: tc.start, End: tc.end}})
			if err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/proposals/%d", original.ID)
			for _, method := range []string{http.MethodPatch, http.MethodGet} {
				req := httptest.NewRequest(method, path, bytes.NewReader(body))
				req = req.WithContext(context.WithValue(req.Context(), middleware.AuthContextKey, auth.User{ID: owner.ID, Username: owner.Username, Role: tc.role}))
				res := httptest.NewRecorder()
				mux.ServeHTTP(res, req)
				if res.Code != http.StatusOK {
					t.Fatalf("%s status = %d, want 200: %s", method, res.Code, res.Body.String())
				}
				var got Proposal
				if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Start != tc.start || got.End != tc.end {
					t.Errorf("%s coordinates = (%d,%d), want (%d,%d)", method, got.Start, got.End, tc.start, tc.end)
				}
				if got.ID != original.ID || got.DocumentID != original.DocumentID || got.DocumentPath != original.DocumentPath || got.User != owner.Username {
					t.Errorf("%s changed proposal identity or metadata: %+v", method, got)
				}
			}
			var stored models.Proposal
			if err := db.First(&stored, original.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Start != tc.start || stored.End != tc.end {
				t.Errorf("stored coordinates = (%d,%d), want (%d,%d)", stored.Start, stored.End, tc.start, tc.end)
			}
			if stored.DocumentID != original.DocumentID || stored.DocumentPath != original.DocumentPath || stored.UserID != original.UserID || stored.DeletedAt.Valid || !stored.CreatedAt.Equal(original.CreatedAt) {
				t.Errorf("changed stored proposal metadata: %+v", stored)
			}
		})
	}
	var untouched models.Proposal
	if err := db.First(&untouched, other.ID).Error; err != nil {
		t.Fatal(err)
	}
	if untouched.Start != other.Start || untouched.End != other.End || !untouched.UpdatedAt.Equal(other.UpdatedAt) {
		t.Errorf("changed unrelated proposal: %+v", untouched)
	}
}
