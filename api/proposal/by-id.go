package proposal

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/cartabinaria/auth/pkg/httputil"
	"github.com/cartabinaria/auth/pkg/middleware"
	"github.com/cartabinaria/polleg/api"
	"github.com/cartabinaria/polleg/models"
	"github.com/cartabinaria/polleg/util"
	"github.com/kataras/muxie"
	"golang.org/x/exp/slog"
)

type UpdateProposalRequest struct {
	Coords api.Coord `json:"coords"`
}

// @Summary		Get proposal by id
// @Description	Get a proposal given its ID
// @Tags			proposal
// @Param			id	path	string	true	"Proposal id"
// @Produce		json
// @Success		200	{object}	Proposal
// @Failure		400	{object}	httputil.ApiError
// @Router			/proposals/{id} [get]
func GetProposalByIdHandler(res http.ResponseWriter, req *http.Request) {
	if !middleware.GetMember(req) && !middleware.GetAdmin(req) {
		httputil.WriteError(res, http.StatusForbidden, "you are not a member or admin")
		return
	}

	db := util.GetDb()
	proposalID := muxie.GetParam(res, "id")
	propID, err := strconv.ParseUint(proposalID, 10, 0)
	if err != nil {
		httputil.WriteError(res, http.StatusBadRequest, "invalid answer id")
		return
	}

	var props models.Proposal
	if err := db.Where("id = ?", propID).Take(&props).Error; err != nil {
		httputil.WriteError(res, http.StatusNotFound, "Not found")
		return
	}

	httputil.WriteData(res, http.StatusOK, dbProposalToProposal(db, &props))
}

// @Summary		Delete a proposal
// @Description	Given a proposal ID, delete the proposal
// @Tags			proposal
// @Param			id	path	string	true	"Proposal id"
// @Produce		json
// @Success		200	{object}	nil
// @Failure		400	{object}	httputil.ApiError
// @Router			/proposals/{id} [delete]
func DeleteProposalByIdHandler(res http.ResponseWriter, req *http.Request) {
	if !middleware.GetMember(req) && !middleware.GetAdmin(req) {
		httputil.WriteError(res, http.StatusForbidden, "you are not a member or admin")
		return
	}

	db := util.GetDb()
	proposalID := muxie.GetParam(res, "id")
	propID, err := strconv.ParseUint(proposalID, 10, 0)
	if err != nil {
		httputil.WriteError(res, http.StatusBadRequest, "invalid answer id")
		return
	}

	if err := db.Delete(&models.Proposal{}, propID).Error; err != nil {
		httputil.WriteError(res, http.StatusInternalServerError, "db query failed")
		return
	}

	res.WriteHeader(http.StatusNoContent)
}

// @Summary		Update a proposal
// @Description	Given a proposal ID, update the proposal
// @Tags			proposal
// @Param			id	path	string	true	"Proposal id"
// @Produce		json
// @Success		200	{object}	Proposal
// @Failure		400	{object}	httputil.ApiError
// @Router			/proposals/{id} [patch]
func UpdateProposalByIdHandler(res http.ResponseWriter, req *http.Request) {
	if !middleware.GetMember(req) && !middleware.GetAdmin(req) {
		httputil.WriteError(res, http.StatusForbidden, "you are not a member or admin")
		return
	}

	var data UpdateProposalRequest
	if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
		httputil.WriteError(res, http.StatusBadRequest, "couldn't decode body")
		return
	}

	db := util.GetDb()
	proposalID := muxie.GetParam(res, "id")
	propID, err := strconv.ParseUint(proposalID, 10, 0)
	if err != nil {
		httputil.WriteError(res, http.StatusBadRequest, "invalid answer id")
		return
	}

	var proposal models.Proposal
	if err := db.Where("id = ?", propID).Take(&proposal).Error; err != nil {
		httputil.WriteError(res, http.StatusNotFound, "Not found")
		return
	}

	proposal.Start = data.Coords.Start
	proposal.End = data.Coords.End

	if err := db.Save(&proposal).Error; err != nil {
		slog.Error("error while updating proposal", "proposal", proposal, "err", err)
		httputil.WriteError(res, http.StatusInternalServerError, "error while updating answer")
		return
	}

	httputil.WriteData(res, http.StatusOK, dbProposalToProposal(db, &proposal))
}
