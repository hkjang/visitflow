package app

import "net/http"

func (s *Server) openAPI(w http.ResponseWriter, _ *http.Request) {
	paths := map[string]any{
		"/version":                  map[string]any{"get": operation("서비스 버전 조회", false)},
		"/auth/config":              map[string]any{"get": operation("로그인 및 SSO 구성 조회", false)},
		"/visits":                   map[string]any{"get": operation("권한 범위 방문 검색", true), "post": operation("단일·단체 방문 신청", true)},
		"/visits/import/preview":    map[string]any{"post": operation("CSV/XLSX 방문자 가져오기 검증", true)},
		"/visits/{visitID}":         map[string]any{"get": operation("방문 상세 조회", true), "put": operation("방문 일정 수정", true)},
		"/visits/{visitID}/approve": map[string]any{"post": operation("방문 승인 및 QR 발급", true)},
		"/visits/{visitID}/reject":  map[string]any{"post": operation("방문 반려", true)},
		"/visits/{visitID}/cancel":  map[string]any{"post": operation("방문 취소 및 QR 폐기", true)},
		"/visitor-visits/{visitorVisitID}/qr/reissue": map[string]any{"post": operation("개별 QR 회전·재발급", true)},
		"/qr/verify":                                   map[string]any{"post": operation("서버 검증형 QR 확인", true)},
		"/checkins":                                    map[string]any{"post": operation("체크인 트랜잭션 및 담당자 알림", true)},
		"/checkouts":                                   map[string]any{"post": operation("방문자 퇴실", true)},
		"/lobby/today":                                 map[string]any{"get": operation("오늘 로비 방문자", true)},
		"/lobby/current":                               map[string]any{"get": operation("현재 사내 체류 방문자", true)},
		"/lobby/stream":                                map[string]any{"get": operation("로비 실시간 SSE", true)},
		"/lobby/walk-ins":                              map[string]any{"post": operation("현장 방문 등록", true)},
		"/visit-templates":                             map[string]any{"get": operation("내 방문 템플릿 조회", true), "post": operation("방문 템플릿 생성", true)},
		"/visit-templates/{templateID}":                map[string]any{"get": operation("방문 템플릿 상세 조회", true), "put": operation("방문 템플릿 수정", true), "delete": operation("방문 템플릿 삭제", true)},
		"/frequent-visitors":                           map[string]any{"get": operation("내 자주 방문자 조회", true), "post": operation("자주 방문자 등록", true)},
		"/frequent-visitors/{frequentVisitorID}":       map[string]any{"put": operation("자주 방문자 수정", true), "delete": operation("자주 방문자 삭제", true)},
		"/guides":                                      map[string]any{"get": operation("게시된 사용자 가이드 조회", true)},
		"/guides/{guideID}":                            map[string]any{"get": operation("게시된 사용자 가이드 상세 조회", true)},
		"/admin/guides":                                map[string]any{"get": operation("전체 사용자 가이드 관리 목록", true), "post": operation("사용자 가이드 등록", true)},
		"/admin/guides/{guideID}":                      map[string]any{"put": operation("사용자 가이드 수정·게시", true), "delete": operation("사용자 가이드 삭제", true)},
		"/admin/notification-apis":                     map[string]any{"get": operation("SMS·MMS·카카오톡 호출 API 조회", true), "post": operation("문자 호출 API 등록", true)},
		"/admin/notification-apis/{apiID}":             map[string]any{"put": operation("문자 호출 API 수정", true), "delete": operation("문자 호출 API 삭제", true)},
		"/admin/notification-rules":                    map[string]any{"get": operation("발송 시점 규칙 조회", true), "post": operation("발송 시점 규칙 등록", true)},
		"/admin/notification-rules/{ruleID}":           map[string]any{"put": operation("발송 시점 규칙 수정", true), "delete": operation("발송 시점 규칙 삭제", true)},
		"/api-keys":                                    map[string]any{"get": operation("개인 API 키 조회", true), "post": operation("개인 API 키 생성", true)},
		"/api-key-policy":                              map[string]any{"get": operation("관리자가 정한 개인 키 Scope·만료 정책 조회", true)},
		"/api-keys/{keyID}":                            map[string]any{"patch": operation("개인 키 이름·Scope 변경", true), "delete": operation("개인 키 즉시 폐기", true)},
		"/api-keys/{keyID}/rotate":                     map[string]any{"post": operation("개인 키 회전", true)},
		"/admin/dashboard":                             map[string]any{"get": operation("관리자 운영 현황", true)},
		"/admin/statistics":                            map[string]any{"get": operation("일별·부서별 방문 통계", true)},
		"/admin/audit-logs":                            map[string]any{"get": operation("감사 로그 조회", true)},
		"/admin/watchlist":                             map[string]any{"get": operation("방문 제한 목록", true), "post": operation("방문 제한 등록", true)},
		"/settings":                                    map[string]any{"get": operation("전체 운영 설정 조회", true), "put": operation("운영 설정 변경", true)},
		"/visitor-visits/{visitorVisitID}/invitation":  map[string]any{"post": operation("방문자 셀프 사전등록 링크 발급", true), "delete": operation("사전등록 링크 폐기", true)},
		"/public/registrations/{token}":                map[string]any{"get": operation("사전등록 대상 조회", false), "post": operation("방문자 본인 사전등록 및 동의 제출", false)},
		"/public/passes/{token}":                       map[string]any{"get": operation("모바일 방문증 조회", false)},
		"/kiosk/enroll":                                map[string]any{"post": operation("로비 키오스크 기기 등록", false)},
		"/lobby/roster":                                map[string]any{"get": operation("비상 대피용 현재 체류자 명단", true)},
		"/admin/metrics":                               map[string]any{"get": operation("운영 지표 스냅샷", true)},
		"/admin/notifications/retry-failed":            map[string]any{"post": operation("실패 알림 일괄 재시도", true)},
		"/admin/notifications/{notificationID}/retry":  map[string]any{"post": operation("실패 알림 개별 재시도", true)},
		"/admin/notifications/{notificationID}/cancel": map[string]any{"post": operation("대기 알림 취소", true)},
		"/admin/visit-types":                           map[string]any{"get": operation("방문 유형·체크리스트 조회", true), "post": operation("방문 유형 등록", true)},
		"/admin/visit-types/{visitTypeID}":             map[string]any{"put": operation("방문 유형 수정", true), "delete": operation("방문 유형 비활성화", true)},
		"/admin/kiosk-devices":                         map[string]any{"get": operation("로비 키오스크 기기 목록", true), "post": operation("키오스크 기기 토큰 발급", true)},
		"/admin/kiosk-devices/{deviceID}":              map[string]any{"delete": operation("키오스크 기기 폐기", true)},
		"/admin/audit-logs.csv":                        map[string]any{"get": operation("감사 로그 CSV 내보내기", true)},
		"/admin/visits.csv":                            map[string]any{"get": operation("방문 이력 CSV 내보내기", true)},
		"/admin/statistics.csv":                        map[string]any{"get": operation("방문 통계 CSV 내보내기", true)},
	}
	qrImage := operation("MMS 연동용 방문자 QR JPEG 조회", false)
	qrImage["servers"] = []map[string]string{{"url": "/"}}
	qrImage["parameters"] = []map[string]any{{"name": "qrcode_file_seq", "in": "path", "required": true, "schema": map[string]string{"type": "string", "pattern": "^[A-Za-z0-9_-]{24}$"}}}
	qrImage["responses"] = map[string]any{
		"200": map[string]any{"description": "QR JPEG", "content": map[string]any{"image/jpeg": map[string]any{"schema": map[string]string{"type": "string", "format": "binary"}}}},
		"404": map[string]string{"description": "QR 없음, 폐기 또는 만료"},
	}
	paths["/img/visitor/{qrcode_file_seq}.jpg"] = map[string]any{"get": qrImage, "head": qrImage}
	writeJSON(w, 200, map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "VisitFlow API", "version": s.version, "description": "오프라인 사내 방문자 관리 REST API. 방문 신청·승인·QR·로비·퇴실·감사 흐름과 역할/범위 기반 접근 제어를 제공합니다."},
		"servers": []map[string]string{{"url": "/api/v1"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"personalKey":   map[string]string{"type": "http", "scheme": "bearer", "bearerFormat": "VisitFlow vf_ personal API key"},
				"cookieSession": map[string]string{"type": "apiKey", "in": "cookie", "name": sessionCookie},
			},
			"schemas": map[string]any{
				"VisitorInput": map[string]any{"type": "object", "required": []string{"name", "phone", "consent"}, "properties": map[string]any{"name": map[string]string{"type": "string"}, "phone": map[string]string{"type": "string"}, "company": map[string]string{"type": "string"}, "vehicle": map[string]string{"type": "string"}, "equipment": map[string]any{"type": "array", "items": map[string]string{"type": "string"}}, "locale": map[string]any{"type": "string", "enum": []string{"ko", "en", "ja", "zh"}}, "consent": map[string]string{"type": "boolean"}}},
				"VisitInput":   map[string]any{"type": "object", "required": []string{"siteId", "startAt", "endAt", "purpose", "visitors"}, "properties": map[string]any{"siteId": map[string]string{"type": "string"}, "visitTypeId": map[string]string{"type": "string"}, "startAt": map[string]string{"type": "string", "format": "date-time"}, "endAt": map[string]string{"type": "string", "format": "date-time"}, "purpose": map[string]string{"type": "string"}, "checklist": map[string]any{"type": "object", "additionalProperties": map[string]string{"type": "boolean"}}, "visitors": map[string]any{"type": "array", "items": map[string]string{"$ref": "#/components/schemas/VisitorInput"}}}},
			},
		},
		"security": []map[string]any{{"personalKey": []string{}}, {"cookieSession": []string{}}},
		"paths":    paths,
	})
}

func operation(summary string, auth bool) map[string]any {
	m := map[string]any{"summary": summary, "responses": map[string]any{"200": map[string]string{"description": "성공"}, "400": map[string]string{"description": "잘못된 요청"}, "401": map[string]string{"description": "인증 필요"}, "403": map[string]string{"description": "권한 없음"}, "409": map[string]string{"description": "상태 충돌"}}}
	if !auth {
		m["security"] = []any{}
	}
	return m
}
