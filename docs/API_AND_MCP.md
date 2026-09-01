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
- `GET /api/v1/visits` 사용자/부서/사업장 범위 방문 검색
- `POST /api/v1/visits/{id}/approve|reject|cancel`
- `POST /api/v1/visitor-visits/{id}/qr/reissue`
- `POST /api/v1/qr/verify`
- `POST /api/v1/checkins`, `POST /api/v1/checkouts`
- `GET /api/v1/lobby/today|current|stream`
- `POST /api/v1/lobby/walk-ins`
- `GET|POST /api/v1/visit-templates`, `GET|PUT|DELETE /api/v1/visit-templates/{id}`
- `GET|POST /api/v1/frequent-visitors`, `PUT|DELETE /api/v1/frequent-visitors/{id}`
- `GET /api/v1/guides`, `GET /api/v1/guides/{id}`
- `GET|HEAD /img/visitor/{qrcode_file_seq}.jpg` 외부 MMS Gateway용 QR JPEG
- `GET /api/v1/admin/statistics|audit-logs|notifications|visitors`
- `GET|POST /api/v1/admin/notification-apis|notification-rules` 및 항목별 `PUT|DELETE`
- `GET|POST /api/v1/admin/guides`, `PUT|DELETE /api/v1/admin/guides/{id}`
- `GET|PUT /api/v1/settings`
- `GET|POST /api/v1/api-keys`
- `PATCH /api/v1/api-keys/{keyID}` 키 이름·Scope 변경

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

개인정보는 마스킹하고 REST와 동일한 Role, API Scope, 사용자/부서/사업장 Scope를 적용한다. 변경 Tool은 감사 로그를 남긴다.
