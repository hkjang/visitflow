package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const visitTypeSelect = `SELECT id,code,name,description,requires_nda,requires_safety_briefing,requires_vehicle,requires_equipment,requires_approval,active,sort_order FROM visit_types`

func scanVisitType(row pgx.Row) (VisitType, error) {
	var item VisitType
	err := row.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &item.RequiresNDA, &item.RequiresSafetyBriefing,
		&item.RequiresVehicle, &item.RequiresEquipment, &item.RequiresApproval, &item.Active, &item.SortOrder)
	return item, err
}

func (s *Server) visitTypes(ctx context.Context, activeOnly bool) ([]VisitType, error) {
	query := visitTypeSelect + ` WHERE ($1=false OR active) ORDER BY sort_order,name`
	rows, err := s.db.Query(ctx, query, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []VisitType{}
	for rows.Next() {
		item, scanErr := scanVisitType(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) listVisitTypes(w http.ResponseWriter, r *http.Request) {
	items, err := s.visitTypes(r.Context(), false)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) upsertVisitType(w http.ResponseWriter, r *http.Request) {
	var in VisitType
	if !decodeJSON(w, r, &in) {
		return
	}
	if id := chi.URLParam(r, "visitTypeID"); id != "" {
		in.ID = id
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Code == "" || in.Name == "" {
		writeError(w, http.StatusBadRequest, "required_fields", "방문 유형 코드와 이름은 필수입니다")
		return
	}
	if len(in.Code) > 32 || len(in.Name) > 100 {
		writeError(w, http.StatusBadRequest, "value_too_long", "코드는 32자, 이름은 100자 이내여야 합니다")
		return
	}
	if in.SortOrder < 0 || in.SortOrder > 10000 {
		writeError(w, http.StatusBadRequest, "invalid_sort_order", "정렬 순서는 0~10000이어야 합니다")
		return
	}
	if in.ID == "" {
		in.ID = newID()
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO visit_types(id,code,name,description,requires_nda,requires_safety_briefing,requires_vehicle,requires_equipment,requires_approval,active,sort_order)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT(id) DO UPDATE SET code=EXCLUDED.code,name=EXCLUDED.name,description=EXCLUDED.description,
			requires_nda=EXCLUDED.requires_nda,requires_safety_briefing=EXCLUDED.requires_safety_briefing,
			requires_vehicle=EXCLUDED.requires_vehicle,requires_equipment=EXCLUDED.requires_equipment,
			requires_approval=EXCLUDED.requires_approval,active=EXCLUDED.active,sort_order=EXCLUDED.sort_order,updated_at=now()`,
		in.ID, in.Code, in.Name, in.Description, in.RequiresNDA, in.RequiresSafetyBriefing, in.RequiresVehicle, in.RequiresEquipment, in.RequiresApproval, in.Active, in.SortOrder)
	if err != nil {
		if strings.Contains(err.Error(), "visit_types_code_key") {
			writeError(w, http.StatusConflict, "duplicate_code", "이미 사용 중인 방문 유형 코드입니다")
			return
		}
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "visit_type.upsert", "visit_type", in.ID, r.RemoteAddr, map[string]any{"code": in.Code, "name": in.Name})
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) deleteVisitType(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "visitTypeID")
	// Visits keep their historical type, so deletion is a deactivation.
	tag, err := s.db.Exec(r.Context(), `UPDATE visit_types SET active=false,updated_at=now() WHERE id=$1 AND active`, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		notFoundOrServer(w, pgx.ErrNoRows)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "visit_type.disable", "visit_type", id, r.RemoteAddr, nil)
	w.WriteHeader(http.StatusNoContent)
}

// visitChecklistError validates the requester's acknowledgements against the
// selected visit type and returns the checklist that is stored on the visit.
func (s *Server) applyVisitType(ctx context.Context, in *VisitInput) (VisitType, []byte, error) {
	empty, _ := json.Marshal(map[string]bool{})
	if strings.TrimSpace(in.VisitTypeID) == "" {
		return VisitType{}, empty, nil
	}
	visitType, err := scanVisitType(s.db.QueryRow(ctx, visitTypeSelect+` WHERE id=$1`, in.VisitTypeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return VisitType{}, empty, visitError{400, "unknown_visit_type", "선택한 방문 유형을 찾을 수 없습니다"}
	}
	if err != nil {
		return VisitType{}, empty, err
	}
	if !visitType.Active {
		return VisitType{}, empty, visitError{400, "inactive_visit_type", "비활성화된 방문 유형입니다"}
	}
	checklist := map[string]bool{}
	if visitType.RequiresNDA {
		if !in.Checklist["nda"] {
			return visitType, empty, visitError{400, "checklist_required", "이 방문 유형은 보안서약 확인이 필요합니다"}
		}
		checklist["nda"] = true
	}
	if visitType.RequiresSafetyBriefing {
		if !in.Checklist["safetyBriefing"] {
			return visitType, empty, visitError{400, "checklist_required", "이 방문 유형은 안전교육 이수 확인이 필요합니다"}
		}
		checklist["safetyBriefing"] = true
	}
	if visitType.RequiresVehicle {
		for _, visitor := range in.Visitors {
			if strings.TrimSpace(visitor.Vehicle) == "" {
				return visitType, empty, visitError{400, "vehicle_required", "이 방문 유형은 방문자별 차량번호가 필요합니다"}
			}
		}
		checklist["vehicle"] = true
	}
	if visitType.RequiresEquipment {
		for _, visitor := range in.Visitors {
			if len(visitor.Equipment) == 0 {
				return visitType, empty, visitError{400, "equipment_required", "이 방문 유형은 방문자별 반입 장비 신고가 필요합니다"}
			}
		}
		checklist["equipment"] = true
	}
	encoded, err := json.Marshal(checklist)
	if err != nil {
		return visitType, empty, err
	}
	return visitType, encoded, nil
}
