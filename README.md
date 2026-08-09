<p align="center">
  <strong>VF</strong><br>
  <h1 align="center">VisitFlow</h1>
</p>

<p align="center">
  <strong>방문 신청부터 승인 · QR · 로비 체크인 · 담당자 알림 · 퇴실 · 감사까지 연결하는 오프라인 기업용 방문자 관리 플랫폼</strong>
</p>

VisitFlow는 Go API와 React/Material UI를 하나의 Docker 이미지에 포함한다. 실행 중 CDN이나 이미지 레지스트리 접근이 없고 PostgreSQL 외 Redis·Object Storage 같은 추가 미들웨어가 필요하지 않다.

## 주요 기능

- 개인 서비스: 단일·단체 방문 신청, 방문 템플릿, 일정/상태 조회, 취소, SMS 재발송
- 승인 Workflow: 관리자 설정으로 전체 비활성화 또는 부서/보안 담당 승인 운영
- 방문자별 서버 검증 QR: 개인정보 미포함, HMAC 조회, 1회 사용, Replay 감지, 폐기·회전·재발급
- 모바일 방문증: 앱 설치 없이 QR, 장소, 시간, 마스킹된 담당자와 상태 표시
- 로비 서비스: 웹/모바일 카메라, USB QR 스캐너, 현장 방문, 실시간 SSE, 체크인/퇴실
- 운영 관리: 멀티 사업장·로비, 조직, RBAC, Watch List, 통계, 알림 재시도, 감사 로그
- 개인정보: AES-256-GCM 필드 암호화, 목록 마스킹, 전화번호 HMAC 색인, 정책 기반 자동 파기
- Keycloak OIDC: Issuer URL, Client ID, Client Secret 설정만으로 Discovery + Authorization Code/PKCE 연동
- 개인 API 키: `read`/`write`/`mcp` Scope, 만료, 회전 유예, 즉시 폐기, 원문 1회 표시
- REST/OpenAPI 3.1 및 MCP Streamable HTTP 9개 Tool
- 로그인 화면과 프로필 컨텍스트 메뉴에 버전·Commit 표시

## UI 프레임워크 결정

React 19 + TypeScript + Material UI 7을 사용한다. 방문자 운영 화면은 폼·테이블·상태 Badge·반응형 레이아웃과 키보드 접근성이 중요하고, MUI는 모든 자산을 빌드 시 이미지에 번들링할 수 있어 오프라인 운영에도 적합하다. UI는 세 가지 작업 문맥을 분리한다.

| 문맥 | UX 우선순위 |
| --- | --- |
| 개인 서비스 | 방문 신청 속도, 오늘 일정, QR 관리 |
| 로비 서비스 | QR 판독, 큰 상태 피드백, 현재 체류·퇴실 |
| 관리자 | 예외·승인·실패 알림, 정책과 감사 추적 |

## 오프라인 설치

PostgreSQL 14+ 데이터베이스를 준비한 뒤 GitHub Release의 이미지 하나만 내부망으로 반입한다.

```bash
docker load < VisitFlow-v2.0.0-linux-amd64-image.tar.gz

export POSTGRES_DSN='postgres://visitflow:password@postgres.intra:5432/visitflow?sslmode=require'
export BOOTSTRAP_ADMIN='admin'
export BOOTSTRAP_ADMIN_PASSWORD='change-this-strong-password'
export VISITFLOW_IMAGE_TAG='2.0.0'
docker compose up -d
```

애플리케이션 컨테이너가 받는 환경변수는 정확히 세 개다.

| 환경변수 | 설명 |
| --- | --- |
| `POSTGRES_DSN` | PostgreSQL DSN |
| `BOOTSTRAP_ADMIN` | 최초 최고 관리자 아이디 |
| `BOOTSTRAP_ADMIN_PASSWORD` | 최초 관리자 비밀번호, 12자 이상 |

`VISITFLOW_IMAGE_TAG`는 Compose가 로컬 이미지 태그를 선택하는 셸 치환값이며 컨테이너에는 전달되지 않는다. 부트스트랩 사용자가 이미 있으면 환경변수 비밀번호로 덮어쓰지 않는다.

`http://host:8080`에 접속한 뒤 관리자 → 시스템 설정에서 회사명, 기준 URL, Keycloak, 승인·QR, SMS, 보존·파기, 세션·키 정책을 구성한다. 운영 환경은 리버스 프록시에서 HTTPS를 종료하고 `X-Forwarded-Proto`와 `X-Forwarded-Host`를 전달해야 카메라와 Secure Cookie를 정상 사용할 수 있다.

## Keycloak 연결

1. 관리자 → 시스템 설정 → Keycloak SSO에서 `Issuer URL`, `Client ID`, `Client Secret`을 입력한다.
2. Keycloak Client의 Client authentication을 켠다.
3. Valid Redirect URI에 `https://서비스주소/api/v1/auth/oidc/callback`을 등록한다.
4. Group Membership mapper로 `groups` claim을 ID Token에 포함한다.
5. 필요하면 `/visitflow-admins`, `/visitflow-lobby`, `/visitflow-security`, `/visitflow-auditors`, `/visitflow-department-managers`를 사내 그룹명으로 변경한다.
6. 저장 후 연결 테스트를 실행하고 SSO를 활성화한다.

Issuer의 표준 Discovery 문서에서 Authorization/Token/JWKS Endpoint가 자동 구성되며 state, nonce, PKCE S256과 ID Token 서명을 검증한다.

## API와 MCP

- OpenAPI: `GET /api/v1/openapi.json`
- MCP: `POST /mcp`
- 인증: `Authorization: Bearer vf_...`

MCP Tool은 `search_visits`, `get_today_visitors`, `get_current_visitors`, `get_visit`, `create_visit`, `cancel_visit`, `search_visitor_history`, `get_lobby_status`, `get_visit_statistics`를 제공한다. 도구별 Role·Scope와 사용자/부서/사업장 범위가 REST와 동일하게 적용된다.

자세한 내용은 [API 및 MCP](docs/API_AND_MCP.md), [아키텍처](docs/ARCHITECTURE.md), [관리자 가이드](docs/ADMIN_GUIDE.md)를 참고한다.

## 백업과 복구

PostgreSQL 백업과 `visitflow-data` 볼륨을 한 세트로 백업한다. `/var/lib/visitflow/master.key`를 잃으면 DB에 암호화된 개인정보와 Client Secret을 복호화할 수 없다. 개인 API 키 원문은 서버에 저장하지 않아 복구할 수 없으며 새 키로 회전해야 한다.

## 개발·검증

```bash
cd web && npm ci && npm run build
cd .. && go test ./... && go vet ./...
docker build -t visitflow:dev .
```

## 릴리스

`v*.*.*` 태그를 push하면 GitHub Actions가 `linux/amd64` 단일 서비스 이미지를 빌드한다. `docker save | gzip`으로 만든 `VisitFlow-vX.Y.Z-linux-amd64-image.tar.gz`만 Release 자산으로 첨부하며 런타임에는 레지스트리나 인터넷이 필요 없다.

```bash
./scripts/release-image.sh 2.0.0
gzip -t VisitFlow-v2.0.0-linux-amd64-image.tar.gz
```
