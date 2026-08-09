# VisitFlow 엔터프라이즈 중장기 기술 로드맵 (Product Roadmap Plan)

- **문서 버전**: v1.0.0 ~ v3.0-VISION  
- **작성일자**: 2026년 8월 10일  
- **문서 분류**: 비즈니스 및 아키텍처 중장기 로드맵 (Strategic Product Roadmap)  

---

## 1. 비전 및 발전 마일스톤 개요

VisitFlow 플랫폼은 사내 오프라인 QR 무인 체크인 및 PII 보호 관제를 시작으로, 사내 AI 데이터 에이전트와 대화형으로 방문자 출입 및 사옥 관제를 자율 수행하는 차세대 Autonomous Visitor Security Platform으로 진화합니다.

```
==================================================================================================
                                [VisitFlow 단계별 마일스톤 아키텍처]
==================================================================================================
 [Phase 1: v1.0.0] (완료) ➔ Pre-Reservation, QR Kiosk Check-in, PII AES-256, Overstay Detection
 [Phase 2: v1.5.0] (진행) ➔ Multi-Site Global Lobby Sync & Automated Facial Recognition Kiosk
 [Phase 3: v2.0.0] (2026 Q4) ➔ AI Autonomous Access Control Copilot (NL-to-Security MCP 2.0)
 [Phase 4: v3.0.0] (2027)    ➔ Predictive Physical Security & Zero-Trust Building Flow Analytics
==================================================================================================
```

---

## 2. Phase별 세부 기술 명세 및 추진 전략

### 2.1 Phase 1: v1.0.0 오프라인 방문자 관리 플랫폼 구축 (완료)
- **방문 사전 예약 & QR 체크인**: 사전 예약 신청, 접견자 1클릭 승인, QR 스캔 무인 키오스크 체크인.
- **이상 출입 & 초과 잔류 감지**: 미체크아웃 초과 잔류(Overstaying), 미반납 출입증, 블랙리스트 감지.
- **PII 암호화 & Keycloak OIDC**: 성명/전화번호 AES-256-GCM 암호화, PKCE SSO 및 3대 환경변수 부트스트랩.
- **Streamable HTTP MCP**: AI 에이전트를 위한 8개 이상의 ACL-aware MCP Tools 탑재.

### 2.2 Phase 2: v1.5.0 멀티 사옥 로비 동기화 & 안면 인식 키오스크 (2026 Q3)
- **다중 사옥 로비 통합 뷰**: 글로벌 사옥/지사 로비 출입 데이터 실시간 동기화.
- **안면 인식 1-Touch 체크인**: 선택적 안면 인식 생체 인증 기반 키오스크 파이프라인.

### 2.3 Phase 3: v2.0.0 AI 자율 출입 관제 코파일럿 (2026 Q4)
- **NL-to-Security Action (MCP 2.0)**: AI 에이전트에 "오늘 R&D센터 초과 잔류자 목록 보여주고 보안팀에 경보 발송해줘" 요청 시 권한 검증 후 자율 수행.

---

## 3. 리소스 및 위험 관리 (Risk Matrix)

| 위험 요소 | 영향도 | 발생 가능성 | 대응 및 완화 전략 |
| :--- | :--- | :--- | :--- |
| **PostgreSQL DB 장애** | High | Low | Multi-AZ HA 클러스터 및 Read-Replica 구축 |
| **PII 암호화 키 손실** | High | Low | `/var/lib/visitflow/master.key` 자동 이중화 백업 |
| **초과 잔류 감지 오알람** | Medium | Medium | 약정 유예시간 설정 및 접견자 연장 승인 기능 제공 |
