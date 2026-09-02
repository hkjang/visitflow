<p align="center">
  <strong>VF</strong><br>
  <h1 align="center">VisitFlow</h1>
</p>

<p align="center">
  <strong>방문 신청부터 승인 · QR · 로비 체크인 · 담당자 알림 · 퇴실 · 감사까지 연결하는 오프라인 기업용 방문자 관리 플랫폼</strong>
</p>

<p align="center">
  <a href="https://hkjang.github.io/visitflow/">🇰🇷 홍보 페이지</a> · <a href="https://hkjang.github.io/visitflow/index_en.html">🇺🇸 English Page</a> · <a href="https://hkjang.github.io/">🌐 전체 서비스</a> · <a href="https://github.com/sponsors/hkjang">💖 Sponsor</a>
</p>

VisitFlow는 Go API와 React/Material UI를 하나의 Docker 이미지에 포함한다. 실행 중 CDN이나 이미지 레지스트리 접근이 없고 PostgreSQL 외 Redis·Object Storage 같은 추가 미들웨어가 필요하지 않다.

## 주요 기능

- 개인 서비스: 단일·단체·매주 반복 방문 신청, CSV/XLSX 가져오기, 수정 가능한 방문 템플릿과 자주 방문자 주소록, 일정/상태 조회, 취소, 알림 재발송
- 방문자 셀프 사전등록: 방문자가 링크에서 직접 정보를 입력하고 본인이 동의하며, 동의 시각·정책 버전·IP를 별도 기록
- 방문 유형 체크리스트: 유형별 보안서약·안전교육·차량·반입장비 신고 필수화와 승인 강제
- 부재 시 대리 담당자: 지정 기간 동안 승인과 도착 알림을 대리자에게 전달하고, 승인 지연은 설정 시간 후 1회 에스컬레이션
- 승인 Workflow: 관리자 설정으로 전체 비활성화 또는 부서/보안 담당 승인 운영
- 방문자별 서버 검증 QR: 개인정보 미포함, HMAC 조회, 1회 사용, Replay 감지, 폐기·회전·재발급
- 모바일 방문증: 앱 설치 없이 QR, 장소, 시간, 마스킹된 담당자와 상태 표시
- 로비 서비스: 웹/모바일 카메라, USB QR 스캐너, 현장 방문, 실시간 SSE, 체크인/퇴실
- 무인 키오스크: 기기 토큰으로 등록한 태블릿이 로비 API만 사용하는 전용 모드
- 비상 대피 명단: 현재 체류자 인쇄용 명단과 네트워크 단절 시에도 열리는 오프라인 캐시
- 사내 SMTP: 관리자 설정·테스트 발송, 담당자별 메일 알림 개인화(도착·승인·반려·취소·승인 대기), 로컬 계정 메일 비밀번호 재설정
- 메시지 연동: SMS·MMS·카카오별 Base URL/Path/Method/Header/Parameter와 발송 이벤트·예약 시점·수신 대상·호출 API를 관리자 화면에서 관리
- 외부 MMS용 QR 이미지: `GET /img/visitor/{qrcode_file_seq}.jpg`로 서버 검증 QR을 JPEG 형식으로 제공
- 사용자 가이드 게시판: 관리자는 초안·게시·상단 고정 글을 관리하고 모든 로그인 사용자는 게시된 안내를 열람
- 운영 관리: 멀티 사업장·로비, 조직, RBAC, Watch List, 통계, 알림 수동 재시도·취소, 감사 로그
- 내보내기: 감사 로그·방문 이력·통계 CSV(UTF-8 BOM) 다운로드와 커서 기반 목록 페이지네이션
- 관측성: 관리자 지표 화면과 토큰으로 보호되는 Prometheus `/metrics`
- 다국어: 모바일 방문증과 셀프 사전등록 화면의 한국어·영어·일본어·중국어, 언어별 문자 발송 규칙
- 개인정보: AES-256-GCM 필드 암호화, 목록 마스킹, 전화번호 HMAC 색인, 정책 기반 자동 파기, 동의 이력 보관
- 접근 보호: 로그인·QR 검증·공개 방문증의 IP/계정 단위 제한과 잠금, 부팅 시 암호화 키 검증
- Keycloak OIDC: Issuer URL, Client ID, Client Secret 설정만으로 Discovery + Authorization Code/PKCE 연동
- 개인 API 키: 관리자 허용 `read`/`write`/`mcp` Scope, 키별 권한 변경, 만료, 회전 유예, 즉시 폐기, 원문 1회 표시
- REST/OpenAPI 3.1 및 MCP Streamable HTTP 9개 Tool
- 외부 시스템 연동: 문자 API와 같은 화면에서 출입 게이트·게스트 Wi-Fi 등 webhook 채널 호출
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
docker load < visitflow-v2.6.1.tar.gz

