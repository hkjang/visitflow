package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxTemplateFrequentVisitors = 100

const touchFrequentVisitorsByIDsSQL = `WITH selected AS MATERIALIZED (
	SELECT visitor.id
	FROM frequent_visitors visitor
	WHERE visitor.user_id=$1 AND visitor.id=ANY($2::text[])
	ORDER BY visitor.id
	FOR UPDATE OF visitor
)
UPDATE frequent_visitors visitor
SET last_used_at=now()
FROM selected
WHERE visitor.id=selected.id AND visitor.user_id=$1`

const touchTemplateFrequentVisitorsSQL = `WITH selected AS MATERIALIZED (
	SELECT visitor.id
	FROM frequent_visitors visitor
	JOIN visit_template_frequent_visitors link ON link.frequent_visitor_id=visitor.id
	WHERE link.template_id=$2 AND link.user_id=$1 AND visitor.user_id=$1
	ORDER BY visitor.id
	FOR UPDATE OF visitor
)
UPDATE frequent_visitors visitor
SET last_used_at=now()
FROM selected
WHERE visitor.id=selected.id AND visitor.user_id=$1`

type frequentVisitorRowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type frequentVisitorExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type visitTemplateInput struct {
	Name               string         `json:"name"`
	Payload            map[string]any `json:"payload"`
	FrequentVisitorIDs []string       `json:"frequentVisitorIds,omitempty"`
}

func validateFrequentVisitorInput(in VisitorInput) string {
	if strings.TrimSpace(in.Name) == "" || len(normalizePhone(in.Phone)) < 7 || !in.Consent {
		return "방문자 이름, 휴대전화, 개인정보 동의는 필수입니다"
	}
	if len(in.Equipment) > 100 {
		return "기본 반입 장비는 최대 100개까지 등록할 수 있습니다"
	}
	return ""
}

func validateFrequentVisitorIDs(ids []string) string {
	if len(ids) > maxTemplateFrequentVisitors {
		return "템플릿에는 자주 방문자를 최대 100명까지 추가할 수 있습니다"
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return "자주 방문자 선택값을 확인하세요"
		}
		if _, exists := seen[id]; exists {
			return "같은 자주 방문자를 템플릿에 중복 추가할 수 없습니다"
		}
		seen[id] = struct{}{}
	}
	return ""
}

func validateVisitTemplatePayload(payload map[string]any) string {
	for key, value := range payload {
		switch key {
		case "purpose", "placeDetail", "company":
			if _, ok := value.(string); !ok {
				return "템플릿의 방문 목적, 세부 장소와 회사명은 문자열이어야 합니다"
			}
		default:
			return "템플릿 내용에는 방문 목적, 세부 장소와 회사명만 저장할 수 있습니다"
		}
	}
	return ""
}

func (s *Server) listFrequentVisitors(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	items, err := s.frequentVisitorItems(r.Context(), u.ID, "")
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.audit(r.Context(), u.ID, "frequent_visitor.list", "frequent_visitor", "", clientIP(r), map[string]int{"count": len(items)})
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) frequentVisitorItems(ctx context.Context, userID, templateID string) ([]map[string]any, error) {
	return s.frequentVisitorItemsWithQuerier(ctx, s.db, userID, templateID)
}

