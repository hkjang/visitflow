package app

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

type guideInput struct {
	Title     string `json:"title"`
	Category  string `json:"category"`
	Content   string `json:"content"`
	Published bool   `json:"published"`
	Pinned    bool   `json:"pinned"`
}

type guidePost struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Category    string     `json:"category"`
	Content     string     `json:"content,omitempty"`
	Excerpt     string     `json:"excerpt,omitempty"`
	Published   bool       `json:"published"`
	Pinned      bool       `json:"pinned"`
	AuthorName  string     `json:"authorName"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func normalizeGuideInput(in *guideInput) string {
	in.Title = strings.TrimSpace(in.Title)
	in.Category = strings.TrimSpace(in.Category)
	in.Content = strings.TrimSpace(in.Content)
	if in.Category == "" {
		in.Category = "일반"
	}
	if in.Title == "" {
		return "제목은 필수입니다"
	}
	if utf8.RuneCountInString(in.Title) > 200 {
		return "제목은 200자 이하여야 합니다"
	}
	if utf8.RuneCountInString(in.Category) > 50 {
		return "카테고리는 50자 이하여야 합니다"
	}
	if in.Content == "" {
		return "내용은 필수입니다"
	}
	if utf8.RuneCountInString(in.Content) > 100000 {
		return "내용은 100,000자 이하여야 합니다"
	}
	return ""
}

func (s *Server) listPublishedGuides(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	rows, err := s.db.Query(r.Context(), `SELECT g.id,g.title,g.category,left(g.content,240),g.published,g.pinned,
		COALESCE(u.display_name,'관리자'),g.published_at,g.created_at,g.updated_at
		FROM guide_posts g LEFT JOIN users u ON u.id=g.created_by
		WHERE g.published=true
		AND ($1='' OR g.title ILIKE '%%'||$1||'%%' OR g.content ILIKE '%%'||$1||'%%')
		AND ($2='' OR g.category=$2)
		ORDER BY g.pinned DESC,COALESCE(g.published_at,g.updated_at) DESC LIMIT 200`, search, category)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []guidePost{}
	for rows.Next() {
		var item guidePost
		if err := rows.Scan(&item.ID, &item.Title, &item.Category, &item.Excerpt, &item.Published, &item.Pinned, &item.AuthorName, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			notFoundOrServer(w, err)
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

func (s *Server) getPublishedGuide(w http.ResponseWriter, r *http.Request) {
	var item guidePost
	err := s.db.QueryRow(r.Context(), `SELECT g.id,g.title,g.category,g.content,g.published,g.pinned,
		COALESCE(u.display_name,'관리자'),g.published_at,g.created_at,g.updated_at
		FROM guide_posts g LEFT JOIN users u ON u.id=g.created_by
		WHERE g.id=$1 AND g.published=true`, chi.URLParam(r, "guideID")).Scan(
		&item.ID, &item.Title, &item.Category, &item.Content, &item.Published, &item.Pinned,
		&item.AuthorName, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) listAdminGuides(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	rows, err := s.db.Query(r.Context(), `SELECT g.id,g.title,g.category,g.content,g.published,g.pinned,
		COALESCE(u.display_name,'관리자'),g.published_at,g.created_at,g.updated_at
		FROM guide_posts g LEFT JOIN users u ON u.id=g.created_by
		WHERE ($1='' OR g.title ILIKE '%%'||$1||'%%' OR g.content ILIKE '%%'||$1||'%%')
		AND ($2='' OR ($2='published' AND g.published=true) OR ($2='draft' AND g.published=false))
		ORDER BY g.pinned DESC,g.updated_at DESC LIMIT 500`, search, status)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	defer rows.Close()
	items := []guidePost{}
	for rows.Next() {
		var item guidePost
		if err := rows.Scan(&item.ID, &item.Title, &item.Category, &item.Content, &item.Published, &item.Pinned, &item.AuthorName, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			notFoundOrServer(w, err)
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

func (s *Server) createGuide(w http.ResponseWriter, r *http.Request) {
	var in guideInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if message := normalizeGuideInput(&in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_guide", message)
		return
	}
	u, _ := userFrom(r)
	id := newID()
	_, err := s.db.Exec(r.Context(), `INSERT INTO guide_posts(id,title,category,content,published,pinned,created_by,updated_by,published_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$7,CASE WHEN $5 THEN now() ELSE NULL END)`, id, in.Title, in.Category, in.Content, in.Published, in.Pinned, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "guide.create", "guide_post", id, r.RemoteAddr, map[string]any{"title": in.Title, "category": in.Category, "published": in.Published, "pinned": in.Pinned})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) updateGuide(w http.ResponseWriter, r *http.Request) {
	var in guideInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if message := normalizeGuideInput(&in); message != "" {
		writeError(w, http.StatusBadRequest, "invalid_guide", message)
		return
	}
	id := chi.URLParam(r, "guideID")
	u, _ := userFrom(r)
	tag, err := s.db.Exec(r.Context(), `UPDATE guide_posts SET title=$2,category=$3,content=$4,published=$5,pinned=$6,updated_by=$7,
		published_at=CASE WHEN $5 THEN COALESCE(published_at,now()) ELSE NULL END,updated_at=now()
		WHERE id=$1`, id, in.Title, in.Category, in.Content, in.Published, in.Pinned, u.ID)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "가이드 글을 찾을 수 없습니다")
		return
	}
	s.audit(r.Context(), u.ID, "guide.update", "guide_post", id, r.RemoteAddr, map[string]any{
		"title": in.Title, "category": in.Category, "published": in.Published, "pinned": in.Pinned,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteGuide(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "guideID")
	var title, category string
	var published bool
	err := s.db.QueryRow(r.Context(), `DELETE FROM guide_posts WHERE id=$1 RETURNING title,category,published`, id).Scan(&title, &category, &published)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	u, _ := userFrom(r)
	s.audit(r.Context(), u.ID, "guide.delete", "guide_post", id, r.RemoteAddr, map[string]any{"title": title, "category": category, "published": published})
	w.WriteHeader(http.StatusNoContent)
}