export POSTGRES_DSN='postgres://visitflow:password@postgres.intra:5432/visitflow?sslmode=require'
export BOOTSTRAP_ADMIN='admin'
export BOOTSTRAP_ADMIN_PASSWORD='change-this-strong-password'
export ENCRYPTION_KEY="$(openssl rand -base64 32)"
docker compose up -d
```

애플리케이션 컨테이너가 받는 환경변수는 정확히 네 개다.

| 환경변수 | 설명 |
| --- | --- |
| `POSTGRES_DSN` | PostgreSQL DSN |
| `BOOTSTRAP_ADMIN` | 최초 최고 관리자 아이디 |
| `BOOTSTRAP_ADMIN_PASSWORD` | 최초 관리자 비밀번호, 12자 이상 |
| `ENCRYPTION_KEY` | 개인정보·OIDC Secret·Token 암호화용 32바이트 키(Base64 또는 64자리 hex) |

`ENCRYPTION_KEY`는 `openssl rand -base64 32`로 한 번 생성한 뒤 PostgreSQL 백업과 함께 별도 보관한다. 값을 분실하거나 다른 값으로 바꾸면 기존 암호화 데이터를 복구할 수 없다. 부트스트랩 사용자가 이미 있으면 환경변수 비밀번호로 덮어쓰지 않는다.

`ENCRYPTION_KEY`가 기존 데이터와 다르면 서비스는 기동하지 않고 즉시 종료한다. 잘못된 키로 운영을 시작해 복호화 불가능한 데이터가 섞이는 것을 막기 위한 동작이다.

`http://host:8080`에 접속한 뒤 관리자 → 시스템 설정에서 회사명, 기준 URL, Keycloak, 승인·QR, SMS, 보존·파기, 세션·키·로그인 잠금 정책을 구성한다. 운영 환경은 리버스 프록시에서 HTTPS를 종료하고 `X-Forwarded-Proto`와 `X-Forwarded-Host`를 전달해야 카메라와 Secure Cookie를 정상 사용할 수 있다.

리버스 프록시 뒤에서는 관리자 → 시스템 설정 → 보안의 `신뢰할 Reverse Proxy`에 프록시 IP나 CIDR을 등록한다(사내 대역 전체는 `private` 한 단어로 지정한다). 등록한 주소에서 온 요청만 `X-Forwarded-For`를 신뢰해 실제 접속 IP를 로그인 잠금, 공개 API 요청 한도, 동의 기록, 감사 로그에 사용한다. 비워두면 헤더를 무시하고 TCP 접속 주소를 사용하므로, 프록시를 쓰지 않는 설치에서 헤더를 위조해 잠금과 요청 한도를 우회할 수 없다.

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

PostgreSQL 백업과 `ENCRYPTION_KEY`를 한 세트로 백업한다. 키를 잃으면 DB에 암호화된 개인정보와 Client Secret을 복호화할 수 없다. 개인 API 키 원문은 서버에 저장하지 않아 복구할 수 없으며 새 키로 회전해야 한다.

## 개발·검증

```bash
cd web && npm ci && npm run build
cd .. && go test ./... && go vet ./...
docker build -t visitflow:dev .
```

Go 테스트에는 실제 PostgreSQL을 사용하는 통합 테스트가 포함된다. `VISITFLOW_TEST_DSN`을 지정하면 테스트별로 임시 데이터베이스를 만들어 방문 신청 → 승인 → QR → 체크인 → 재사용 차단 → 퇴실 흐름과 로그인 잠금, 암호화 키 검증, 사전등록 동의 기록을 검증한다. 지정하지 않으면 해당 테스트는 건너뛴다.

```bash
VISITFLOW_TEST_DSN='postgres://visitflow:visitflow@127.0.0.1:5432/visitflow?sslmode=disable' go test ./... -count=1
```

브라우저 흐름은 Playwright로 검증한다. 서비스를 실행한 뒤 아래를 실행하면 로그인, 방문 신청, 모바일 방문증 언어 전환, USB 스캐너 방식 체크인, 비상 명단을 실제 브라우저로 확인한다.

```bash
cd web && npx playwright install --with-deps chromium
VISITFLOW_BASE_URL=http://127.0.0.1:8080 npm run test:e2e
```

## 릴리스

`v*.*.*` 태그를 push하면 GitHub Actions가 `linux/amd64` 단일 서비스 이미지 `visitflow:vX.Y.Z`를 빌드한다. `docker save | gzip`으로 만든 `visitflow-vX.Y.Z.tar.gz`만 Release 자산으로 첨부하며 런타임에는 레지스트리나 인터넷이 필요 없다. 아카이브에는 Compose가 바로 사용할 수 있는 `visitflow:latest` 별칭도 포함된다.

```bash
./scripts/release-image.sh 2.6.1
gzip -t visitflow-v2.6.1.tar.gz
```
