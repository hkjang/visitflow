# VisitFlow 아키텍처

```text
임직원 Web  관리자 Web  로비/키오스크  모바일 방문증·사전등록  MCP Client
       └────────────── same-origin HTTPS ───────────────┘
                              │
               ┌──────────────▼──────────────┐
               │ VisitFlow single container │
               │ Go REST/MCP/SSE API         │
               │ React + Material UI         │
               │ OIDC · RBAC · Audit         │
               │ QR · Notification Worker    │
               └──────────────┬──────────────┘
                              │ POSTGRES_DSN
                         PostgreSQL 14+
```

## 보안 경계

- 브라우저: HttpOnly/SameSite Session Cookie, 변경 요청 CSRF Token, CSP, Secure Cookie 자동 적용
- Keycloak: Discovery, Authorization Code + PKCE S256, state/nonce, ID Token 서명·Audience 검증
- API 키: `vf_` 원문은 1회만 표시하고 설치별 HMAC-SHA-256 Digest만 저장
- 설정/개인정보: `ENCRYPTION_KEY`로 주입한 설치별 32바이트 Key로 AES-256-GCM 암호화
- 방문자 검색: 전화번호 원문 대신 정규화 값의 namespaced HMAC 색인 사용
- QR: `vfq_` 256-bit Random Token, HMAC 조회, 개인정보 미포함, 유효기간·1회 사용·폐기·회전
- Dynamic QR: 관리자가 30~60초 주기를 설정하면 시간 Window HMAC 서명을 추가하고 현재/직전 Window만 허용
- MMS QR JPEG: 외부 Gateway가 내려받은 이미지 안의 QR에는 별도 HMAC 정적 서명을 사용한다. 난수 파일 경로·유효기간·폐기 상태는 계속 검증하지만 Dynamic Window 예외이므로 이미지 URL을 메시지 Gateway 외에 노출하지 않는다.
- 로그인 잠금: IP와 계정별 실패 횟수를 `auth_throttle`에 저장해 재시작과 다중 노드에서도 유지하고, 임계값을 넘으면 설정 시간 동안 차단
- 공개 엔드포인트: 로그인·모바일 방문증·QR 이미지·셀프 사전등록에 IP 단위 분당 요청 한도를 적용해 Token 열거 차단
- 암호화 키 검증: 부팅 시 저장된 검증 암호문을 복호화해 `ENCRYPTION_KEY` 일치를 확인하고, 불일치면 기동을 중단
- 키오스크 기기: `vfk_` 기기 토큰은 SameSite=Strict Cookie와 Double Submit CSRF로 동작하며 로비 Route Group에서만 인증된다
- CSP: 요청마다 nonce를 발급해 Material UI가 주입하는 Style Element에만 허용하고, Script는 파일 Source만 허용
- 동의 기록: 방문자 동의를 주체(담당자 대행/본인), 정책 버전, 언어, IP, User Agent와 함께 `consent_records`에 이벤트로 보관
- 감사: 로그인, 방문 변경, QR 검증, 입·퇴실, 개인정보/Watch List 조회, 명단 조회, 내보내기, 설정과 키 변경 기록

## 상태와 트랜잭션

```text
REQUESTED → PENDING_APPROVAL → SCHEDULED → CHECKED_IN → CHECKED_OUT
                    └→ REJECTED     ├→ CANCELLED
                                    └→ NO_SHOW
```

방문 승인 시 방문자별 QR 생성과 방문증 알림 큐 등록을 같은 DB 트랜잭션에서 처리한다. 체크인 시 QR Row Lock, 상태/기간/Replay 검증, Token 사용 처리, 방문자/방문 상태 전이, 이벤트 기록, 담당자 도착 알림 큐 등록을 한 트랜잭션에서 처리한다.

## 운영 구성

- PostgreSQL 외 Redis, Message Broker, Object Storage 불필요
- 알림 Worker는 기존 `log`/단일 `webhook` 호환 모드와 관리자 정의 다중 API Adapter를 함께 제공한다. API별 채널(SMS·MMS·카카오), Base URL/Path, HTTP Method, JSON/Form/Query Parameter, Header를 암호화 저장하며 최대 5회 재시도한다. Worker는 한 건씩 임의 Claim Token으로 선점하고 외부 호출 전체 동안 해당 알림 Row를 잠가 발송과 취소의 순서를 직렬화한다. 만료된 `sending` Row는 다시 회수하되 결과 저장도 같은 Claim Token일 때만 허용하며, 같은 알림 ID를 Gateway 멱등성 키로 반드시 전달한다.
- 발송 규칙은 방문 확정·방문 시작·체크인·퇴실·취소 이벤트, 방문 시작 기준 오프셋, 방문자/담당자 수신 대상과 호출 API를 결합한다. 대기열 Row에는 선택 API와 방문자별 QR 이미지 문맥을 고정한다.
- MMS Gateway는 인증 없이 `GET /img/visitor/{qrcode_file_seq}.jpg`를 가져갈 수 있다. 식별자는 추측하기 어려운 난수이며 폐기·만료·취소·반려·퇴실·미방문 QR은 같은 404 응답으로 차단한다. 참가자별 활성 QR은 partial unique index와 참가자 Row Lock으로 하나만 허용한다.
- 사용자 가이드 글은 PostgreSQL에 저장하고 로그인 사용자에게 게시 글만 제공한다. 초안·등록·수정·삭제는 관리자 RBAC와 감사 로그 경계를 따른다.
- SSE는 프로세스 내 Fan-out으로 로비 변경을 전달하고 DB가 Source of Truth 역할을 수행
- Scheduler는 미방문, 자동 퇴실, Session·잠금 정리, 만료 사전등록 링크 폐기, 승인 지연 에스컬레이션, 개인정보 파기와 감사 로그 보존을 수행
- Migration은 `migrations/<version>_<name>.sql` 파일 단위로 각자의 트랜잭션에서 한 번만 적용하고 `schema_migrations`에 기록한다. `GET /readyz`는 적용 버전과 바이너리가 기대하는 버전을 함께 반환하고 불일치 시 503을 반환한다.
- 목록은 커서(keyset) 페이지네이션을 사용해 새 방문이 생겨도 다음 페이지가 밀리지 않으며, 로비 목록은 한도를 넘으면 잘렸음을 응답에 표시한다.
- 지표는 프로세스 카운터와 DB Gauge를 합쳐 관리자 화면과 토큰 보호 `/metrics`로 제공한다.
- Service Worker는 비상 대피 명단만 오프라인 캐시하고 방문자 데이터가 담긴 다른 API 응답은 캐시하지 않는다.
- 성능: 설정값은 프로세스 내 5초 캐시(변경 노드는 즉시 무효화), 인증은 세션·CSRF·권한 범위를 한 쿼리로 해석하고 API 키·키오스크의 사용 시각 갱신은 분당 1회로 제한한다. 응답은 gzip 압축되고 해시된 정적 자산은 1년 immutable 캐시, SPA 문서는 nonce 때문에 no-store다. 프런트엔드는 화면 단위로 코드 분할되어 모바일 방문증은 관리자 화면 코드를 내려받지 않는다.
- 영속 백업 단위는 PostgreSQL + 별도 보관한 `ENCRYPTION_KEY`
