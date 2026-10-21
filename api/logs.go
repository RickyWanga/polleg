package api

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/cartabinaria/auth/pkg/httputil"
	"github.com/cartabinaria/auth/pkg/middleware"
	"github.com/cartabinaria/polleg/models"
	"github.com/cartabinaria/polleg/util"
)

const SYSTEM_USER_ID = 0

type Log struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`    // Created, Updated, Deleted
	ItemType  string    `json:"item_type"` // Answer, Image, ecc.
	ItemID    string    `json:"item_id"`

	UserID        uint   `json:"-"`
	Username      string `json:"username"`
	UserAvatarURL string `json:"user_avatar_url"`
}

// @Summary		Get system logs
// @Description	Get system logs (admin only)
// @Tags			admin
// @Produce		json
// @Success		200	{array}		Log
// @Failure		400	{object}	httputil.ApiError
// @Failure		403	{object}	httputil.ApiError
// @Failure		405	{object}	httputil.ApiError
// @Router			/logs [get]
func LogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "invalid method")
		return
	}

	if !middleware.GetAdmin(r) {
		httputil.WriteError(w, http.StatusForbidden, "you are not admin")
		return
	}

	db := util.GetDb()

	var logs []Log

	// Images
	var images []models.Image
	if err := db.Unscoped().Find(&images).Error; err != nil {
		slog.With("err", err).Error("error while getting images from DB")
		httputil.WriteError(w, http.StatusBadRequest, "could not get logs")
		return
	}
	logs = append(logs, imagesToLogs(images)...)

	// Answers
	var answers []models.Answer
	if err := db.Unscoped().Find(&answers).Error; err != nil {
		slog.With("err", err).Error("error while getting answers from DB")
		httputil.WriteError(w, http.StatusBadRequest, "could not get logs")
		return
	}
	logs = append(logs, answersToLogs(answers)...)

	// Answers versions
	var answerVersions []models.AnswerVersion
	if err := db.Find(&answerVersions).Error; err != nil {
		slog.With("err", err).Error("error while getting answer versions from DB")
		httputil.WriteError(w, http.StatusBadRequest, "could not get logs")
		return
	}
	logs = append(logs, answersVersionsToLogs(answerVersions, answers)...)

	// Users
	var users []models.User
	if err := db.Unscoped().Find(&users).Error; err != nil {
		slog.With("err", err).Error("error while getting users from DB")
		httputil.WriteError(w, http.StatusBadRequest, "could not get logs")
		return
	}
	logs = append(logs, usersToLogs(users)...)

	// Proposals
	var proposals []models.Proposal
	if err := db.Unscoped().Find(&proposals).Error; err != nil {
		slog.With("err", err).Error("error while getting proposals from DB")
		httputil.WriteError(w, http.StatusBadRequest, "could not get logs")
		return
	}
	logs = append(logs, proposalsToLogs(proposals)...)

	// Map user IDs to usernames and avatar URLs
	userMap := make(map[uint]string, len(users))
	for _, u := range users {
		userMap[u.ID] = u.Username
	}

	for i := range logs {
		if logs[i].UserID == SYSTEM_USER_ID {
			logs[i].Username = "system"
			logs[i].UserAvatarURL = ""
			continue
		} else if username, ok := userMap[logs[i].UserID]; ok {
			logs[i].Username = username
			logs[i].UserAvatarURL = util.GetPublicAvatarURL(logs[i].UserID)
		}
	}

	// Sort logs by timestamp descending
	slices.SortFunc(logs, func(a, b Log) int {
		return b.Timestamp.Compare(a.Timestamp)
	})

	httputil.WriteData(w, http.StatusOK, logs)
}

func imagesToLogs(images []models.Image) []Log {
	logs := make([]Log, 0, len(images))
	for _, img := range images {
		logs = append(logs, Log{
			Timestamp: img.CreatedAt,
			Action:    "created",
			ItemType:  "image",
			ItemID:    img.ID,

			UserID:        img.UserID,
			Username:      "",
			UserAvatarURL: "",
		})

		if img.DeletedAt.Valid {
			logs = append(logs, Log{
				Timestamp: img.DeletedAt.Time,
				Action:    "deleted",
				ItemType:  "image",
				ItemID:    img.ID,

				UserID:        SYSTEM_USER_ID,
				Username:      "",
				UserAvatarURL: "",
			})
		}
	}
	return logs
}

func answersToLogs(answers []models.Answer) []Log {
	logs := make([]Log, 0, len(answers))
	for _, ans := range answers {
		logs = append(logs, Log{
			Timestamp: ans.CreatedAt,
			Action:    "created",
			ItemType:  "answer",
			ItemID:    strconv.FormatUint(uint64(ans.ID), 10),

			UserID:        ans.UserId,
			Username:      "",
			UserAvatarURL: "",
		})

		if ans.DeletedAt.Valid {
			l := Log{
				Timestamp: ans.DeletedAt.Time,
				Action:    "deleted",
				ItemType:  "answer",
				ItemID:    strconv.FormatUint(uint64(ans.ID), 10),
			}

			if ans.State == models.AnswerStateDeletedByAdmin {
				l.Username = "administrator"
			} else {
				l.UserID = ans.UserId
			}

			logs = append(logs, l)
		}
	}

	return logs
}

func answersVersionsToLogs(answersVersions []models.AnswerVersion, answers []models.Answer) []Log {
	logs := make([]Log, 0, len(answersVersions))
	answerMap := make(map[uint]uint, len(answers))
	for _, ans := range answers {
		answerMap[ans.ID] = ans.UserId
	}

	for _, av := range answersVersions {
		logs = append(logs, Log{
			Timestamp: av.CreatedAt,
			Action:    "modified",
			ItemType:  "answer-content",
			ItemID:    strconv.FormatUint(uint64(av.AnswerID), 10),

			UserID:        answerMap[av.AnswerID],
			Username:      "",
			UserAvatarURL: "",
		})
	}

	return logs
}

func usersToLogs(users []models.User) []Log {
	logs := make([]Log, 0, len(users)*2)
	for _, u := range users {
		logs = append(logs, Log{
			Timestamp: u.CreatedAt,
			Action:    "created",
			ItemType:  "user",
			ItemID:    strconv.FormatUint(uint64(u.ID), 10),

			UserID:        u.ID,
			Username:      "",
			UserAvatarURL: "",
		})

		if u.DeletedAt.Valid {
			logs = append(logs, Log{
				Timestamp: u.DeletedAt.Time,
				Action:    "deleted",
				ItemType:  "user",
				ItemID:    strconv.FormatUint(uint64(u.ID), 10),

				UserID:        SYSTEM_USER_ID,
				Username:      "",
				UserAvatarURL: "",
			})
		}

		if u.Banned {
			logs = append(logs, Log{
				Timestamp:     *u.BannedAt,
				Action:        "banned",
				ItemType:      "user",
				ItemID:        strconv.FormatUint(uint64(u.ID), 10),
				UserID:        SYSTEM_USER_ID,
				Username:      "",
				UserAvatarURL: "",
			})
		}
	}
	return logs
}

func proposalsToLogs(proposals []models.Proposal) []Log {
	logs := make([]Log, 0, len(proposals)*2)
	for _, p := range proposals {
		logs = append(logs, Log{
			Timestamp: p.CreatedAt,
			Action:    "created",
			ItemType:  "proposal",
			ItemID:    strconv.FormatUint(uint64(p.ID), 10),

			UserID:        p.UserID,
			Username:      "",
			UserAvatarURL: "",
		})

		if p.DeletedAt.Valid {
			logs = append(logs, Log{
				Timestamp: p.DeletedAt.Time,
				Action:    "deleted",
				ItemType:  "proposal",
				ItemID:    strconv.FormatUint(uint64(p.ID), 10),

				UserID:        SYSTEM_USER_ID,
				Username:      "",
				UserAvatarURL: "",
			})
		}
	}
	return logs
}
