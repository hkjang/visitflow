package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingFrequentVisitorExecutor struct {
	tag   pgconn.CommandTag
	err   error
	query string
	args  []any
}

func (executor *recordingFrequentVisitorExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	executor.query = query
	executor.args = args
	return executor.tag, executor.err
}

func TestValidateFrequentVisitorInput(t *testing.T) {
	valid := VisitorInput{Name: "홍길동", Phone: "010-1234-5678", Equipment: []string{"노트북"}, Consent: true}
	if message := validateFrequentVisitorInput(valid); message != "" {
		t.Fatalf("valid frequent visitor rejected: %s", message)
	}
	invalid := []VisitorInput{
		{Name: "", Phone: "010-1234-5678", Consent: true},
		{Name: "홍길동", Phone: "123", Consent: true},
		{Name: "홍길동", Phone: "010-1234-5678", Consent: false},
	}
	for _, input := range invalid {
		if message := validateFrequentVisitorInput(input); message == "" {
			t.Fatalf("invalid frequent visitor accepted: %#v", input)
		}
	}
}

func TestValidateFrequentVisitorIDs(t *testing.T) {
	if message := validateFrequentVisitorIDs([]string{"visitor-a", "visitor-b"}); message != "" {
		t.Fatalf("valid visitor IDs rejected: %s", message)
	}
	if message := validateFrequentVisitorIDs([]string{"visitor-a", "visitor-a"}); message == "" {
		t.Fatal("duplicate visitor IDs were accepted")
	}
	tooMany := make([]string, maxTemplateFrequentVisitors+1)
	for index := range tooMany {
		tooMany[index] = newID()
	}
	if message := validateFrequentVisitorIDs(tooMany); message == "" {
		t.Fatal("visitor limit was not enforced")
	}
}

func TestValidateVisitTemplatePayloadRejectsPII(t *testing.T) {
	valid := map[string]any{"purpose": "회의", "placeDetail": "8층", "company": "ABC"}
	if message := validateVisitTemplatePayload(valid); message != "" {
		t.Fatalf("valid payload rejected: %s", message)
	}
	for _, payload := range []map[string]any{
		{"visitors": []any{map[string]any{"phone": "01012345678"}}},
		{"phone": "01012345678"},
		{"purpose": 42},
	} {
		if message := validateVisitTemplatePayload(payload); message == "" {
			t.Fatalf("unsafe payload accepted: %#v", payload)
		}
	}
}

func TestTouchFrequentVisitorIDsRefreshesRetentionClockAndValidatesOwnership(t *testing.T) {
	visitorIDs := []string{"visitor-b", "visitor-a"}
	executor := &recordingFrequentVisitorExecutor{tag: pgconn.NewCommandTag("UPDATE 2")}
	if err := touchFrequentVisitorIDs(context.Background(), executor, "user-a", visitorIDs); err != nil {
		t.Fatalf("touch frequent visitors: %v", err)
	}
	assertRetentionTouchQuery(t, executor.query)
	if len(executor.args) != 2 || executor.args[0] != "user-a" || !reflect.DeepEqual(executor.args[1], visitorIDs) {
		t.Fatalf("unexpected touch arguments: %#v", executor.args)
	}

	executor = &recordingFrequentVisitorExecutor{tag: pgconn.NewCommandTag("UPDATE 1")}
	if err := touchFrequentVisitorIDs(context.Background(), executor, "user-a", visitorIDs); !errors.Is(err, errInvalidFrequentVisitorSelection) {
		t.Fatalf("partial ownership must reject the selection, got %v", err)
	}
}

func TestTouchFrequentVisitorIDsSkipsEmptySelection(t *testing.T) {
	executor := &recordingFrequentVisitorExecutor{}
	if err := touchFrequentVisitorIDs(context.Background(), executor, "user-a", nil); err != nil {
		t.Fatalf("touch empty frequent visitor selection: %v", err)
	}
	if executor.query != "" {
		t.Fatalf("empty selection unexpectedly executed query: %s", executor.query)
	}
}

func TestTouchTemplateFrequentVisitorsLocksAndRefreshesBeforeDetailRead(t *testing.T) {
	executor := &recordingFrequentVisitorExecutor{tag: pgconn.NewCommandTag("UPDATE 2")}
	if err := touchTemplateFrequentVisitors(context.Background(), executor, "user-a", "template-a"); err != nil {
		t.Fatalf("touch template frequent visitors: %v", err)
	}
	assertRetentionTouchQuery(t, executor.query)
	compactQuery := strings.Join(strings.Fields(executor.query), " ")
	if !strings.Contains(compactQuery, "link.template_id=$2") || !strings.Contains(compactQuery, "link.user_id=$1") {
		t.Fatalf("template touch is not owner scoped: %s", compactQuery)
	}
	if !reflect.DeepEqual(executor.args, []any{"user-a", "template-a"}) {
		t.Fatalf("unexpected template touch arguments: %#v", executor.args)
	}
}

func assertRetentionTouchQuery(t *testing.T, query string) {
	t.Helper()
	compactQuery := strings.Join(strings.Fields(query), " ")
	for _, required := range []string{"SET last_used_at=now()", "FOR UPDATE OF visitor", "visitor.user_id=$1"} {
		if !strings.Contains(compactQuery, required) {
			t.Fatalf("retention touch query is missing %q: %s", required, compactQuery)
		}
	}
}
