# SeatOn 아키텍처

```text
Browser / MCP client
        │ same-origin HTTPS, OIDC PKCE, personal key
        ▼
┌────────────────────────────────────────────┐
│ SeatOn single container                    │
│ Go HTTP API + embedded React/MUI UI        │
│ RBAC · audit · rules · offline CV · MCP    │
│ PDF rasterizer (first page)                │
└───────────────┬────────────────────────────┘
                │ POSTGRES_DSN
                ▼
          PostgreSQL 14+

Persistent pair: PostgreSQL backup + /var/lib/seaton/master.key
```

도면 파일도 PostgreSQL에 보관하여 별도 오브젝트 스토리지가 필요 없다. Keycloak과 인사 API는 관리자 설정으로 사내 엔드포인트를 지정한다. 설치별 마스터 키는 Keycloak Client Secret, 인사 API Token 등 비밀 설정을 AES-256-GCM으로 암호화한다.

도면 좌표는 `0..1` 비율 값으로 저장한다. React UI는 외부 지도나 CDN 없이 SVG 오버레이로 좌석을 표시하므로 화면 크기와 오프라인 환경에 독립적이다.

기본 도면 분석기 `offline-cv-v1`은 이미지 보정 후 명암 연결 요소와 직사각형 특성을 계산하여 좌석 후보와 신뢰도를 생성한다. PDF는 첫 페이지를 로컬에서 래스터화한다. 고신뢰도 후보를 자동 생성하고 기준 이하 항목만 검토 대상으로 남긴다. 정교한 사내 Vision 모델은 이후 동일한 분석 작업 인터페이스에 교체할 수 있다.
