# VisitFlow 엔터프라이즈 사용자 가이드 (User Guide & Manual)

- **문서 버전**: v1.0.0-ENTERPRISE  
- **작성일자**: 2026년 8월 10일  
- **대상**: 일반 임직원(Host), 안내데스크/키오스크 사용자, AI MCP 클라이언트 사용자  
- **문서 개요**: 사전 방문 신청 및 접견 승인, QR 무인 체크인, 보안 서약서 동의, Personal Key 발급 및 Streamable MCP 연동 매뉴얼  

---

## 1. 개요 및 방문자 워크플로우 (Visitor Workflow)

VisitFlow는 사전 예약, QR 체크인, PII 암호화 및 무인 출입 관제를 완벽하게 지원하는 스마트 오피스 플랫폼입니다.

---

## 2. 사전 방문 신청 및 접견자(Host) 승인

1. **사전 방문 신청**: 방문자가 사외 웹 링크를 통해 방문 일시, 성명, 연락처, 방문 목적, 차종/차량번호 및 반입 자산을 등록합니다.
2. **접견자 승인**: 담당 임직원(Host)에게 알림이 전송되며, 1클릭으로 승인/반려합니다.
3. **QR 발급**: 승인 완료 시 방문자 단말기로 셀프 체크인용 QR 코드가 발급됩니다.

---

## 3. 키오스크 무인 체크인 & 보안 서약서(NDA)

1. **키오스크 스캔**: 사옥 로비 무인 키오스크에 발급된 QR 코드를 스캔합니다.
2. **보안 서약 서명**: 화면에서 사내 보안 규정 및 NDA 동의 서명을 진행합니다.
3. **출입증 발급**: 키오스크에서 임시 출입증(Access Card)이 출력/발급되며 접견자에게 "방문자 로비 도착" 알림이 발송됩니다.

---

## 4. Personal API / MCP Key 발급 및 AI 연동

1. 프로필 메뉴 ➔ **`/me/keys` (개인 API/MCP 키)** 이동.
2. **[신규 Personal Key 발급]** 클릭 ➔ `vst_7f9c8d11a2b3c4d5_xxxxxxxx` 형식 키 생성.
3. Claude Desktop 또는 Cursor 설정 파일에 MCP 서버를 등록하여 자연어로 방문자 현황 조회:

```json
{
  "mcpServers": {
    "visitflow": {
      "command": "curl",
      "args": [
        "-X", "POST",
        "-H", "Authorization: Bearer vst_7f9c8d11a2b3c4d5_xxxxxxxx",
        "https://visitflow.internal/mcp"
      ]
    }
  }
}
```

### 제공되는 핵심 MCP Tools 목록
1. `visitflow_search_visits`: 사전 방문 예약 및 당일 입장 현황 조회
2. `visitflow_get_active_visitors`: 사내 현재 체류 중인 방문자 수 및 로비별 현황 파악
3. `visitflow_approve_visit`: 권한 범위 내 방문 신청 1클릭 승인/반려
4. `visitflow_list_overstay_alerts`: 미체크아웃 초과 잔류 및 이상 출입자 리포트
