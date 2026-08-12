# VisitFlow 아키텍처

```text
임직원 Web  관리자 Web  로비/키오스크  모바일 방문증  MCP Client
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
- 감사: 로그인, 방문 변경, QR 검증, 입·퇴실, 개인정보/Watch List 조회, 설정과 키 변경 기록

## 상태와 트랜잭션

```text
REQUESTED → PENDING_APPROVAL → SCHEDULED → CHECKED_IN → CHECKED_OUT
                    └→ REJECTED     ├→ CANCELLED
                                    └→ NO_SHOW
```

방문 승인 시 방문자별 QR 생성과 방문증 알림 큐 등록을 같은 DB 트랜잭션에서 처리한다. 체크인 시 QR Row Lock, 상태/기간/Replay 검증, Token 사용 처리, 방문자/방문 상태 전이, 이벤트 기록, 담당자 도착 알림 큐 등록을 한 트랜잭션에서 처리한다.

## 운영 구성

- PostgreSQL 외 Redis, Message Broker, Object Storage 불필요
- 알림 Worker는 `log`와 사내 `webhook` Adapter를 제공하며 최대 5회 재시도
- SSE는 프로세스 내 Fan-out으로 로비 변경을 전달하고 DB가 Source of Truth 역할을 수행
- Scheduler는 미방문, 자동 퇴실, Session 정리, 개인정보 파기와 감사 로그 보존을 수행
- 영속 백업 단위는 PostgreSQL + 별도 보관한 `ENCRYPTION_KEY`
