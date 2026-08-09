# SeatOn 엔터프라이즈 중장기 기술 로드맵 (Product Roadmap Plan)

- **문서 버전**: v1.0.0 ~ v3.0-VISION  
- **작성일자**: 2026년 8월 9일  
- **문서 분류**: 비즈니스 및 아키텍처 중장기 로드맵 (Strategic Product Roadmap)  

---

## 1. 비전 및 발전 마일스톤 개요

SeatOn 플랫폼은 오프라인 CV 도면 분석 및 SVG 비율좌표 스마트 좌석 관리를 시작으로, 사내 AI 데이터 에이전트와 대화형으로 오피스 공간 및 좌석을 자동 추천·예약하는 차세대 Autonomous Smart Office Platform으로 진화합니다.

```
==================================================================================================
                                [SeatOn 단계별 마일스톤 아키텍처]
==================================================================================================
 [Phase 1: v1.0.0] (완료) ➔ Offline CV Parsing, SVG Coordinate Engine, Anomaly Seat Detection
 [Phase 2: v1.5.0] (진행) ➔ Multi-Building Floorplan Sync & Real-Time IoT Sensor Integration
 [Phase 3: v2.0.0] (2026 Q4) ➔ AI Auto Seat Allocation Copilot (NL-to-Space Action MCP 2.0)
 [Phase 4: v3.0.0] (2027)    ➔ Predictive Office Space Analytics & Autonomous Energy Saving
==================================================================================================
```

---

## 2. Phase별 세부 기술 명세 및 추진 전략

### 2.1 Phase 1: v1.0.0 오프라인 스마트 좌석 플랫폼 구축 (완료)
- **도면 CV & SVG 엔진**: PNG/PDF 도면 오프라인 CV 후보 파싱, 비율 좌표 SVG 좌석 편집기.
- **이상 좌석 감지**: 미배정, 퇴직자 점유, 조직 구역 불일치 자동 감지.
- **Keycloak OIDC & Break Glass**: PKCE SSO 연동 및 3대 환경변수 부트스트랩.
- **Streamable HTTP MCP**: AI 에이전트를 위한 8개 이상의 ACL-aware MCP Tools 탑재.

### 2.2 Phase 2: v1.5.0 멀티 사옥 도면 동기화 & 실시간 센서 연동 (2026 Q3)
- **다중 건물/사옥 통합 뷰**: 본사, 지사, R&D 센터 멀티 사옥 레이아웃 통합 관리.
- **IoT 모션 센서 연동**: 좌석 재실 센서 데이터 실시간 바인딩.

### 2.3 Phase 3: v2.0.0 AI 자율 좌석 코파일럿 (2026 Q4)
- **NL-to-Space Action (MCP 2.0)**: AI 에이전트에 "10층 개발팀 구역 근처에 빈 좌석 찾아 배정해줘" 요청 시 권한 검증 후 자동 배치.

---

## 3. 리소스 및 위험 관리 (Risk Matrix)

| 위험 요소 | 영향도 | 발생 가능성 | 대응 및 완화 전략 |
| :--- | :--- | :--- | :--- |
| **PostgreSQL DB 장애** | High | Low | Multi-AZ HA 클러스터 및 Read-Replica 구축 |
| **도면 CV 파싱 오차** | Medium | Medium | 신뢰도 점수 기반 관리자 수동 교정 레이어 제공 |
| **Master Key 분실** | High | Low | `/var/lib/seaton/master.key` 자동 이중화 백업 |