func (s *Server) frequentVisitorItemsWithQuerier(ctx context.Context, querier frequentVisitorRowsQuerier, userID, templateID string) ([]map[string]any, error) {
	rows, err := querier.Query(ctx, `SELECT fv.id,fv.name_encrypted,fv.phone_encrypted,COALESCE(fv.email_encrypted,''),
		COALESCE(fv.company,''),COALESCE(fv.title,''),COALESCE(fv.vehicle_encrypted,''),fv.equipment,
		fv.consented_at,fv.created_at,fv.updated_at,
		(SELECT count(*) FROM visit_template_frequent_visitors links WHERE links.frequent_visitor_id=fv.id),selected.sort_order
		FROM frequent_visitors fv
		LEFT JOIN visit_template_frequent_visitors selected ON selected.frequent_visitor_id=fv.id AND selected.template_id=NULLIF($2,'')
		WHERE fv.user_id=$1 AND ($2='' OR selected.template_id IS NOT NULL)
		ORDER BY selected.sort_order NULLS LAST,fv.updated_at DESC,fv.id`, userID, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, nameEnc, phoneEnc, emailEnc, company, title, vehicleEnc string
		var equipmentJSON []byte
		var consented, created, updated time.Time
		var templateCount int
		var sortOrder *int
		if err := rows.Scan(&id, &nameEnc, &phoneEnc, &emailEnc, &company, &title, &vehicleEnc, &equipmentJSON, &consented, &created, &updated, &templateCount, &sortOrder); err != nil {
			return nil, err
		}
		equipment := []string{}
		if err := json.Unmarshal(equipmentJSON, &equipment); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"id": id, "name": s.decryptOptional(nameEnc), "phone": s.decryptOptional(phoneEnc),
			"email": s.decryptOptional(emailEnc), "company": company, "title": title,
			"vehicle": s.decryptOptional(vehicleEnc), "equipment": equipment, "consent": true,
			"consentedAt": consented, "createdAt": created, "updatedAt": updated, "templateCount": templateCount,
		})
	}
	return items, rows.Err()
}

func (s *Server) createFrequentVisitor(w http.ResponseWriter, r *http.Request) {
	var in VisitorInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if message := validateFrequentVisitorInput(in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_frequent_visitor", message)
		return
	}
	u, _ := userFrom(r)
	id := newID()
	nameEnc, phoneEnc, emailEnc, vehicleEnc, equipment, ok := s.prepareFrequentVisitor(w, in)
	if !ok {
		return
	}
	err := s.db.QueryRow(r.Context(), `INSERT INTO frequent_visitors(
		id,user_id,name_encrypted,phone_encrypted,phone_hash,email_encrypted,company,title,vehicle_encrypted,equipment,consented_at
	) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,now())
	ON CONFLICT(user_id,phone_hash) DO NOTHING RETURNING id`, id, u.ID, nameEnc, phoneEnc,
		s.keys.Digest("frequent-phone:"+normalizePhone(in.Phone)), emailEnc, strings.TrimSpace(in.Company),
		strings.TrimSpace(in.Title), vehicleEnc, equipment).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "frequent_visitor_exists", "같은 휴대전화의 자주 방문자가 이미 등록되어 있습니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "frequent_visitor.create", "frequent_visitor", id, clientIP(r), nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) updateFrequentVisitor(w http.ResponseWriter, r *http.Request) {
	var in VisitorInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if message := validateFrequentVisitorInput(in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_frequent_visitor", message)
		return
	}
	u, _ := userFrom(r)
	id := chi.URLParam(r, "frequentVisitorID")
	nameEnc, phoneEnc, emailEnc, vehicleEnc, equipment, ok := s.prepareFrequentVisitor(w, in)
	if !ok {
		return
	}
	err := s.db.QueryRow(r.Context(), `UPDATE frequent_visitors SET
		name_encrypted=$3,phone_encrypted=$4,phone_hash=$5,email_encrypted=NULLIF($6,''),company=NULLIF($7,''),
		title=NULLIF($8,''),vehicle_encrypted=NULLIF($9,''),equipment=$10,consented_at=now(),last_used_at=now(),updated_at=now()
		WHERE id=$1 AND user_id=$2 RETURNING id`, id, u.ID, nameEnc, phoneEnc,
		s.keys.Digest("frequent-phone:"+normalizePhone(in.Phone)), emailEnc, strings.TrimSpace(in.Company),
		strings.TrimSpace(in.Title), vehicleEnc, equipment).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "자주 방문자를 찾을 수 없습니다")
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		writeError(w, http.StatusConflict, "frequent_visitor_exists", "같은 휴대전화의 자주 방문자가 이미 등록되어 있습니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "frequent_visitor.update", "frequent_visitor", id, clientIP(r), nil)
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) prepareFrequentVisitor(w http.ResponseWriter, in VisitorInput) (nameEnc, phoneEnc, emailEnc, vehicleEnc string, equipment []byte, ok bool) {
	var err error
	nameEnc, err = s.keys.Encrypt(strings.TrimSpace(in.Name))
	if err == nil {
		phoneEnc, err = s.keys.Encrypt(normalizePhone(in.Phone))
	}
	if err == nil {
		emailEnc, err = s.encryptOptional(in.Email)
	}
	if err == nil {
		vehicleEnc, err = s.encryptOptional(in.Vehicle)
	}
	if err == nil {
		equipment, err = json.Marshal(in.Equipment)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encryption_failed", "자주 방문자 정보를 보호해 저장하지 못했습니다")
		return "", "", "", "", nil, false
	}
	return nameEnc, phoneEnc, emailEnc, vehicleEnc, equipment, true
}

