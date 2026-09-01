# VisitFlow 관리자 가이드

## 최초 실행

컨테이너에는 `POSTGRES_DSN`, `BOOTSTRAP_ADMIN`, `BOOTSTRAP_ADMIN_PASSWORD`, `ENCRYPTION_KEY` 네 환경변수만 전달한다. 최초 관리자와 설치 암호화 키만 환경변수로 주입하며 이후 비밀번호·SSO·정책·연동은 관리자 UI에서 바꾼다.

## 필수 운영 설정

1. 일반: 회사명과 외부 기준 URL
2. 사업장: 주소, 로비, 방문 안내
3. Keycloak: Issuer, Client ID, Client Secret, 그룹 매핑
4. 방문 정책: 승인 사용, 조기 체크인, 미방문 유예, 자동 퇴실
5. QR: 1회 사용, Dynamic 주기
6. 알림: 기존 `log`/Webhook 호환 설정과 SMS·MMS·카카오 다중 API 및 발송 규칙
7. 개인정보: 마스킹·파기·감사 보존기간. 자주 방문자 주소록도 마지막 템플릿 사용 시점을 기준으로 동일한 파기 기간을 적용한다.
8. 보안: Session, 개인 키 만료와 회전 유예

## 메시지 API와 발송 규칙

관리자 → 메시지 API · 발송 규칙에서 Gateway를 여러 개 등록할 수 있다. 각 API는 채널(`sms`, `mms`, `kakao`), Base URL, Path, HTTP Method, 요청 형식(`json`, `form`, `query`), Header와 Parameter Template을 독립적으로 가진다. Header와 Parameter는 데이터베이스에 암호화 저장되며 `secretKeys`로 지정한 값은 조회 화면에서 마스킹된다.

Parameter 값에서는 `{{recipient}}`, `{{message}}`, `{{notificationId}}`, `{{idempotencyKey}}`, `{{visitId}}`, `{{visitor}}`, `{{company}}`, `{{start}}`, `{{place}}`, `{{passUrl}}`, `{{qrcodePath}}`, `{{qrcodeUrl}}` 같은 발송 문맥 변수를 사용할 수 있다. MMS 이미지 Parameter에는 `{{qrcodeUrl}}`을 지정하면 외부 기준 URL을 포함한 주소가, `{{qrcodePath}}`에는 `/img/visitor/{qrcode_file_seq}.jpg` 상대 경로가 전달된다.

활성 API는 Header 또는 Parameter에 `{{idempotencyKey}}`나 `{{notificationId}}` 중 하나를 반드시 전달해야 한다. 같은 알림 ID로 재시도해도 중복 발송하지 않도록 Gateway도 이 값을 멱등성 키로 처리해야 한다. API를 중지하면 연결된 활성 규칙도 중지되고 아직 발송되지 않은 대기 건은 취소된다.

외부 Gateway가 QR 이미지를 가져가려면 일반 설정의 외부 기준 URL을 반드시 Gateway에서 접근 가능한 HTTPS 주소로 설정한다. Gateway가 호스트를 별도로 알고 있어 상대 경로를 요구하는 경우에는 `/img/visitor/{{qrcodeFileSeq}}.jpg`를 Parameter에 직접 조합할 수 있다.

발송 규칙은 방문 확정, 방문 시작, 체크인, 퇴실, 방문 취소 중 하나를 선택하고 방문자/담당자 수신 대상, 분 단위 오프셋, 메시지 Template과 호출 API를 연결한다. 방문 시작 규칙은 음수 오프셋으로 사전 알림을 예약할 수 있고, 나머지 이벤트는 0 이상의 지연 발송을 사용한다. 방문 시작 규칙을 바꾸면 기존 예약 건도 수신자·채널·API·본문·시각을 한 묶음으로 다시 계산한다. 다른 이벤트의 큐 정책을 바꾸거나 규칙을 중지하면 혼합 설정으로 발송되지 않도록 기존 대기 건을 취소하고 다음 이벤트부터 새 설정을 적용한다.

## 기존 SMS Webhook 호환 계약

```json
{
  "recipient": "01012345678",
  "message": "방문 안내 본문",
  "channel": "sms",
  "idempotencyKey": "notification-uuid"
}
```

2xx를 성공으로 처리한다. 실패는 5분 단위 Backoff로 최대 5회 재시도하며 관리자 통계·알림 화면에 사유를 표시한다.

## 사용자 가이드 게시판

관리자 → 사용자 가이드에서 제목, 분류와 본문을 등록한다. 초안은 관리자에게만 보이고 게시 상태로 바꾼 글만 로그인 사용자에게 노출된다. 자주 확인해야 하는 글은 상단 고정할 수 있으며 수정·게시·삭제는 감사 로그에 기록된다.

## 백업

PostgreSQL과 `ENCRYPTION_KEY`를 같은 복구 시점으로 백업한다. Encryption Key는 Client Secret과 개인정보 복호화에 필요하므로 별도 보안 백업을 유지한다.

## 오프라인 업그레이드

새 Release의 `visitflow-vX.Y.Z.tar.gz`를 반입해 `docker load`한다. 아카이브의 `visitflow:latest` 별칭을 Compose가 바로 사용하며 시작 시 멱등 Migration이 자동 적용된다. 업그레이드 전 PostgreSQL과 `ENCRYPTION_KEY`를 백업한다.
