# VisitFlow API 및 MCP

## 인증

브라우저 Session 또는 프로필 메뉴에서 만든 개인 API 키를 사용한다.

```http
Authorization: Bearer vf_xxxxxxxxxxxxxxxxxxxxxxxxx
```

| Scope | 권한 |
| --- | --- |
| `read` | REST 조회 |
| `write` | REST/MCP 변경 |
| `mcp` | MCP 연결 |

관리자는 허용 Scope, 기본 만료일, 사용자별 활성 키 한도를 설정한다. 사용자는 허용 범위 안에서 키별 Scope를 변경할 수 있다. 관리자가 Scope를 제거하면 기존 키에도 즉시 차단된다. 회전하면 새 버전과 새 만료일이 생성되고 기존 키는 관리자 설정 유예시간 동안만 유효하다. 즉시 폐기하면 유예 없이 무효화된다.

## REST 대표 경로

- `POST /api/v1/visits` 단일·단체 방문 신청
- `POST /api/v1/visits/import/preview` CSV/XLSX 방문자 가져오기 검증
- `GET /api/v1/visits` 사용자/부서/사업장 범위 방문 검색. `limit`과 `cursor`로 페이지를 넘기고 응답의 `nextCursor`·`hasMore`로 다음 페이지를 판단한다.
- `POST /api/v1/visits/{id}/approve|reject|cancel`, `POST /api/v1/visits/{id}/cancel-series` 이 회차부터 반복 일정 취소
- `POST /api/v1/visitor-visits/{id}/cancel` 단체 방문의 방문자 개별 취소
- `POST /api/v1/checkins/manual` QR 없는 방문자의 신분 확인 후 직접 체크인(로비 담당자, 사유 필수)
- `POST /api/v1/visitor-visits/{id}/qr/reissue`
- `POST|DELETE /api/v1/visitor-visits/{id}/invitation` 방문자 셀프 사전등록 링크 발급·폐기
- `GET|POST /api/v1/public/registrations/{token}` 방문자 본인 사전등록과 동의 제출(인증 불필요)
- `GET /api/v1/public/passes/{token}` 모바일 방문증. `?lang=ko|en|ja|zh`로 언어를 지정한다.
- `POST /api/v1/qr/verify`
- `POST /api/v1/checkins`, `POST /api/v1/checkouts`
- `GET /api/v1/lobby/today|current|stream`
- `GET /api/v1/lobby/roster` 비상 대피용 현재 체류자 명단
- `POST /api/v1/kiosk/enroll` 로비 키오스크 기기 등록(기기 토큰으로 인증)
- `POST /api/v1/lobby/walk-ins`
- `GET|POST /api/v1/visit-templates`, `GET|PUT|DELETE /api/v1/visit-templates/{id}`
- `GET|POST /api/v1/frequent-visitors`, `PUT|DELETE /api/v1/frequent-visitors/{id}`
- `GET /api/v1/guides`, `GET /api/v1/guides/{id}`
- `GET|HEAD /img/visitor/{qrcode_file_seq}.jpg` 외부 MMS Gateway용 QR JPEG
- `GET /api/v1/admin/statistics|audit-logs|notifications|visitors|metrics` — 감사 로그는 `action`·`actor`·`from`·`to`·`before`(keyset), 방문자는 `q`, 알림은 `status`로 필터링한다.
- `GET /api/v1/admin/audit-logs.csv|visits.csv|statistics.csv` UTF-8 BOM CSV 내보내기 — 감사 로그 CSV는 목록과 동일하게 `action`·`actor`·`from`·`to`를 적용하고 `limit`(기본 10,000, 최대 100,000)까지 내려받는다.
- `POST /api/v1/admin/notifications/retry-failed`, `POST /api/v1/admin/notifications/{id}/retry|cancel`
- `POST /api/v1/admin/users` 로컬 사용자 생성, `POST /api/v1/admin/users/{id}/password-reset|sessions/revoke`
- `GET /api/v1/admin/visitors/{id}`, `POST /api/v1/admin/visitors/{id}/erase` 방문자 이력 조회·삭제 요청 처리
- `POST /api/v1/admin/notification-apis/{id}/test` 문자 API 테스트 발송
- `POST /api/v1/settings/smtp/test` SMTP 테스트 메일
- `POST /api/v1/auth/password-reset/request`, `GET|POST /api/v1/auth/password-reset/{token}` 로컬 계정 메일 재설정(인증 불필요, 항상 202)
- `GET|PUT /api/v1/profile/notifications` 내 메일 알림 이벤트 선택
- `GET /api/v1/settings/export` 비밀값 제외 설정 JSON(`PUT /settings`에 그대로 사용 가능)
- `GET|POST /api/v1/admin/visit-types`, `PUT|DELETE /api/v1/admin/visit-types/{id}`
- `GET|POST /api/v1/admin/kiosk-devices`, `DELETE /api/v1/admin/kiosk-devices/{id}`
- `GET|POST /api/v1/admin/notification-apis|notification-rules` 및 항목별 `PUT|DELETE`
- `GET|POST /api/v1/admin/guides`, `PUT|DELETE /api/v1/admin/guides/{id}`
- `GET|PUT /api/v1/settings`
- `GET|POST /api/v1/api-keys`
- `PATCH /api/v1/api-keys/{keyID}` 키 이름·Scope 변경

임시 비밀번호 상태(`mustChangePassword`)인 세션은 `/auth/me`, `/auth/password`, `/auth/logout` 외 모든 요청에 `403 password_change_required`를 받는다.

공개 엔드포인트(로그인, 모바일 방문증, QR 이미지, 셀프 사전등록)에는 IP 단위 분당 요청 한도가 적용되며 초과 시 `429`와 `Retry-After`를 반환한다. 로그인은 추가로 IP·계정별 실패 잠금이 적용된다.

운영 지표는 `GET /metrics`에서 Prometheus 형식으로 제공한다. 관리자가 `security.metrics_token`을 설정한 뒤 `Authorization: Bearer <토큰>`으로 호출해야 하며, 설정 전에는 404를 반환한다.

OpenAPI 3.1 문서는 실행 중인 서비스의 `/api/v1/openapi.json`에서 제공한다.

## MCP

```text
POST https://visitflow.example.intra/mcp
Authorization: Bearer vf_...
```

지원 Tool:

- `search_visits`
- `get_today_visitors`
- `get_current_visitors`
- `get_visit`
- `create_visit`
- `cancel_visit`
- `search_visitor_history`
- `get_lobby_status`
- `get_visit_statistics`

`search_visits`와 `get_today_visitors`는 REST와 같은 `cursor` 페이지네이션을 지원하고 `create_visit`은 `visit_type_id`를 받는다.

개인정보는 마스킹하고 REST와 동일한 Role, API Scope, 사용자/부서/사업장 Scope를 적용한다. 변경 Tool은 감사 로그를 남긴다.
