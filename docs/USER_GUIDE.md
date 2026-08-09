# SeatOn 엔터프라이즈 사용자 가이드 (User Guide & Manual)

- **문서 버전**: v1.0.0-ENTERPRISE  
- **작성일자**: 2026년 8월 9일  
- **대상**: 일반 임직원, 총무/인사/시설 관리자, AI MCP 클라이언트 사용자  
- **문서 개요**: SVG 좌석맵 검색 및 선택, Drag & Drop 좌석 배정, CV 도면 후보 파싱, Personal Key 발급 및 Streamable MCP 연동 매뉴얼  

---

## 1. 개요 및 좌석맵 워크플로우 (SeatMap Workflow)

SeatOn은 사무실 도면과 직원 정보를 비율 좌표 SVG와 오프라인 Computer Vision(CV)으로 연결하여 스마트 오피스 환경을 구축합니다.

---

## 2. 일반 직원용 검색 및 좌석 탐색

- **직원/좌석 검색**: 상단 검색창에 이름, 부서명, 좌석 번호(예: `10F-W-42`)를 입력하여 도면 상의 위치를 실시간 강조 표시합니다.
- **좌석 뷰어**: 해상도 및 모니터 크기에 맞춰 확대/축소(Zoom) 및 드래그 팬(Pan) 기능을 제공합니다.

---

## 3. 관리자용 도면 등록 & Drag & Drop 좌석 배정

### 3.1 CV 도면 파싱 및 좌석 후보 자동 탐지
1. 관리자 메뉴 ➔ **[도면 관리]** ➔ 신규 건축 도면(PNG, JPG, PDF) 업로드.
2. **[CV 좌석 후보 분석]** 클릭 ➔ 오프라인 CV 파서가 좌석 사각형 위치를 자동 검출하여 파란색 가이드 라인으로 표시.
3. 확정 버튼 클릭 시 비율 좌표 SVG 편집기에 좌석 그리드가 자동 배치됩니다.

### 3.2 Drag & Drop 배정 및 CSV/XLSX 일괄 가져오기
- 직원 목록 패널에서 직원을 선택 후 목표 좌석으로 Drag & Drop하여 실시간 배정.
- 인사 CSV/XLSX 파일을 일괄 업로드하여 조직 단위 전원 자동 배정 처리.

---

## 4. Personal API / MCP Key 발급 및 AI 연동

1. 프로필 메뉴 ➔ **`/me/keys` (개인 API/MCP 키)** 이동.
2. **[신규 Personal Key 발급]** 클릭 ➔ `stn_7f9c8d11a2b3c4d5_xxxxxxxx` 형식 키 생성.
3. Claude Desktop 또는 Cursor 설정 파일에 MCP 서버를 등록하여 자연어로 빈 좌석 검색:

```json
{
  "mcpServers": {
    "seaton": {
      "command": "curl",
      "args": [
        "-X", "POST",
        "-H", "Authorization: Bearer stn_7f9c8d11a2b3c4d5_xxxxxxxx",
        "https://seaton.internal/mcp"
      ]
    }
  }
}
```

### 제공되는 핵심 MCP Tools 목록
1. `seaton_search_seats`: 빈 좌석 및 부서별 배치 현황 파싱
2. `seaton_get_floor_layout`: 층별 도면 및 비율 좌표 좌석 정보 조회
3. `seaton_assign_seat`: 사용자 권한 범위 내 좌석 배정 및 이동
4. `seaton_list_anomalies`: 이상 점유 좌석 및 퇴직자 좌석 탐지 리포트
