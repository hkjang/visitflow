<p align="center">
  <img src="docs/favicon.svg" alt="VisitFlow Logo" width="90"><br><br>
  <h1 align="center">VisitFlow</h1>
</p>

<p align="center">
  <strong>사내 방문자 예약, QR 출입 통제 및 오프라인 에어갭 방문자 관리 플랫폼</strong><br>
  사전 예약 신청, QR 무인 체크인, 보안 서약서 동의, 이상 출입 감지 및 Streamable MCP를 단일 Docker 컨테이너로 제공합니다.
</p>

<p align="center">
  <a href="https://hkjang.github.io/visitflow/">🇰🇷 홍보 페이지</a> · <a href="https://hkjang.github.io/visitflow/index_en.html">🇺🇸 English Page</a> · <a href="https://github.com/sponsors/hkjang">💖 Sponsor</a>
</p>

---

## 주요 기능

- 방문자 사전 예약, 접견자 승인 및 보안 서약서(NDA) 디지털 서명
- QR 코드 발급, 키오스크 셀프 체크인/체크아웃, 임시 출입증(Access Card) 발급 및 회수 트래킹
- 미체크아웃 초과 잔류(Overstaying), 미반납 출입증, 블랙리스트 자동 감지 및 관제
- 3개 구역 분리 (`/app` 방문자 관제, `/me` 개인화 및 Key, `/admin` 사옥/로비/보안 정책)
- PII 데이터 암호화(AES-256-GCM) 및 마스킹 처리 (전화번호/이름)
- Keycloak OIDC Discovery + Authorization Code/PKCE + Group RBAC
- 삭제 불가능한 Break Glass 부트스트랩 비상 관리자 계정
- HMAC Digest 기반 Personal API/MCP Key 관리 및 7일 유예기간 회전
- REST/OpenAPI 및 8개 이상의 ACL-aware Streamable HTTP MCP Tools
- 외부 CDN 및 인터넷 연결이 필요 없는 100% 폐쇄망(Air-Gapped) 단일 Docker 이미지

## 실행

외부 또는 사내 PostgreSQL 14+ 데이터베이스를 준비합니다. VisitFlow가 시작될 때 DB 스키마를 자동 생성합니다.

```bash
docker load < VisitFlow-v1.0.0-linux-amd64-image.tar.gz

export POSTGRES_DSN='postgres://visitflow:password@postgres.intra:5432/visitflow?sslmode=require'
export BOOTSTRAP_ADMIN='admin'
export BOOTSTRAP_ADMIN_PASSWORD='change-this-strong-password'
docker compose up -d
```

애플리케이션이 전달받는 필수 환경변수는 아래 세 개뿐입니다.

| 환경변수 | 설명 |
| --- | --- |
| `POSTGRES_DSN` | PostgreSQL 접속 DSN |
| `BOOTSTRAP_ADMIN` | 최초 시스템 관리자 아이디 |
| `BOOTSTRAP_ADMIN_PASSWORD` | 최초 관리자 비밀번호 (12자 이상) |

## 백업과 복구

PostgreSQL 데이터베이스 백업과 `/var/lib/visitflow` 볼륨을 한 세트로 백업합니다. `/var/lib/visitflow/master.key`를 손실하면 DB의 암호화된 시크릿 및 PII 개인정보를 복호화할 수 없으므로 정기 백업이 필수적입니다.

## 라이선스

Apache License 2.0. 자세한 내용은 [LICENSE](LICENSE)를 확인하세요.
