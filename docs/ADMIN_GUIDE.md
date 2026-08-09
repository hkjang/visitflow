# VisitFlow 엔터프라이즈 관리자 가이드 (Admin & Operational Guide)

- **문서 버전**: v1.0.0-ENTERPRISE  
- **작성일자**: 2026년 8월 10일  
- **대상**: 시스템 관리자, Security/DevOps 엔지니어, 시설보안 책임자  
- **문서 개요**: VisitFlow 3대 환경변수 부트스트랩, Keycloak OIDC SSO, PII 암호화 키 보존, 미체크아웃 초과 잔류 감지 규칙 및 감사 로그 운영  

---

## 1. 시스템 부트스트랩 및 필수 환경변수 (Bootstrap Specification)

VisitFlow 컨테이너 프로세스는 오직 **3개의 애플리케이션 환경변수**만으로 최소 인프라 구축 및 부트스트랩을 완수합니다.

```bash
# VisitFlow 실행 환경변수 명세
POSTGRES_DSN=postgres://visitflow:Secr3tPass@10.10.60.5:5432/visitflow?sslmode=disable
BOOTSTRAP_ADMIN=admin
BOOTSTRAP_ADMIN_PASSWORD=SuperSecretAdminPassword123!
```

> **비상 관리자(Break Glass) 원칙**:  
> `BOOTSTRAP_ADMIN` 계정은 시스템 최초 실행 시 DB 계정이 존재하지 않을 때만 자동 생성되며 삭제가 불가능한 비상 복구용 계정입니다.

---

## 2. 볼륨 마운트 및 마스터 키 백업 (`/var/lib/visitflow`)

컨테이너 기동 시 반드시 마운트해야 하는 3대 데이터 볼륨:

```bash
docker run -d \
  --name visitflow \
  -p 8080:8080 \
  -e POSTGRES_DSN="postgres://visitflow:password@postgres:5432/visitflow" \
  -e BOOTSTRAP_ADMIN="admin" \
  -e BOOTSTRAP_ADMIN_PASSWORD="change-this-strong-password" \
  -v visitflow-data:/var/lib/visitflow \
  visitflow:v1.0.0
```

### 2.1 마스터 키 보존 중요성 (`/var/lib/visitflow/master.key`)
`/var/lib/visitflow/master.key` 파일은 DB에 저장된 Client Secret, API Token 및 방문자 PII 개인정보(성명/전화번호)를 암호화(AES-256-GCM)하는 마스터 키입니다. 이 키를 분실할 경우 암호화 데이터 복호화가 불가능하므로 반드시 정기 백업 대상에 포함시켜야 합니다.

---

## 3. Keycloak OIDC SSO 및 RBAC 그룹 매핑

- **OIDC Discovery**: Keycloak Discovery 엔드포인트를 등록하고 Authorization Code + PKCE (S256) 인증을 켭니다.
- **Valid Redirect URI**: `https://visitflow.internal/api/v1/auth/oidc/callback`
- **그룹 매핑**: Keycloak `/visitflow-admins`, `/visitflow-receptionists` 그룹을 사내 권한 그룹으로 맵핑하여 자동 RBAC 부여.

---

## 4. 이상 출입 자동 감지 규칙 및 감사 로그 (Audit Trail)

- **이상 상태 검출 규칙**:
  - `OVERSTAYING_ALERT`: 약정 예약 시간 경과 후에도 체크아웃되지 않은 방문자.
  - `UNRETURNED_CARD`: 퇴실 처리되었으나 출입증 매핑이 해제되지 않은 상태.
  - `BLACKLIST_ATTEMPT`: 블랙리스트 지정 인원의 방문 사전 신청 시도.
- **감사 로그 (Audit Trail)**: 사옥/로비 추가, 방문 승인/반려, PII 조회, OIDC 변경 등 모든 액션이 사용자 ID 및 IP 주소와 함께 무결성 감사 레코드로 영구 기록됩니다.