func (s *Server) deleteFrequentVisitor(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "frequentVisitorID")
	err := s.db.QueryRow(r.Context(), `DELETE FROM frequent_visitors WHERE id=$1 AND user_id=$2 RETURNING id`, id, u.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "자주 방문자를 찾을 수 없습니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "frequent_visitor.delete", "frequent_visitor", id, clientIP(r), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listVisitTemplates(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	rows, err := s.db.Query(r.Context(), `SELECT vt.id,vt.name,vt.payload,vt.created_at,vt.updated_at,
		COALESCE((SELECT array_agg(link.frequent_visitor_id ORDER BY link.sort_order,link.frequent_visitor_id)
			FROM visit_template_frequent_visitors link WHERE link.template_id=vt.id),ARRAY[]::text[])
		FROM visit_templates vt WHERE vt.user_id=$1 ORDER BY vt.updated_at DESC`, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		item, scanErr := scanVisitTemplate(rows)
		if scanErr != nil {
			notFoundOrServer(w, scanErr)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func scanVisitTemplate(row pgx.Row) (map[string]any, error) {
	var id, name string
	var payloadJSON []byte
	var created, updated time.Time
	var frequentVisitorIDs []string
	if err := row.Scan(&id, &name, &payloadJSON, &created, &updated, &frequentVisitorIDs); err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if len(payloadJSON) > 0 && string(payloadJSON) != "null" {
		if err := json.Unmarshal(payloadJSON, &payload); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"id": id, "name": name, "payload": payload, "frequentVisitorIds": frequentVisitorIDs,
		"frequentVisitorCount": len(frequentVisitorIDs), "createdAt": created, "updatedAt": updated,
	}, nil
}

func (s *Server) getVisitTemplate(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "templateID")
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := scanVisitTemplate(tx.QueryRow(r.Context(), `SELECT vt.id,vt.name,vt.payload,vt.created_at,vt.updated_at,ARRAY[]::text[]
		FROM visit_templates vt WHERE vt.id=$1 AND vt.user_id=$2 FOR SHARE OF vt`, id, u.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "방문 템플릿을 찾을 수 없습니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if err := touchTemplateFrequentVisitors(r.Context(), tx, u.ID, id); err != nil {
		notFoundOrServer(w, err)
		return
	}
	visitors, err := s.frequentVisitorItemsWithQuerier(r.Context(), tx, u.ID, id)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	frequentVisitorIDs := make([]string, 0, len(visitors))
	for _, visitor := range visitors {
		if visitorID, ok := visitor["id"].(string); ok {
			frequentVisitorIDs = append(frequentVisitorIDs, visitorID)
		}
	}
	item["frequentVisitorIds"] = frequentVisitorIDs
	item["frequentVisitorCount"] = len(frequentVisitorIDs)
	item["frequentVisitors"] = visitors
	if err := tx.Commit(r.Context()); err != nil {
		notFoundOrServer(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.audit(r.Context(), u.ID, "visit_template.view", "visit_template", id, clientIP(r), map[string]int{"frequentVisitorCount": len(visitors)})
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createVisitTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveVisitTemplate(w, r, "")
}

func (s *Server) updateVisitTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveVisitTemplate(w, r, chi.URLParam(r, "templateID"))
}

func (s *Server) saveVisitTemplate(w http.ResponseWriter, r *http.Request, templateID string) {
	var in visitTemplateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name_required", "템플릿 이름은 필수입니다")
		return
	}
	if message := validateFrequentVisitorIDs(in.FrequentVisitorIDs); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_frequent_visitors", message)
		return
	}
	if in.Payload == nil {
		in.Payload = map[string]any{}
	}
	if message := validateVisitTemplatePayload(in.Payload); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_payload", message)
		return
	}
	payload, err := json.Marshal(in.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "템플릿 내용을 확인하세요")
		return
	}
	u, _ := userFrom(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	created := templateID == ""
	if created {
		templateID = newID()
		_, err = tx.Exec(r.Context(), `INSERT INTO visit_templates(id,user_id,name,payload) VALUES($1,$2,$3,$4)`, templateID, u.ID, in.Name, payload)
	} else {
		err = tx.QueryRow(r.Context(), `UPDATE visit_templates SET name=$3,payload=$4,updated_at=now()
			WHERE id=$1 AND user_id=$2 RETURNING id`, templateID, u.ID, in.Name, payload).Scan(&templateID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "방문 템플릿을 찾을 수 없습니다")
			return
		}
	}
	if err == nil {
		err = replaceTemplateFrequentVisitors(r.Context(), tx, templateID, u.ID, in.FrequentVisitorIDs)
	}
	if errors.Is(err, errInvalidFrequentVisitorSelection) {
		writeError(w, http.StatusBadRequest, "invalid_frequent_visitors", "본인이 등록한 자주 방문자만 템플릿에 추가할 수 있습니다")
		return
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	action := "visit_template.update"
	status := http.StatusOK
	if created {
		action = "visit_template.create"
		status = http.StatusCreated
	}
	s.audit(r.Context(), u.ID, action, "visit_template", templateID, clientIP(r), map[string]int{"frequentVisitorCount": len(in.FrequentVisitorIDs)})
	writeJSON(w, status, map[string]string{"id": templateID})
}

var errInvalidFrequentVisitorSelection = errors.New("invalid frequent visitor selection")

func replaceTemplateFrequentVisitors(ctx context.Context, tx pgx.Tx, templateID, userID string, ids []string) error {
	if err := touchFrequentVisitorIDs(ctx, tx, userID, ids); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM visit_template_frequent_visitors WHERE template_id=$1`, templateID); err != nil {
		return err
	}
	for index, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO visit_template_frequent_visitors(user_id,template_id,frequent_visitor_id,sort_order) VALUES($1,$2,$3,$4)`, userID, templateID, id, index); err != nil {
			return err
		}
	}
	return nil
}

func touchFrequentVisitorIDs(ctx context.Context, executor frequentVisitorExecutor, userID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tag, err := executor.Exec(ctx, touchFrequentVisitorsByIDsSQL, userID, ids)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return errInvalidFrequentVisitorSelection
	}
	return nil
}

func touchTemplateFrequentVisitors(ctx context.Context, executor frequentVisitorExecutor, userID, templateID string) error {
	_, err := executor.Exec(ctx, touchTemplateFrequentVisitorsSQL, userID, templateID)
	return err
}

func (s *Server) deleteVisitTemplate(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	id := chi.URLParam(r, "templateID")
	err := s.db.QueryRow(r.Context(), `DELETE FROM visit_templates WHERE id=$1 AND user_id=$2 RETURNING id`, id, u.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "방문 템플릿을 찾을 수 없습니다")
		return
	}
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "visit_template.delete", "visit_template", id, clientIP(r), nil)
	w.WriteHeader(http.StatusNoContent)
}
