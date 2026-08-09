package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type detectedObject struct{ X, Y, W, H, Confidence float64 }

func (s *Server) analyzeFloorMap(w http.ResponseWriter, r *http.Request) {
	mapID := chi.URLParam(r, "mapID")
	var data []byte
	var contentType, status string
	err := s.db.QueryRow(r.Context(), `SELECT file_data,content_type,status FROM floor_maps WHERE id=$1`, mapID).Scan(&data, &contentType, &status)
	if err != nil {
		notFoundOrServer(w, err)
		return
	}
	if status == "published" {
		writeError(w, 409, "published_map", "게시 중인 도면은 다시 분석할 수 없습니다")
		return
	}
	threshold := .8
	if v, _ := s.getSetting(r.Context(), "ai.confidence_threshold"); v != "" {
		threshold = parseFloat(v, .8)
	}
	autoThreshold := .95
	if v, _ := s.getSetting(r.Context(), "ai.auto_approve_threshold"); v != "" {
		autoThreshold = parseFloat(v, .95)
	}
	u, _ := userFrom(r)
	jobID := newID()
	_, _ = s.db.Exec(r.Context(), `INSERT INTO analysis_jobs(id,floor_map_id,status,confidence_threshold,created_by) VALUES($1,$2,'running',$3,$4)`, jobID, mapID, threshold, u.ID)
	_, _ = s.db.Exec(r.Context(), `UPDATE floor_maps SET status='analyzing' WHERE id=$1`, mapID)
	img, err := decodeMapImage(r.Context(), data, contentType)
	if err != nil {
		s.failAnalysis(r.Context(), jobID, mapID, err)
		writeError(w, 422, "analysis_failed", err.Error())
		return
	}
	objects := detectRectangles(img)
	prefix := "SEAT-"
	_ = s.db.QueryRow(r.Context(), `SELECT b.code||'-'||f.code||'-' FROM floor_maps m JOIN floors f ON f.id=m.floor_id JOIN buildings b ON b.id=f.building_id WHERE m.id=$1`, mapID).Scan(&prefix)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.failAnalysis(r.Context(), jobID, mapID, err)
		notFoundOrServer(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, _ = tx.Exec(r.Context(), `DELETE FROM seats WHERE floor_map_id=$1 AND confidence IS NOT NULL AND NOT EXISTS(SELECT 1 FROM seat_assignments a WHERE a.seat_id=seats.id)`, mapID)
	created, review := 0, 0
	for _, obj := range objects {
		if obj.Confidence < threshold {
			continue
		}
		created++
		if obj.Confidence < autoThreshold {
			review++
		}
		seatNo := fmt.Sprintf("%s%03d", prefix, created)
		_, err = tx.Exec(r.Context(), `INSERT INTO seats(id,floor_map_id,seat_no,x,y,width,height,confidence,metadata) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'{"source":"offline-cv"}') ON CONFLICT(floor_map_id,seat_no) DO NOTHING`, newID(), mapID, seatNo, obj.X, obj.Y, obj.W, obj.H, obj.Confidence)
		if err != nil {
			break
		}
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE floor_maps SET status='review' WHERE id=$1`, mapID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE analysis_jobs SET status='completed',detected_count=$2,review_count=$3,completed_at=now() WHERE id=$1`, jobID, created, review)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		s.failAnalysis(r.Context(), jobID, mapID, err)
		notFoundOrServer(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "floor_map.analyze", "floor_map", mapID, r.RemoteAddr, map[string]any{"engine": "offline-cv-v1", "detected": created, "review": review})
	writeJSON(w, 200, map[string]any{"jobId": jobID, "engine": "offline-cv-v1", "detected": created, "needsReview": review, "message": analysisMessage(created)})
}

func analysisMessage(count int) string {
	if count == 0 {
		return "자동 인식 후보가 없습니다. 좌석 일괄 생성 도구로 보정해 주세요."
	}
	return strconv.Itoa(count) + "개의 좌석 후보를 생성했습니다. 낮은 신뢰도 항목만 확인해 주세요."
}

func (s *Server) failAnalysis(ctx context.Context, jobID, mapID string, err error) {
	msg := "analysis failed"
	if err != nil {
		msg = err.Error()
	}
	_, _ = s.db.Exec(ctx, `UPDATE analysis_jobs SET status='failed',error=$2,completed_at=now() WHERE id=$1`, jobID, msg)
	_, _ = s.db.Exec(ctx, `UPDATE floor_maps SET status='failed' WHERE id=$1`, mapID)
}

func decodeMapImage(ctx context.Context, data []byte, contentType string) (image.Image, error) {
	if strings.HasPrefix(contentType, "image/") {
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
	if contentType != "application/pdf" {
		return nil, fmt.Errorf("지원하지 않는 도면 형식입니다")
	}
	dir, err := os.MkdirTemp("", "seaton-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "map.pdf")
	if err = os.WriteFile(input, data, 0o600); err != nil {
		return nil, err
	}
	output := filepath.Join(dir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-f", "1", "-singlefile", "-png", "-r", "150", input, output)
	if raw, e := cmd.CombinedOutput(); e != nil {
		return nil, fmt.Errorf("PDF 변환 실패: %s", strings.TrimSpace(string(raw)))
	}
	f, err := os.Open(output + ".png")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func detectRectangles(src image.Image) []detectedObject {
	b := src.Bounds()
	scale := 1.0
	maxSide := math.Max(float64(b.Dx()), float64(b.Dy()))
	if maxSide > 1400 {
		scale = 1400 / maxSide
	}
	w := int(float64(b.Dx()) * scale)
	h := int(float64(b.Dy()) * scale)
	if w < 1 || h < 1 {
		return nil
	}
	dark := make([]bool, w*h)
	for y := 0; y < h; y++ {
		sy := b.Min.Y + int(float64(y)/scale)
		for x := 0; x < w; x++ {
			sx := b.Min.X + int(float64(x)/scale)
			g := color.GrayModel.Convert(src.At(sx, sy)).(color.Gray)
			dark[y*w+x] = g.Y < 105
		}
	}
	seen := make([]bool, len(dark))
	type point struct{ x, y int }
	objects := []detectedObject{}
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			idx := y*w + x
			if seen[idx] || !dark[idx] {
				continue
			}
			queue := []point{{x, y}}
			seen[idx] = true
			minX, maxX, minY, maxY, count := x, x, y, y, 0
			for len(queue) > 0 {
				p := queue[len(queue)-1]
				queue = queue[:len(queue)-1]
				count++
				if p.x < minX {
					minX = p.x
				}
				if p.x > maxX {
					maxX = p.x
				}
				if p.y < minY {
					minY = p.y
				}
				if p.y > maxY {
					maxY = p.y
				}
				for _, n := range []point{{p.x - 1, p.y}, {p.x + 1, p.y}, {p.x, p.y - 1}, {p.x, p.y + 1}} {
					if n.x < 0 || n.x >= w || n.y < 0 || n.y >= h {
						continue
					}
					ni := n.y*w + n.x
					if !seen[ni] && dark[ni] {
						seen[ni] = true
						queue = append(queue, n)
					}
				}
			}
			bw, bh := maxX-minX+1, maxY-minY+1
			if bw < 8 || bh < 8 || bw > w/7 || bh > h/7 {
				continue
			}
			ratio := float64(bw) / float64(bh)
			if ratio < .35 || ratio > 2.8 {
				continue
			}
			density := float64(count) / float64(bw*bh)
			if density < .035 || density > .72 {
				continue
			}
			rectangularity := 1 - math.Min(1, math.Abs(.25-density)/.5)
			sizeScore := math.Min(1, float64(bw*bh)/400)
			confidence := .72 + .16*rectangularity + .10*sizeScore
			if confidence > .99 {
				confidence = .99
			}
			objects = append(objects, detectedObject{float64(minX) / float64(w), float64(minY) / float64(h), float64(bw) / float64(w), float64(bh) / float64(h), confidence})
		}
	}
	sort.Slice(objects, func(i, j int) bool {
		if math.Abs(objects[i].Y-objects[j].Y) > .02 {
			return objects[i].Y < objects[j].Y
		}
		return objects[i].X < objects[j].X
	})
	if len(objects) > 500 {
		return objects[:500]
	}
	return objects
}
