package app

import (
	"strings"
	"testing"
)

// The MCP statistics tool and the statistics screen answer the same question,
// so they have to read the same window: the site-local span the trend draws,
// closed at both ends. The old query took CURRENT_DATE-days with no upper
// bound, which both started on the wrong day and counted future bookings.
func TestMCPVisitStatisticsQueryUsesTheStatisticsSpan(t *testing.T) {
	if strings.Contains(mcpVisitStatisticsQuery, "CURRENT_DATE-$1") {
		t.Fatalf("MCP statistics still spans from the session date: %s", mcpVisitStatisticsQuery)
	}
	if !strings.Contains(mcpVisitStatisticsQuery, statisticsSpanWhere("v.start_at")) {
		t.Fatalf("MCP statistics does not reuse the statistics span filter: %s", mcpVisitStatisticsQuery)
	}
	// The span filter dates v.start_at in si.timezone, so the query has to join
	// the site it names or PostgreSQL rejects it.
	if !strings.Contains(mcpVisitStatisticsQuery, "JOIN sites si ON si.id=v.site_id") {
		t.Fatalf("MCP statistics does not join the site the span filter dates by: %s", mcpVisitStatisticsQuery)
	}
}

// Every tool the server advertises has to be one executeMCPTool answers to,
// otherwise an agent picks a name from tools/list and gets "알 수 없는 도구".
func TestMCPToolsAreUniquelyNamed(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range mcpTools() {
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("tool without a name: %v", tool)
		}
		if seen[name] {
			t.Fatalf("tool %s is advertised twice", name)
		}
		seen[name] = true
		if _, ok := tool["inputSchema"]; !ok {
			t.Fatalf("tool %s has no input schema", name)
		}
	}
	if !seen["get_visit_statistics"] {
		t.Fatal("get_visit_statistics is no longer advertised")
	}
}
