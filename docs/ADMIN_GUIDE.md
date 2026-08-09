# VisitFlow 관리자 가이드

## 최초 실행

컨테이너에는 `POSTGRES_DSN`, `BOOTSTRAP_ADMIN`, `BOOTSTRAP_ADMIN_PASSWORD` 세 환경변수만 전달한다. 최초 관리자만 환경변수로 생성하며 이후 비밀번호·SSO·정책·연동은 관리자 UI에서 바꾼다.

## 필수 운영 설정

1. 일반: 회사명과 외부 기준 URL
2. 사업장: 주소, 로비, 방문 안내
3. Keycloak: Issuer, Client ID, Client Secret, 그룹 매핑
4. 방문 정책: 승인 사용, 조기 체크인, 미방문 유예, 자동 퇴실
5. QR: 1회 사용, Dynamic 주기
6. 알림: `log` 검증 또는 사내 SMS Webhook과 템플릿
7. 개인정보: 마스킹·파기·감사 보존기간
8. 보안: Session, 개인 키 만료와 회전 유예

## SMS Webhook 계약

```json
{
  "recipient": "01012345678",
  "message": "방문 안내 본문",
  "channel": "sms",
  "idempotencyKey": "notification-uuid"
}
```

2xx를 성공으로 처리한다. 실패는 5분 단위 Backoff로 최대 5회 재시도하며 관리자 통계·알림 화면에 사유를 표시한다.

## 백업

PostgreSQL과 `/var/lib/visitflow/master.key`를 같은 복구 시점으로 백업한다. Master Key는 Client Secret과 개인정보 복호화에 필요하므로 별도 보안 백업을 유지한다.

## 오프라인 업그레이드

새 Release의 `VisitFlow-vX.Y.Z-linux-amd64-image.tar.gz`를 반입해 `docker load`하고 Compose 이미지 태그만 변경한다. 시작 시 멱등 Migration이 자동 적용된다. 업그레이드 전 PostgreSQL과 Master Key를 백업한다.
